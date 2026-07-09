package media

import (
	"toonflow/service/storyboard"
	"strings"
	"testing"

	"toonflow/task"
)

func TestTrimImagePromptForVideo(t *testing.T) {
	in := "3D anime, character_id: hero, Unreal Engine 5 render, Octane Render, dolly in, hero stands up angrily, ambient occlusion"
	got := trimImagePromptForVideo(in)
	if got == "" {
		t.Fatal("expected non-empty trim")
	}
	if strings.Contains(got, "Unreal") || strings.Contains(got, "Octane") || strings.Contains(got, "character_id") {
		t.Fatalf("render jargon not stripped: %q", got)
	}
	if !strings.Contains(got, "dolly in") && !strings.Contains(got, "hero stands") {
		t.Fatalf("motion content lost: %q", got)
	}
}

func TestBuildShotVideoPrompt_motionFirst(t *testing.T) {
	shot := &storyboard.ShotMeta{
		Description: "石昊猛然起身，赤红双目",
		Camera:      "推镜 dolly in",
		Prompt:      "Unreal Engine 5 render, Octane, long static render tags...",
	}
	pos, neg := buildShotVideoPrompt(shot, "3D动漫", "", "", true)
	if !strings.Contains(pos, "石昊") {
		t.Fatalf("description missing: %q", pos)
	}
	if !strings.Contains(pos, "dolly") {
		t.Fatalf("camera motion missing: %q", pos)
	}
	if strings.Contains(pos, "Unreal Engine") {
		t.Fatalf("image render tags should not appear: %q", pos)
	}
	if neg == "" {
		t.Fatal("negative prompt empty")
	}
}

func TestBuildShotVideoPrompt_withDialogue(t *testing.T) {
	shot := &storyboard.ShotMeta{
		Description: "石昊怒视前方",
		Dialogue:    &task.ShotDialogue{Lines: []task.DialogueLine{{Speaker: "石昊", Text: "你们欺人太甚！"}}},
		Camera:      "近景",
	}
	pos, neg := buildShotVideoPrompt(shot, "3D动漫", "", "", true)
	if !strings.Contains(pos, "石昊") {
		t.Fatalf("speaker missing: %q", pos)
	}
	if !strings.Contains(pos, "欺人太甚") {
		t.Fatalf("dialogue line missing: %q", pos)
	}
	if !strings.Contains(pos, "口型") {
		t.Fatalf("lip sync instruction missing: %q", pos)
	}
	if strings.Contains(pos, "dialogue performance") {
		t.Fatalf("should not use English dialogue performance hint: %q", pos)
	}
	if !strings.Contains(neg, "no lip sync") {
		t.Fatalf("negative lip sync guard missing: %q", neg)
	}
	if !strings.Contains(neg, "English speech") {
		t.Fatalf("negative English speech guard missing: %q", neg)
	}
}

func TestBuildShotVideoPrompt_withDialogue_nonHuman(t *testing.T) {
	shot := &storyboard.ShotMeta{
		Description: "焦黑树桩在雷光中颤动",
		Dialogue:    &task.ShotDialogue{Lines: []task.DialogueLine{{Speaker: "旁白", Text: "天地变色"}}},
	}
	pos, _ := buildShotVideoPrompt(shot, "3D动漫", "", "", false)
	if strings.Contains(pos, "lip sync") {
		t.Fatalf("non-human shot should not include lip sync: %q", pos)
	}
}
