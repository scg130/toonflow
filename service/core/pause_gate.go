package core

import (
	"context"
	"sync"
)

type ctxKeyPauseGate struct{}

// PauseGate supports cooperative pause/resume for long episode pipelines.
type PauseGate struct {
	mu       sync.Mutex
	paused   bool
	resumeCh chan struct{}
}

// NewPauseGate creates an initially running gate.
func NewPauseGate() *PauseGate {
	return &PauseGate{resumeCh: make(chan struct{}, 1)}
}

// WithPauseGate attaches a pause gate to context.
func WithPauseGate(ctx context.Context, gate *PauseGate) context.Context {
	if gate == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyPauseGate{}, gate)
}

// PauseGateFromContext returns the gate bound to ctx, if any.
func PauseGateFromContext(ctx context.Context) *PauseGate {
	g, _ := ctx.Value(ctxKeyPauseGate{}).(*PauseGate)
	return g
}

// WaitIfPaused blocks until resumed or ctx is cancelled.
func WaitIfPaused(ctx context.Context) error {
	g := PauseGateFromContext(ctx)
	if g == nil {
		return nil
	}
	return g.Wait(ctx)
}

// Pause marks the gate paused.
func (g *PauseGate) Pause() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.paused = true
}

// Resume unblocks waiters.
func (g *PauseGate) Resume() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.paused {
		return
	}
	g.paused = false
	select {
	case g.resumeCh <- struct{}{}:
	default:
	}
}

// IsPaused reports whether the gate is paused.
func (g *PauseGate) IsPaused() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.paused
}

// Wait blocks while paused.
func (g *PauseGate) Wait(ctx context.Context) error {
	for {
		g.mu.Lock()
		if !g.paused {
			g.mu.Unlock()
			return nil
		}
		ch := g.resumeCh
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
	}
}
