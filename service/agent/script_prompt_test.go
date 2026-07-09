package agent

import (
	"strings"
	"testing"

	"toonflow/service/project"
)

func TestAIShortDramaScriptPrompts(t *testing.T) {
	sys := AIShortDramaScriptSystemPrompt()
	for _, kw := range []string{"10 字", "1–3 秒", "威压", "炸裂开局", "光影"} {
		if !strings.Contains(sys, kw) {
			t.Fatalf("system prompt missing %q", kw)
		}
	}
	user := AIShortDramaScriptUserPrompt("第一集", project.EpisodeParams{
		TargetDurationMin: 2,
		TargetWords:       800,
		VideoRatio:        "9:16",
		ArtStyle:          "3D_anime_render",
	}, "context", "skeleton", "strategy")
	if !strings.Contains(user, "【镜头N") || !strings.Contains(user, "≤10字") {
		t.Fatalf("user prompt missing format hints: %s", user[:200])
	}
}

func TestSkeletonStrategyPrompts(t *testing.T) {
	if !strings.Contains(skeletonPrompt(), "开篇钩子") {
		t.Fatal("skeleton prompt missing hook")
	}
	if !strings.Contains(strategyPrompt(), "抽象→具象") {
		t.Fatal("strategy prompt missing abstract mapping")
	}
}
