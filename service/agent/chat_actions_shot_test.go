package agent

import "testing"

func TestInferShotNumberFromUserMessage(t *testing.T) {
	cases := []struct {
		msg  string
		want int
		ok   bool
	}{
		{"为第二镜生成图片", 2, true},
		{"让第2镜生成图片", 2, true},
		{"第 3 镜 出图", 3, true},
		{"镜5生成", 5, true},
		{"生成图片", 0, false},
	}
	for _, tc := range cases {
		got, ok := inferShotNumberFromUserMessage(tc.msg)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("inferShotNumberFromUserMessage(%q) = (%d, %v), want (%d, %v)", tc.msg, got, ok, tc.want, tc.ok)
		}
	}
}

func TestIsExplicitExecutionRequest(t *testing.T) {
	explicit := []string{
		"帮我生成故事骨架",
		"现在生成分镜",
		"为第二镜生成图片",
		"重新生成剧本吗",
		"请提取资产",
	}
	for _, msg := range explicit {
		if !IsExplicitExecutionRequest(msg) {
			t.Fatalf("IsExplicitExecutionRequest(%q) = false, want true", msg)
		}
	}
	implicit := []string{
		"你对这个剧本有什么看法",
		"改编策略是什么",
		"分镜很重要",
		"删除第一镜的视频",
	}
	for _, msg := range implicit {
		if IsExplicitExecutionRequest(msg) {
			t.Fatalf("IsExplicitExecutionRequest(%q) = true, want false", msg)
		}
	}
}

func TestShouldBlockChatAction(t *testing.T) {
	blocked := []string{
		"你对这个剧本有什么看法",
		"删除第一镜的视频v1",
		"这样改好不好？",
		"分镜是什么？",
	}
	for _, msg := range blocked {
		if !ShouldBlockChatAction(msg) {
			t.Fatalf("ShouldBlockChatAction(%q) = false, want true", msg)
		}
	}
	allowed := []string{
		"帮我生成故事骨架",
		"现在生成分镜",
		"为第二镜生成图片",
		"重新生成剧本吗",
	}
	for _, msg := range allowed {
		if ShouldBlockChatAction(msg) {
			t.Fatalf("ShouldBlockChatAction(%q) = true, want false", msg)
		}
	}
}

func TestEnrichIntentFromUserMessage(t *testing.T) {
	intent := &ChatActionIntent{Type: "generate_shot_image"}
	EnrichIntentFromUserMessage(intent, "为第二镜生成图片")
	if intent.Params["shot_number"] != "2" {
		t.Fatalf("shot_number = %q, want 2", intent.Params["shot_number"])
	}
}
