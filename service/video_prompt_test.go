package service

import (
	"strings"
	"testing"
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
	shot := &storyboardShot{
		Description: "石昊猛然起身，赤红双目",
		Camera:      "推镜 dolly in",
		Prompt:      "Unreal Engine 5 render, Octane, long static render tags...",
	}
	pos, neg := buildShotVideoPrompt(shot, "3D动漫", "", "")
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
