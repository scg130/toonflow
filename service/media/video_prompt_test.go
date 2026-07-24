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
	if !strings.Contains(pos, "high clarity") && !strings.Contains(pos, "sharp facial") {
		t.Fatalf("clarity/detail quality tags missing: %q", pos)
	}
	if !strings.Contains(neg, "low resolution") || !strings.Contains(neg, "muddy details") {
		t.Fatalf("negative should reject soft/low-res mush: %q", neg)
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

func TestBuildShotVideoPrompt_skipsIncompatibleHandoffAndRoarLipSync(t *testing.T) {
	shot := &storyboard.ShotMeta{
		Description:    "石昊爆发怒吼",
		Camera:         "中景 平视 手持微抖缓慢推近上半身",
		ActionContinue: "上镜末：指甲抠紧树皮 → 本镜起始：双手维持抠抓姿态",
		Dialogue:       &task.ShotDialogue{Lines: []task.DialogueLine{{Speaker: "石昊", Text: "柳神！"}}},
		Beats: []task.ShotBeat{
			{Time: 0, Action: "画面：石昊背部中景。动作：双肩猛然耸起。", ImagePrompt: "medium shot back view, shoulders shrugging"},
			{Time: 5, Action: "画面：石昊面部近景。动作：仰面张嘴长啸。", ImagePrompt: "close-up head tilted back, mouth open wide shouting"},
		},
		Duration: 10,
	}
	pos, _ := buildShotVideoPrompt(shot, "3D动漫", "", "", true)
	if strings.Contains(pos, "树皮") || strings.Contains(pos, "抠抓") {
		t.Fatalf("incompatible action_continue handoff should be dropped: %q", pos)
	}
	if strings.Contains(pos, "张嘴说短句") || strings.Contains(pos, "柳神！") {
		t.Fatalf("roar beat should not force lip-sync dialogue: %q", pos)
	}
	if !strings.Contains(pos, "reframe between locked keyframes") {
		t.Fatalf("back→face jump should request reframe, not morph: %q", pos)
	}
	if strings.Contains(pos, "fast dolly push-in") {
		t.Fatalf("aggressive push-in should be skipped on framing jump: %q", pos)
	}
}

func TestBuildShotVideoPrompt_objectLiquidImpact(t *testing.T) {
	shot := &storyboard.ShotMeta{
		Description:    "焦黑树桩断面特写",
		Camera:         "极特写 俯拍 缓慢推镜至血珠接触面",
		ActionContinue: "开场：无前置姿态",
		Beats: []task.ShotBeat{
			{Time: 0, Action: "画面：焦黑树桩极特写。动作：树皮边缘缓慢剥落。"},
			{Time: 4, Action: "画面：断面特写。动作：一滴暗红血珠砸落，液体向四周晕开。"},
		},
		Duration: 8,
	}
	pos, _ := buildShotVideoPrompt(shot, "3D动漫", "", "", true)
	for _, bad := range []string{"eyes and mouth", "brows lids lips", "handoff from previous ending"} {
		if strings.Contains(pos, bad) {
			t.Fatalf("object macro prompt contains human/placeholder instruction %q: %s", bad, pos)
		}
	}
	for _, want := range []string{"preserve object geometry", "spreads flat", "no upright liquid spike", "locked macro camera"} {
		if !strings.Contains(pos, want) {
			t.Fatalf("object macro guard %q missing: %s", want, pos)
		}
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

func TestIsImpactActionShot(t *testing.T) {
	fight := &storyboard.ShotMeta{
		Description: "黑衣女剑客挥刀斩向白发师尊",
		Beats: []task.ShotBeat{
			{Time: 0, Action: "蓄力举刀"},
			{Time: 3, Action: "刀刃击中肩甲，对方短暂停顿后失衡"},
		},
	}
	if !isImpactActionShot(fight) {
		t.Fatal("expected impact action shot")
	}
	talk := &storyboard.ShotMeta{
		Description: "两人隔桌对视",
		Dialogue:    &task.ShotDialogue{Lines: []task.DialogueLine{{Speaker: "甲", Text: "你来了"}}},
		Beats:       []task.ShotBeat{{Time: 0, Action: "近景对视"}},
	}
	if isImpactActionShot(talk) {
		t.Fatal("dialogue stare should not be impact")
	}
}

func TestBuildShotVideoPrompt_impactChain(t *testing.T) {
	shot := &storyboard.ShotMeta{
		Description: "武士挥刀斩中丧尸脖颈空档",
		Camera:      "中景",
		Beats: []task.ShotBeat{
			{Time: 0, Action: "画面：武士举刀蓄力。动作：刀尖对准空档。"},
			{Time: 2, Action: "画面：刀刃击中甲胄缝隙。动作：手臂顶住，对方停顿。"},
			{Time: 5, Action: "画面：丧尸重心侧倾坠下。"},
		},
		Duration: 8,
	}
	pos, neg := buildShotVideoPrompt(shot, "3D动漫", "", "", true)
	for _, want := range []string{"wind-up", "contact", "hit pause", "weight shift"} {
		if !strings.Contains(pos, want) {
			t.Fatalf("impact chain %q missing: %s", want, pos)
		}
	}
	if !strings.Contains(pos, "micro hitch on contact") && !strings.Contains(pos, "readable hit") {
		t.Fatalf("impact camera missing: %s", pos)
	}
	if strings.Contains(pos, "slow vertical short-drama push-in on face") {
		t.Fatalf("default face push-in should not override impact camera: %s", pos)
	}
	for _, want := range []string{"empty air", "sparks without hit reaction", "no hit pause"} {
		if !strings.Contains(neg, want) {
			t.Fatalf("negative_impact %q missing: %s", want, neg)
		}
	}
}

func TestBuildShotVideoPrompt_emotionProgression(t *testing.T) {
	shot := &storyboard.ShotMeta{
		Description: "女主近景开口",
		Camera:      "近景 定镜",
		Dialogue:    &task.ShotDialogue{Lines: []task.DialogueLine{{Speaker: "女主", Text: "你骗我"}}},
		Beats: []task.ShotBeat{
			{Time: 0, Action: "画面：女主近景。动作：眉心微蹙。"},
			{Time: 4, Action: "画面：女主近景。动作：下颌咬紧。"},
		},
		Duration: 6,
	}
	pos, _ := buildShotVideoPrompt(shot, "3D动漫", "", "", true)
	if !strings.Contains(pos, "emotion progresses") {
		t.Fatalf("emotion progression missing: %s", pos)
	}
}

