package task

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const anonymousUserID = "_anonymous"

// QueueConfig limits task concurrency and history retention.
type QueueConfig struct {
	MaxGlobal         int // site-wide concurrent cap
	MaxPerUser        int // per-user concurrent cap
	MaxHistoryPerUser int // completed tasks kept per user
}

// DefaultQueueConfig returns multi-user friendly defaults.
func DefaultQueueConfig() QueueConfig {
	return QueueConfig{
		MaxGlobal:         10,
		MaxPerUser:        3,
		MaxHistoryPerUser: 50,
	}
}

// Queue manages concurrent task execution.
type Queue struct {
	mu                sync.Mutex
	tasks             map[string]*Task
	history           map[string][]*Task // userID -> recent tasks
	maxGlobal         int
	maxPerUser        int
	maxHistoryPerUser int
	done              chan *Task
}

// NewQueue creates a queue with the given limits.
func NewQueue(cfg QueueConfig) *Queue {
	if cfg.MaxGlobal <= 0 {
		cfg.MaxGlobal = DefaultQueueConfig().MaxGlobal
	}
	if cfg.MaxPerUser <= 0 {
		cfg.MaxPerUser = DefaultQueueConfig().MaxPerUser
	}
	if cfg.MaxHistoryPerUser <= 0 {
		cfg.MaxHistoryPerUser = DefaultQueueConfig().MaxHistoryPerUser
	}
	return &Queue{
		tasks:             make(map[string]*Task),
		history:           make(map[string][]*Task),
		maxGlobal:         cfg.MaxGlobal,
		maxPerUser:        cfg.MaxPerUser,
		maxHistoryPerUser: cfg.MaxHistoryPerUser,
		done:              make(chan *Task, 100),
	}
}

func taskUserID(t *Task) string {
	if t == nil || t.UserID == "" {
		return anonymousUserID
	}
	return t.UserID
}

// Submit runs the function in a goroutine with per-user and global concurrency limits.
func (q *Queue) Submit(t *Task, fn func(context.Context, *Task) error) {
	q.mu.Lock()
	q.tasks[t.ID] = t
	globalActive := len(q.tasks)
	userActive := q.activeCountForLocked(taskUserID(t))
	q.mu.Unlock()

	if globalActive > q.maxGlobal {
		q.rejectTask(t, "全站并发任务已满，请稍后再试")
		return
	}
	if userActive > q.maxPerUser {
		q.rejectTask(t, "您的并发任务已满，请稍后再试")
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

func (q *Queue) rejectTask(t *Task, msg string) {
	t.SetError(msg)
	q.mu.Lock()
	delete(q.tasks, t.ID)
	q.addHistoryLocked(t)
	q.mu.Unlock()
	q.done <- t
}

// WaitDone returns the completion channel.
func (q *Queue) WaitDone() <-chan *Task {
	return q.done
}

// ActiveCount returns the number of running tasks (global).
func (q *Queue) ActiveCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.tasks)
}

// ActiveCountForUser returns running tasks for one user.
func (q *Queue) ActiveCountForUser(userID string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.activeCountForLocked(normalizeUserID(userID))
}

func normalizeUserID(userID string) string {
	if userID == "" {
		return anonymousUserID
	}
	return userID
}

func (q *Queue) activeCountForLocked(userID string) int {
	n := 0
	for _, t := range q.tasks {
		if taskUserID(t) == userID {
			n++
		}
	}
	return n
}

// GetTask returns a task by ID.
func (q *Queue) GetTask(id string) (*Task, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	t, ok := q.tasks[id]
	return t, ok
}

// AllTasksForUser returns active and recent tasks belonging to userID, newest first.
func (q *Queue) AllTasksForUser(userID string) []*Task {
	if userID == "" {
		return nil
	}
	uid := normalizeUserID(userID)

	q.mu.Lock()
	defer q.mu.Unlock()

	result := make([]*Task, 0, len(q.tasks)+q.maxHistoryPerUser)
	for _, t := range q.history[uid] {
		result = append(result, t.Clone())
	}
	for _, t := range q.tasks {
		if taskUserID(t) == uid {
			result = append(result, t.Clone())
		}
	}
	sortTasksByTime(result)
	return result
}

func sortTasksByTime(tasks []*Task) {
	for i := 0; i < len(tasks); i++ {
		for j := i + 1; j < len(tasks); j++ {
			if tasks[j].CreatedAt.After(tasks[i].CreatedAt) {
				tasks[i], tasks[j] = tasks[j], tasks[i]
			}
		}
	}
}

// AllTasks returns copies of active and recent completed tasks.
func (q *Queue) AllTasks() []*Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	result := make([]*Task, 0, len(q.tasks)+64)
	for _, hist := range q.history {
		for _, t := range hist {
			result = append(result, t.Clone())
		}
	}
	for _, t := range q.tasks {
		result = append(result, t.Clone())
	}
	return result
}

func (q *Queue) addHistoryLocked(t *Task) {
	uid := taskUserID(t)
	q.history[uid] = append(q.history[uid], t.Clone())
	hist := q.history[uid]
	if len(hist) > q.maxHistoryPerUser {
		q.history[uid] = hist[len(hist)-q.maxHistoryPerUser:]
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
