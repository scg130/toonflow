package service

import "testing"

func TestAppendPipelineLine(t *testing.T) {
	lines := []string{"🚀 已开始"}
	appendPipelineLine(&lines, "正在生成分镜...")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	appendPipelineLine(&lines, "正在批量生图...")
	if len(lines) != 2 || lines[1] != "正在批量生图..." {
		t.Fatalf("expected replace in-progress line, got %v", lines)
	}
	appendPipelineLine(&lines, "第 1 镜图片完成 (1/15)")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	appendPipelineLine(&lines, "第 1 镜图片完成 (1/15)")
	if len(lines) != 3 {
		t.Fatalf("expected dedupe, got %v", lines)
	}
}
