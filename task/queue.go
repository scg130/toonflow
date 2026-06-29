package task

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Queue manages concurrent task execution.
type Queue struct {
	mu         sync.Mutex
	tasks      map[string]*Task
	history    []*Task
	maxHistory int
	done       chan *Task
	maxConc    int
}

// NewQueue creates a new task queue.
func NewQueue(maxConcurrency int) *Queue {
	return &Queue{
		tasks:      make(map[string]*Task),
		history:    make([]*Task, 0, 64),
		maxHistory: 100,
		done:       make(chan *Task, 100),
		maxConc:    maxConcurrency,
	}
}

// Submit runs the function in a goroutine with concurrency limiting.
func (q *Queue) Submit(t *Task, fn func(context.Context, *Task) error) {
	q.mu.Lock()
	q.tasks[t.ID] = t
	active := len(q.tasks)
	q.mu.Unlock()

	if active > q.maxConc {
		t.SetError("max concurrent tasks reached")
		delete(q.tasks, t.ID)
		q.addHistoryLocked(t)
		q.mu.Unlock()
		q.done <- t
		return
	}

	go func() {
		defer func() {
			q.mu.Lock()
			delete(q.tasks, t.ID)
			q.addHistoryLocked(t)
			q.mu.Unlock()
			q.done <- t
		}()

		if err := fn(t.Context(), t); err != nil {
			if t.IsTimeout() {
				t.SetError("task timed out")
			} else {
				t.SetError(err.Error())
			}
		}
	}()
}

// WaitDone returns the completion channel.
func (q *Queue) WaitDone() <-chan *Task {
	return q.done
}

// ActiveCount returns the number of running tasks.
func (q *Queue) ActiveCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.tasks)
}

// GetTask returns a task by ID.
func (q *Queue) GetTask(id string) (*Task, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	t, ok := q.tasks[id]
	return t, ok
}

// AllTasks returns copies of active and recent completed tasks.
func (q *Queue) AllTasks() []*Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	result := make([]*Task, 0, len(q.tasks)+len(q.history))
	for _, t := range q.history {
		result = append(result, t.Clone())
	}
	for _, t := range q.tasks {
		result = append(result, t.Clone())
	}
	return result
}

func (q *Queue) addHistoryLocked(t *Task) {
	q.history = append(q.history, t.Clone())
	if len(q.history) > q.maxHistory {
		q.history = q.history[len(q.history)-q.maxHistory:]
	}
}

// RetryableError is an error that can be retried.
type RetryableError struct{ Err error }

func (e *RetryableError) Error() string { return fmt.Sprintf("retryable: %v", e.Err) }
func (e *RetryableError) Unwrap() error { return e.Err }

// NonRetryableError is an error that should not be retried.
type NonRetryableError struct{ Err error }

func (e *NonRetryableError) Error() string { return fmt.Sprintf("non-retryable: %v", e.Err) }
func (e *NonRetryableError) Unwrap() error { return e.Err }

// WithRetry wraps a function with exponential backoff.
func WithRetry(ctx context.Context, maxRetries int, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if _, ok := lastErr.(*NonRetryableError); ok {
			return lastErr
		}
		if attempt < maxRetries {
			dur := 500 * time.Millisecond * time.Duration(1<<uint(attempt))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(dur):
			}
		}
	}
	return fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}
