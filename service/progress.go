package service

import (
	"context"

	"toonflow/logger"
)

// ProgressFunc reports step progress (0-100) during long operations.
type ProgressFunc func(step string, progress float32, message string)

type ctxKeyProgress struct{}

// WithProgress attaches a progress callback to context (same log_id chain).
func WithProgress(ctx context.Context, fn ProgressFunc) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyProgress{}, fn)
}

// ReportProgress emits progress if a callback is bound to context.
func ReportProgress(ctx context.Context, step string, progress float32, message string) {
	logger.CtxTrace(ctx, "progress step=%s pct=%.0f msg=%s", step, progress, message)
	fn, _ := ctx.Value(ctxKeyProgress{}).(ProgressFunc)
	if fn != nil {
		fn(step, progress, message)
	}
}
