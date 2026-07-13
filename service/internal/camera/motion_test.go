package camera

import (
	"strings"
	"testing"
)

func TestMapCameraToVideoMotion_hongguoPunch(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "aggressive"},
		{"特写 推近", "close-up"},
		{"推近 dolly in", "aggressive dolly push-in"},
		{"手持", "handheld"},
		{"仰拍", "low angle"},
	}
	for _, tc := range cases {
		got := MapCameraToVideoMotion(tc.in)
		if !strings.Contains(strings.ToLower(got), strings.ToLower(tc.want)) && !strings.Contains(got, tc.want) {
			t.Fatalf("in=%q got=%q want contain %q", tc.in, got, tc.want)
		}
		if strings.Contains(got, "subtle cinematic") || strings.Contains(got, "slow cinematic dolly") {
			t.Fatalf("soft cinematic default leaked for %q: %q", tc.in, got)
		}
	}
}
