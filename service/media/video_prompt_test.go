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
	if !strings.Contains(pos, "frames2video") && !strings.Contains(pos, "FLF2V") && !strings.Contains(pos, "multiframe motion") && !strings.Contains(pos, "image-to-video") {
		t.Fatalf("inter-keyframe motion plan missing: %q", pos)
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

func TestBuildShotVideoPrompt_frames2InterKeyframe(t *testing.T) {
	shot := &storyboard.ShotMeta{
		Description: "【目标】交代身份。【承接】开场。【结果】女主上场。",
		Camera:      "中景稳镜",
		Beats: []task.ShotBeat{
			{Time: 0, Action: "画面：双人中景。动作：女主抬下巴。反应：男主沉默。"},
			{Time: 8, Action: "画面：女主特写。动作：唇角轻笑。反应：眼神下压。"},
		},
		Duration: 10,
	}
	pos, _ := buildShotVideoPrompt(shot, "3D动漫", "", "", true)
	if !strings.Contains(pos, "frames2video") && !strings.Contains(pos, "FLF2V") {
		t.Fatalf("dialogue shot should use frames2/FLF2V motion: %q", pos)
	}
	if !strings.Contains(pos, "first frame") && !strings.Contains(pos, "two-frame") && !strings.Contains(pos, "FLF2V") {
		t.Fatalf("FLF2V lock wording missing: %q", pos)
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

func TestRewriteEmotionToPhysical(t *testing.T) {
	got := rewriteEmotionToPhysical("石昊怒视前方，悲愤欲绝，冷笑一声")
	for _, bad := range []string{"怒视", "悲愤欲绝", "冷笑"} {
		if strings.Contains(got, bad) {
			t.Fatalf("emotion word %q left in: %q", bad, got)
		}
	}
	for _, want := range []string{"盯", "握拳", "唇角"} {
		if !strings.Contains(got, want) {
			t.Fatalf("physical motion %q missing: %q", want, got)
		}
	}
}

func TestBuildShotVideoPrompt_noOpaqueEmotion(t *testing.T) {
	shot := &storyboard.ShotMeta{
		Description: "【目标】石昊悲愤欲绝怒视前方。【承接】开场。【结果】杀意沸腾。",
		Camera:      "特写",
		Beats: []task.ShotBeat{
			{Time: 0, Action: "画面：石昊近景。动作：愤怒。反应：杀气腾腾。"},
			{Time: 6, Action: "画面：特写。动作：泪流满面。反应：情绪崩溃。"},
		},
		Duration: 10,
	}
	pos, _ := buildShotVideoPrompt(shot, "3D动漫", "", "", true)
	for _, bad := range []string{"悲愤欲绝", "杀意沸腾", "愤怒", "杀气腾腾", "泪流满面", "情绪崩溃", "emotional", "emotion"} {
		if strings.Contains(strings.ToLower(pos), strings.ToLower(bad)) {
			t.Fatalf("opaque emotion leaked %q in: %q", bad, pos)
		}
	}
	if !strings.Contains(pos, "握拳") && !strings.Contains(pos, "嘴唇") && !strings.Contains(pos, "泪") && !strings.Contains(pos, "肩") {
		t.Fatalf("expected physical substitutes in: %q", pos)
	}
}
