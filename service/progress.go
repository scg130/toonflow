package service

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
