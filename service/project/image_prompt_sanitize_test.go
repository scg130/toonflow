package project

import (
	"errors"
	"strings"
	"testing"
)

func TestIsContentPolicyViolation(t *testing.T) {
	err := errors.New(`agnes image error 400: {"error":{"code":"content_policy_violation"}}`)
	if !IsContentPolicyViolation(err) {
		t.Fatal("expected policy violation")
	}
	unable := errors.New(`agnes image error 400: {"error":{"message":"Unable to generate this content. Please modify your prompt and try again."}}`)
	if !IsContentPolicyViolation(unable) {
		t.Fatal("expected unable-to-generate as policy")
	}
	if IsContentPolicyViolation(errors.New("timeout")) {
		t.Fatal("unexpected policy violation")
	}
}

func TestBuildSafeImagePromptFallback(t *testing.T) {
	out := BuildSafeImagePromptFallback("石昊猛然起身，赤红双目，鲜血飞溅，blood on sword, asset consistency: 染血杀意")
	if out == "" {
		t.Fatal("empty fallback")
	}
	lower := strings.ToLower(out)
	if strings.Contains(out, "鲜血") || strings.Contains(out, "染血") || strings.Contains(lower, "blood") {
		t.Fatalf("fallback still risky: %q", out)
	}
	if !strings.Contains(lower, "no graphic violence") {
		t.Fatalf("missing safety anchors: %q", out)
	}
}

func TestBuildUltraMinimalSafeImagePrompt(t *testing.T) {
	out := BuildUltraMinimalSafeImagePrompt("close-up mouth open wide shouting, blood-stained robes, asset consistency: 染血杀意")
	lower := strings.ToLower(out)
	if strings.Contains(lower, "blood") || strings.Contains(out, "染血") {
		t.Fatalf("ultra still risky: %q", out)
	}
	if !strings.Contains(lower, "family friendly") {
		t.Fatalf("missing anchors: %q", out)
	}
	if !strings.Contains(lower, "close-up") {
		t.Fatalf("should keep camera cue: %q", out)
	}
}

func TestExtractVisualActionCore(t *testing.T) {
	in := "medium shot back view, black hair, vertical 9:16, asset consistency: 角色「石昊」染血"
	got := ExtractVisualActionCore(in)
	if strings.Contains(got, "asset consistency") || strings.Contains(got, "染血") {
		t.Fatalf("should strip asset block: %q", got)
	}
	if !strings.Contains(got, "medium shot") {
		t.Fatalf("should keep action: %q", got)
	}
}

func TestSanitizeImagePromptForPolicy(t *testing.T) {
	in := "石昊猛然起身，赤红双目，鲜血飞溅，染血长袍杀意魔化，blood-stained white robes, upward pose"
	out := SanitizeImagePromptForPolicy(in, SanitizeLevelLight)
	lower := strings.ToLower(out)
	if strings.Contains(lower, "blood") || strings.Contains(out, "鲜血") || strings.Contains(out, "染血") {
		t.Fatalf("light sanitize left extreme terms: %q", out)
	}
	// 杀意 / 魔化 等剧情词不应被改写
	if !strings.Contains(out, "杀意") || !strings.Contains(out, "魔化") {
		t.Fatalf("light sanitize should keep dramatic words like 杀意/魔化: %q", out)
	}
	if !strings.Contains(out, "upward") {
		t.Fatalf("light sanitize should not corrupt upward: %q", out)
	}
	strict := SanitizeImagePromptForPolicy(in, SanitizeLevelStrict)
	if reChinese.MatchString(strict) {
		t.Fatalf("strict sanitize should remove Chinese: %q", strict)
	}
}
