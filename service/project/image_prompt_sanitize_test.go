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
	out := BuildSafeImagePromptFallback("石昊猛然起身，赤红双目，鲜血飞溅，blood on sword")
	if out == "" {
		t.Fatal("empty fallback")
	}
	lower := strings.ToLower(out)
	if strings.Contains(out, "鲜血") || strings.Contains(lower, "blood on") || strings.Contains(lower, "bloody") {
		t.Fatalf("fallback still risky: %q", out)
	}
	if !strings.Contains(lower, "no graphic violence") {
		t.Fatalf("missing safety anchors: %q", out)
	}
}

func TestSanitizeImagePromptForPolicy(t *testing.T) {
	in := "石昊猛然起身，赤红双目，鲜血飞溅，blood on sword"
	out := SanitizeImagePromptForPolicy(in, SanitizeLevelLight)
	if strings.Contains(strings.ToLower(out), "blood") || strings.Contains(out, "鲜血") {
		t.Fatalf("light sanitize left risky terms: %q", out)
	}
	strict := SanitizeImagePromptForPolicy(in, SanitizeLevelStrict)
	if reChinese.MatchString(strict) {
		t.Fatalf("strict sanitize should remove Chinese: %q", strict)
	}
}
