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
		{"", "push-in"},
		{"特写 推近", "close-up"},
		{"推近 dolly in", "dolly push-in"},
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
		lower := strings.ToLower(got)
		for _, bad := range []string{"emotion", "emotional", "intensity", "rising emotion", "arri", "masterpiece", "8k"} {
			if strings.Contains(lower, bad) {
				t.Fatalf("opaque/slop word %q in camera prompt for %q: %q", bad, tc.in, got)
			}
		}
	}
}

func TestMapCameraToVideoMotion_namedLexicon(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"横移揭示伏兵", "horizontal dolly"},
		{"慢推压迫脸部", "slow continuous push-in"},
		{"骤停定格剑尖", "hard freeze"},
		{"手持奔逃穿过人群", "handheld running"},
		{"长廊后退人物追镜头", "track backward"},
		{"低空掠行贴地", "near-ground rush"},
		{"战场俯扫", "sweeping pan"},
		{"顶视包围", "top-down"},
	}
	for _, tc := range cases {
		got := MapCameraToVideoMotion(tc.in)
		if !strings.Contains(strings.ToLower(got), strings.ToLower(tc.want)) {
			t.Fatalf("in=%q got=%q want contain %q", tc.in, got, tc.want)
		}
	}
	// Specific named move must win over bare 手持.
	got := MapCameraToVideoMotion("手持奔逃 中景")
	if !strings.Contains(got, "running") {
		t.Fatalf("手持奔逃 should map to running follow, got %q", got)
	}
	if strings.Contains(got, "micro-shake increases as subject steps forward") && !strings.Contains(got, "running") {
		t.Fatalf("generic handheld leaked over 手持奔逃: %q", got)
	}
}

func TestMicroExpressionMotion(t *testing.T) {
	if got := MicroExpressionMotion("画面：女主近景。动作：瞳孔收缩。"); !strings.Contains(got, "pupils") {
		t.Fatalf("pupil micro missing: %q", got)
	}
	if got := MicroExpressionMotion("拳心收紧指节发白"); !strings.Contains(got, "fist clenches") {
		t.Fatalf("fist micro missing: %q", got)
	}
	if MicroExpressionMotion("两人中景对峙站桩") != "" {
		t.Fatal("plain stand-off should not invent micro-expression")
	}
}
