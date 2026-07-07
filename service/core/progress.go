package core

import (
	"context"
	"sync"

	"toonflow/logger"
)

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

// Snapshot returns a JSON-friendly copy for WS payloads (nil-safe).
func (s *PipelineStatus) Snapshot() map[string]interface{} {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]interface{}{
		"step_id":     s.stepID,
		"step_label":  s.stepLabel,
		"shot":        s.shot,
		"shot_seq":    s.shotSeq,
		"shot_total":  s.shotTotal,
		"attempt":     s.attempt,
		"max_attempt": s.maxAtt,
		"retry_note":  s.note,
	}
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
	fn, _ := ctx.Value(ctxKeyProgress{}).(ProgressFunc)
	if fn != nil {
		fn(step, progress, message)
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
