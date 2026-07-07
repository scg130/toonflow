package media

import (
	"testing"
	"time"
)

func TestBatchVideoTaskTimeout(t *testing.T) {
	if BatchVideoTaskTimeout(1) < 30*time.Minute {
		t.Fatal("single shot batch should be at least 30m")
	}
	got := BatchVideoTaskTimeout(30)
	if got != 12*time.Hour {
		t.Fatalf("large batch capped at 12h, got %v", got)
	}
}
