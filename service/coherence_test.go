package service

import (
	"strings"
	"testing"
)

func TestBuildStyleAnchor(t *testing.T) {
	anchor := BuildStyleAnchor("3D_anime_render", "16:9", "warm cinematic grade")
	if anchor == "" {
		t.Fatal("empty anchor")
	}
	lower := strings.ToLower(anchor)
	for _, want := range []string{
		"global style embedding locked",
		"zero model mutation",
		"unified cinematic color grade",
		"warm cinematic grade",
	} {
		if !strings.Contains(lower, strings.ToLower(want)) {
			t.Fatalf("anchor missing %q: %s", want, anchor)
		}
	}
}

func TestScoreMetadataDimensions(t *testing.T) {
	shot := &storyboardShot{
		Prompt:         "character_id: hero, style: consistent, frame-to-frame continuity",
		ActionContinue: "hero raises hand",
		Transition:     "match cut to close-up",
		Lighting:       "warm golden key light",
	}
	sem, trans, style := scoreMetadataDimensions(shot)
	if sem < 75 || trans < 75 || style < 75 {
		t.Fatalf("expected high metadata scores, got sem=%.0f trans=%.0f style=%.0f", sem, trans, style)
	}
}

func TestCoherenceDimensionCount(t *testing.T) {
	if len(coherenceDimensionKeys) != 14 {
		t.Fatalf("expected 14 dimensions, got %d", len(coherenceDimensionKeys))
	}
}

func TestClampScore(t *testing.T) {
	if clampScore(-5) != 0 || clampScore(150) != 100 || clampScore(72.3) != 72.3 {
		t.Fatal("clampScore bounds wrong")
	}
}
