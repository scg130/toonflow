package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"toonflow/logger"
)

// DefaultProgressHeartbeat is how often long AI waits refresh the chat progress bar.
const DefaultProgressHeartbeat = 5 * time.Second

// PipelineStatus tracks the live "where are we now" of a running pipeline so the
// UI can show 分集 / 环节 / 分镜 / 重试 in real time. Safe for concurrent use.
type PipelineStatus struct {
	mu        sync.Mutex
	stepID    string
	stepLabel string
	shot      int // current shot number (0 = not shot-scoped)
	shotSeq   int // 1-based position within the current batch
	shotTotal int // total shots in the current batch
	attempt   int // current retry attempt (0 = first try / no retry in progress)
	maxAtt    int // max attempts (0 = unlimited/unknown)
	note      string

	// last progress emission — used by the 5s heartbeat while waiting on AI.
	lastStep string
	lastPct  float32
	lastMsg  string
	phaseAt  time.Time
}

// SetStep marks the start of a new pipeline step and resets shot/retry state.
func (s *PipelineStatus) SetStep(id, label string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.stepID, s.stepLabel = id, label
	s.shot, s.shotSeq, s.shotTotal = 0, 0, 0
	s.attempt, s.maxAtt, s.note = 0, 0, ""
	s.mu.Unlock()
}

// SetShot marks the shot currently being processed and resets retry state.
func (s *PipelineStatus) SetShot(shot, seq, total int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.shot, s.shotSeq, s.shotTotal = shot, seq, total
	s.attempt, s.maxAtt, s.note = 0, 0, ""
	s.mu.Unlock()
}

// SetRetry records an in-progress retry (max=0 means unbounded).
func (s *PipelineStatus) SetRetry(attempt, max int, note string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.attempt, s.maxAtt, s.note = attempt, max, note
	s.mu.Unlock()
}

// ClearRetry clears any in-progress retry marker (e.g. after a success).
func (s *PipelineStatus) ClearRetry() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.attempt, s.maxAtt, s.note = 0, 0, ""
	s.mu.Unlock()
}

// ShotProgress returns the current (seq, total) within the batch.
func (s *PipelineStatus) ShotProgress() (int, int) {
	if s == nil {
		return 0, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shotSeq, s.shotTotal
}

// CurrentShot returns the shot number currently being processed (0 if none).
func (s *PipelineStatus) CurrentShot() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shot
}

// RecordProgress remembers the latest chat progress line for heartbeat refreshes.
func (s *PipelineStatus) RecordProgress(step string, pct float32, msg string) {
	if s == nil {
		return
	}
	msg = strings.TrimSpace(stripWaitSuffix(msg))
	if msg == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if msg != s.lastMsg {
		s.phaseAt = time.Now()
	}
	s.lastStep, s.lastPct, s.lastMsg = step, pct, msg
}

// HeartbeatPayload returns a refreshed progress line when the phase has been
// waiting long enough (so the chat bar stays alive during slow image/video calls).
func (s *PipelineStatus) HeartbeatPayload() (step string, pct float32, msg string, ok bool) {
	if s == nil {
		return "", 0, "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastMsg == "" || s.phaseAt.IsZero() {
		return "", 0, "", false
	}
	elapsed := time.Since(s.phaseAt)
	if elapsed < 4*time.Second {
		return "", 0, "", false
	}
	sec := int(elapsed.Seconds())
	msg = fmt.Sprintf("%s · 已等待 %d 秒", s.lastMsg, sec)
	if s.attempt > 0 {
		if s.maxAtt > 0 {
			msg = fmt.Sprintf("%s · 重试 %d/%d · 已等待 %d 秒", s.lastMsg, s.attempt, s.maxAtt, sec)
		} else {
			msg = fmt.Sprintf("%s · 重试中 · 已等待 %d 秒", s.lastMsg, sec)
		}
	}
	return s.lastStep, s.lastPct, msg, true
}

// Snapshot returns a JSON-friendly copy for WS payloads (nil-safe).
func (s *PipelineStatus) Snapshot() map[string]interface{} {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]interface{}{
		"step_id":     s.stepID,
		"step_label":  s.stepLabel,
		"shot":        s.shot,
		"shot_seq":    s.shotSeq,
		"shot_total":  s.shotTotal,
		"attempt":     s.attempt,
		"max_attempt": s.maxAtt,
		"retry_note":  s.note,
	}
	if s.lastMsg != "" && !s.phaseAt.IsZero() {
		out["waiting_sec"] = int(time.Since(s.phaseAt).Seconds())
		out["last_msg"] = s.lastMsg
	}
	return out
}

func stripWaitSuffix(msg string) string {
	for {
		trimmed := false
		if i := strings.LastIndex(msg, " · 已等待 "); i >= 0 {
			msg = strings.TrimSpace(msg[:i])
			trimmed = true
		}
		if i := strings.LastIndex(msg, " · 重试中"); i >= 0 {
			msg = strings.TrimSpace(msg[:i])
			trimmed = true
		}
		if i := strings.LastIndex(msg, " · 重试 "); i >= 0 {
			msg = strings.TrimSpace(msg[:i])
			trimmed = true
		}
		if !trimmed {
			break
		}
	}
	return msg
}

type ctxKeyPipelineStatus struct{}

// WithPipelineStatus attaches a live PipelineStatus tracker to context.
func WithPipelineStatus(ctx context.Context, s *PipelineStatus) context.Context {
	if s == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyPipelineStatus{}, s)
}

// PipelineStatusFromContext returns the tracker bound to ctx, if any.
func PipelineStatusFromContext(ctx context.Context) *PipelineStatus {
	s, _ := ctx.Value(ctxKeyPipelineStatus{}).(*PipelineStatus)
	return s
}

// ProgressFunc reports step progress (0-100) during long operations.
type ProgressFunc func(step string, progress float32, message string)

// StreamDeltaFunc emits a text delta during streaming chat completion.
type StreamDeltaFunc func(delta string)

// StreamEndFunc signals the end of a streaming chat completion.
type StreamEndFunc func()

type ctxKeyProgress struct{}
type ctxKeyStreamDelta struct{}
type ctxKeyStreamEnd struct{}
type ctxKeyStepProgress struct{}

// stepProgress maps a sub-task's 0–100% into a parent step's progress range.
type stepProgress struct {
	stepID string
	base   float32
	span   float32
}

// WithStepProgress scopes shot-level progress into [base, base+span] on the parent step.
func WithStepProgress(ctx context.Context, stepID string, base, span float32) context.Context {
	return context.WithValue(ctx, ctxKeyStepProgress{}, &stepProgress{stepID: stepID, base: base, span: span})
}

// ReportStepProgress emits progress for the current step range when bound.
func ReportStepProgress(ctx context.Context, localPct float32, message string) {
	sp, _ := ctx.Value(ctxKeyStepProgress{}).(*stepProgress)
	if sp != nil {
		ReportProgress(ctx, sp.stepID, sp.base+sp.span*localPct/100, message)
		return
	}
	ReportProgress(ctx, "", localPct, message)
}

// ProgressFromContext returns the progress callback bound to ctx, if any.
func ProgressFromContext(ctx context.Context) ProgressFunc {
	fn, _ := ctx.Value(ctxKeyProgress{}).(ProgressFunc)
	return fn
}

// InheritPipelineContext copies pause gate, progress, and step progress from parent onto child.
func InheritPipelineContext(parent, child context.Context) context.Context {
	if parent == nil || child == nil {
		return child
	}
	if gate := PauseGateFromContext(parent); gate != nil {
		child = WithPauseGate(child, gate)
	}
	if fn := ProgressFromContext(parent); fn != nil {
		child = WithProgress(child, fn)
	}
	if sp, ok := parent.Value(ctxKeyStepProgress{}).(*stepProgress); ok && sp != nil {
		child = context.WithValue(child, ctxKeyStepProgress{}, sp)
	}
	if st := PipelineStatusFromContext(parent); st != nil {
		child = WithPipelineStatus(child, st)
	}
	child = logger.InheritID(parent, child)
	return child
}

// WithProgress attaches a progress callback to context (same log_id chain).
func WithProgress(ctx context.Context, fn ProgressFunc) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyProgress{}, fn)
}

// WithStreamDelta attaches a streaming delta callback to context.
func WithStreamDelta(ctx context.Context, fn StreamDeltaFunc) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyStreamDelta{}, fn)
}

// WithStreamEnd attaches a stream-end callback to context.
func WithStreamEnd(ctx context.Context, fn StreamEndFunc) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyStreamEnd{}, fn)
}

// ReportProgress emits progress if a callback is bound to context.
func ReportProgress(ctx context.Context, step string, progress float32, message string) {
	logger.CtxTrace(ctx, "progress step=%s pct=%.0f msg=%s", step, progress, message)
	if st := PipelineStatusFromContext(ctx); st != nil {
		st.RecordProgress(step, progress, message)
	}
	fn, _ := ctx.Value(ctxKeyProgress{}).(ProgressFunc)
	if fn != nil {
		fn(step, progress, message)
	}
}

// StartProgressHeartbeat re-broadcasts the latest progress every interval so the
// chat bar stays fresh while blocked on long image/video API calls.
// Stops automatically when ctx is cancelled.
func StartProgressHeartbeat(ctx context.Context, interval time.Duration) {
	if ProgressFromContext(ctx) == nil {
		return
	}
	if PipelineStatusFromContext(ctx) == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultProgressHeartbeat
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				st := PipelineStatusFromContext(ctx)
				step, pct, msg, ok := st.HeartbeatPayload()
				if !ok {
					continue
				}
				ReportProgress(ctx, step, pct, msg)
			}
		}
	}()
}

// EnsureProgressHeartbeat attaches a PipelineStatus (if missing) and starts the 5s heartbeat.
func EnsureProgressHeartbeat(ctx context.Context) context.Context {
	if PipelineStatusFromContext(ctx) == nil {
		ctx = WithPipelineStatus(ctx, &PipelineStatus{})
	}
	StartProgressHeartbeat(ctx, DefaultProgressHeartbeat)
	return ctx
}

// WaitWithProgress waits for d while refreshing the chat bar every 5s with remaining time.
// format must contain one %d for remaining seconds, e.g. "冷却中，%d 秒后生成下一镜视频…".
func WaitWithProgress(ctx context.Context, d time.Duration, localPct float32, format string) error {
	if d <= 0 {
		return nil
	}
	if format == "" {
		format = "等待中，还剩 %d 秒…"
	}
	deadline := time.Now().Add(d)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return nil
		}
		sec := int(left.Seconds())
		if sec < 1 {
			sec = 1
		}
		ReportStepProgress(ctx, localPct, fmt.Sprintf(format, sec))
		slice := DefaultProgressHeartbeat
		if left < slice {
			slice = left
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(slice):
		}
	}
}

// ReportStreamDelta emits a streaming text delta if bound to context.
func ReportStreamDelta(ctx context.Context, delta string) {
	if delta == "" {
		return
	}
	fn, _ := ctx.Value(ctxKeyStreamDelta{}).(StreamDeltaFunc)
	if fn != nil {
		fn(delta)
	}
}

// ReportStreamEnd signals streaming completion if bound to context.
func ReportStreamEnd(ctx context.Context) {
	fn, _ := ctx.Value(ctxKeyStreamEnd{}).(StreamEndFunc)
	if fn != nil {
		fn()
	}
}

// TextStreamDelta returns an adapter OnDelta callback wired to context streaming.
func TextStreamDelta(ctx context.Context) func(string) error {
	return func(delta string) error {
		ReportStreamDelta(ctx, delta)
		return nil
	}
}
