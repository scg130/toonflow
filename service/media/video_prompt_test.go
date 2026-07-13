package media

import (
	"strings"
	"testing"

	"toonflow/service/storyboard"
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

func TestBuildShotVideoPrompt_hongguoStyle(t *testing.T) {
	shot := &storyboard.ShotMeta{
		Description: "【目标】石昊发现柳神消失。【承接】开场。【结果】他愣住。",
		Camera:      "特写 推近",
		Lighting:    "冷黄黄昏飞灰",
		Prompt:      "Unreal Engine 5 render, Octane, long static render tags...",
		Beats: []task.ShotBeat{
			{Time: 0, Action: "画面：跪地扶树桩。动作：双手抓紧。反应：低头。"},
			{Time: 5, Action: "画面：特写双手。动作：树桩化灰散落。反应：手指僵住。"},
		},
		Duration: 10,
	}
	pos, neg := buildShotVideoPrompt(shot, "3D动漫", "", "Unreal Engine 5, Octane, global style embedding locked", true)
	if strings.Contains(pos, "【目标】") {
		t.Fatalf("literary labels should be stripped: %q", pos)
	}
	if strings.Contains(pos, "Unreal Engine") || strings.Contains(pos, "global style embedding") {
		t.Fatalf("still-image style anchor must not leak into I2V: %q", pos)
	}
	if !strings.Contains(pos, "Hongguo") && !strings.Contains(pos, "short drama") {
		t.Fatalf("hongguo short-drama tags missing: %q", pos)
	}
	if !strings.Contains(pos, "timed action") {
		t.Fatalf("beat motion plan missing: %q", pos)
	}
	if !strings.Contains(pos, "close-up") && !strings.Contains(pos, "push") {
		t.Fatalf("punchy camera missing: %q", pos)
	}
	if !strings.Contains(neg, "vague mood") {
		t.Fatalf("negative should reject mood-only shots: %q", neg)
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

func TestBuildShotVideoPrompt_rejectsJunkDialogue(t *testing.T) {
	shot := &storyboard.ShotMeta{
		Description: "石昊跪在树桩前",
		Camera:      "特写",
		Dialogue: &task.ShotDialogue{Lines: []task.DialogueLine{
			{Speaker: "柳神", Text: "4565"},
			{Speaker: "鸿帝", Text: "654"},
			{Speaker: "石昊", Text: "柳神……"},
		}},
	}
	pos, _ := buildShotVideoPrompt(shot, "3D动漫", "", "", true)
	if strings.Contains(pos, "4565") || strings.Contains(pos, "654") {
		t.Fatalf("junk digit dialogue leaked into I2V prompt: %q", pos)
	}
	if !strings.Contains(pos, "柳神……") {
		t.Fatalf("valid Chinese dialogue missing: %q", pos)
	}
}

func TestCompressDescriptionForVideo(t *testing.T) {
	got := compressDescriptionForVideo("【目标】石昊发现柳神消失。【承接】开场跪地。【结果】手指僵住。")
	if strings.Contains(got, "【") {
		t.Fatalf("labels remain: %q", got)
	}
	if !strings.Contains(got, "石昊") {
		t.Fatalf("event lost: %q", got)
	}
}
