package task

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestQueuePerUserConcurrencyLimit(t *testing.T) {
	q := NewQueue(QueueConfig{MaxGlobal: 10, MaxPerUser: 2, MaxHistoryPerUser: 10})
	block := make(chan struct{})
	var started sync.WaitGroup
	started.Add(2)

	for i := 0; i < 2; i++ {
		tk := NewTask(fmt.Sprintf("u1_%d", i), "", "", "", 3, "1280x720", 24, time.Minute)
		tk.UserID = "user1"
		q.Submit(tk, func(ctx context.Context, tk *Task) error {
			started.Done()
			<-block
			return nil
		})
	}

	waitStarted := make(chan struct{})
	go func() {
		started.Wait()
		close(waitStarted)
	}()
	select {
	case <-waitStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("tasks did not start")
	}

	third := NewTask("u1_overflow", "", "", "", 3, "1280x720", 24, time.Minute)
	third.UserID = "user1"
	q.Submit(third, func(ctx context.Context, tk *Task) error { return nil })

	time.Sleep(100 * time.Millisecond)
	close(block)

	for _, item := range q.AllTasksForUser("user1") {
		if item.ID == "u1_overflow" {
			if item.ErrorMessage == "您的并发任务已满，请稍后再试" {
				return
			}
			t.Fatalf("unexpected error: %q", item.ErrorMessage)
		}
	}
	t.Fatal("expected per-user limit error on overflow task")
}

func TestQueueGlobalConcurrencyLimit(t *testing.T) {
	q := NewQueue(QueueConfig{MaxGlobal: 2, MaxPerUser: 5, MaxHistoryPerUser: 10})
	block := make(chan struct{})
	var started sync.WaitGroup
	started.Add(2)

	for i := 0; i < 2; i++ {
		tk := NewTask(fmt.Sprintf("g_%d", i), "", "", "", 3, "1280x720", 24, time.Minute)
		tk.UserID = fmt.Sprintf("user_%d", i)
		q.Submit(tk, func(ctx context.Context, tk *Task) error {
			started.Done()
			<-block
			return nil
		})
	}

	done := make(chan struct{})
	go func() {
		started.Wait()
		close(done)
	}()
	<-done

	overflow := NewTask("g_overflow", "", "", "", 3, "1280x720", 24, time.Minute)
	overflow.UserID = "userZ"
	q.Submit(overflow, func(ctx context.Context, tk *Task) error { return nil })

	time.Sleep(100 * time.Millisecond)
	close(block)

	if _, ok := q.GetTask("g_overflow"); ok {
		t.Fatal("overflow task should not be running")
	}
	for _, item := range q.AllTasksForUser("userZ") {
		if item.ID == "g_overflow" && item.ErrorMessage == "全站并发任务已满，请稍后再试" {
			return
		}
	}
	t.Fatal("expected global limit error on overflow task")
}

func TestQueueHistoryPerUser(t *testing.T) {
	q := NewQueue(QueueConfig{MaxGlobal: 20, MaxPerUser: 20, MaxHistoryPerUser: 3})

	for i := 0; i < 5; i++ {
		tk := NewTask(fmt.Sprintf("h%d", i), "", "", "", 3, "1280x720", 24, time.Minute)
		tk.UserID = "alice"
		tk.SetState(StateDone, "done")
		q.mu.Lock()
		q.addHistoryLocked(tk)
		q.mu.Unlock()
	}

	alice := q.AllTasksForUser("alice")
	if len(alice) != 3 {
		t.Fatalf("alice history want 3 got %d", len(alice))
	}

	bobTask := NewTask("b1", "", "", "", 3, "1280x720", 24, time.Minute)
	bobTask.UserID = "bob"
	bobTask.SetState(StateDone, "done")
	q.mu.Lock()
	q.addHistoryLocked(bobTask)
	q.mu.Unlock()

	bob := q.AllTasksForUser("bob")
	if len(bob) != 1 {
		t.Fatalf("bob history want 1 got %d", len(bob))
	}
}
