package service

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
	if IsContentPolicyViolation(errors.New("timeout")) {
		t.Fatal("unexpected policy violation")
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
