package pipeline

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
	appendPipelineLine(&lines, "正在生成第 2 镜关键帧图片 (1/22)")
	appendPipelineLine(&lines, "正在生成第 2 镜关键帧图片 (1/22) · 已等待 10 秒")
	if len(lines) != 4 {
		t.Fatalf("expected heartbeat replace, got %d: %v", len(lines), lines)
	}
	if lines[3] != "正在生成第 2 镜关键帧图片 (1/22) · 已等待 10 秒" {
		t.Fatalf("got %q", lines[3])
	}
	appendPipelineLine(&lines, "生图刚结束，冷却中，40 秒后开始批量生视频…")
	appendPipelineLine(&lines, "生图刚结束，冷却中，35 秒后开始批量生视频…")
	if len(lines) != 5 {
		t.Fatalf("expected cooldown replace, got %d: %v", len(lines), lines)
	}
}
