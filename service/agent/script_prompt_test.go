package agent

import (
	"strings"
	"testing"

	"toonflow/service/project"
)

func TestAIShortDramaScriptPrompts(t *testing.T) {
	sys := AIShortDramaScriptSystemPrompt()
	for _, kw := range []string{"5分钟", "开场钩子", "18–25", "下集钩子"} {
		if !strings.Contains(sys, kw) {
			t.Fatalf("system prompt missing %q", kw)
		}
	}
	user := AIShortDramaScriptUserPrompt("第一集", project.EpisodeParams{
		TargetDurationMin: 5,
		TargetWords:       900,
		VideoRatio:        "9:16",
		ArtStyle:          "3D_anime_render",
	}, "context", "skeleton", "strategy")
	if !strings.Contains(user, "分场剧本") || !strings.Contains(user, "本集爽点") {
		t.Fatalf("user prompt missing format sections: %s", user[:min(200, len(user))])
	}
}

func TestSkeletonStrategyPrompts(t *testing.T) {
	if !strings.Contains(skeletonPrompt(), "六段节奏") {
		t.Fatal("skeleton prompt missing six-act anchors")
	}
	if !strings.Contains(strategyPrompt(), "18–25") {
		t.Fatal("strategy prompt missing shot quota")
	}
}
