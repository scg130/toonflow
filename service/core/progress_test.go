package core

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"toonflow/logger"
)

func TestInheritPipelineContextCopiesLogID(t *testing.T) {
	parent := logger.WithID(context.Background(), "wf_test123")
	child := InheritPipelineContext(parent, context.Background())
	if got := logger.IDFromContext(child); got != "wf_test123" {
		t.Fatalf("log_id=%q want wf_test123", got)
	}
}

func TestReportStepProgress(t *testing.T) {
	var gotStep string
	var gotPct float32
	var gotMsg string
	ctx := WithProgress(context.Background(), func(step string, pct float32, msg string) {
		gotStep = step
		gotPct = pct
		gotMsg = msg
	})
	ctx = WithStepProgress(ctx, "batch_generate_shot_images", 20, 70)
	ReportStepProgress(ctx, 50, "正在生成第 3 镜图片 (2/5)")
	if gotStep != "batch_generate_shot_images" {
		t.Fatalf("step=%q", gotStep)
	}
	if gotPct != 55 {
		t.Fatalf("pct=%v want 55", gotPct)
	}
	if gotMsg == "" {
		t.Fatal("expected message")
	}
}

func TestProgressHeartbeat(t *testing.T) {
	st := &PipelineStatus{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = WithPipelineStatus(ctx, st)
	var msgs []string
	var mu sync.Mutex
	ctx = WithProgress(ctx, func(_ string, _ float32, msg string) {
		mu.Lock()
		msgs = append(msgs, msg)
		mu.Unlock()
	})
	ReportProgress(ctx, "batch_generate_shot_images", 40, "正在生成第 2 镜关键帧图片 (1/22)")
	st.mu.Lock()
	st.phaseAt = time.Now().Add(-6 * time.Second)
	st.mu.Unlock()
	StartProgressHeartbeat(ctx, 50*time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(msgs)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	cancel()
	mu.Lock()
	defer mu.Unlock()
	if len(msgs) < 2 {
		t.Fatalf("expected heartbeat refresh, got %v", msgs)
	}
	found := false
	for _, m := range msgs {
		if strings.Contains(m, "已等待") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 已等待 in %v", msgs)
	}
}

func TestStripWaitSuffix(t *testing.T) {
	in := "正在生成第 2 镜关键帧图片 (1/22) · 重试 1/4 · 已等待 12 秒"
	got := stripWaitSuffix(in)
	want := "正在生成第 2 镜关键帧图片 (1/22)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
