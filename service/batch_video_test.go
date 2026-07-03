package service

import (
	"fmt"
	"testing"
	"time"
)

func TestBatchVideoTaskTimeout(t *testing.T) {
	if BatchVideoTaskTimeout(1) < 30*time.Minute {
		t.Fatal("single shot batch should be at least 30m")
	}
	got := BatchVideoTaskTimeout(9)
	if got != 3*time.Hour {
		t.Fatalf("9 shots capped at 3h, got %v", got)
	}
}

func TestIsRetryableVideoErr(t *testing.T) {
	if !isRetryableVideoErr(fmt.Errorf("agnes video poll error 429")) {
		t.Fatal("429 should retry")
	}
	if isRetryableVideoErr(fmt.Errorf("content policy violation")) {
		t.Fatal("policy should not retry")
	}
}
