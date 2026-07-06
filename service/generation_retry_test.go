package service

import (
	"testing"
	"time"
)

func TestRetryBackoffDelay(t *testing.T) {
	if RetryBackoffDelay(0) != 0 {
		t.Fatal("first attempt should not wait")
	}
	if RetryBackoffDelay(1) != 5*time.Second {
		t.Fatal("second backoff")
	}
	if RetryBackoffDelay(100) != 120*time.Second {
		t.Fatal("cap at 120s")
	}
}

func TestPromptForImageAttemptTiers(t *testing.T) {
	base := "hero with 鲜血 and blood"
	orig := PromptForImageAttempt(base, 0)
	if orig != base {
		t.Fatal("early attempts use original prompt")
	}
	light := PromptForImageAttempt(base, 4)
	if light == base || light == "" {
		t.Fatalf("mid attempts sanitize: %q", light)
	}
	strict := PromptForImageAttempt(base, 10)
	if strict == light {
		t.Fatal("late attempts use stricter sanitize")
	}
}
