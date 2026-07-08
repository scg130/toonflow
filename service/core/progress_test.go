package core

import (
	"context"
	"testing"

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
