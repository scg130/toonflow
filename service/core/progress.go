package core

import (
	"context"

	"toonflow/logger"
)

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
