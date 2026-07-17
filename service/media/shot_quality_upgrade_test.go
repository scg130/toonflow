package media

import (
	"strings"
	"testing"

	"toonflow/service/storyboard"
	"toonflow/task"
)

func TestVideoSizeForRatio_hongguo720p(t *testing.T) {
	w, h := videoSizeForRatio("9:16")
	if w != 720 || h != 1280 {
		t.Fatalf("9:16 got %dx%d want 720x1280", w, h)
	}
	w, h = videoSizeForRatio("720x1280")
	if w != 720 || h != 1280 {
		t.Fatalf("720x1280 got %dx%d", w, h)
	}
	w, h = videoSizeForRatio("16:9")
	if w != 1280 || h != 720 {
		t.Fatalf("16:9 got %dx%d want 1280x720", w, h)
	}
}

func TestClassifyShotVideoMode_framingJumpForcesFrames2(t *testing.T) {
	shot := &storyboard.ShotMeta{
		Description: "石昊爆发怒吼",
		Camera:      "手持急速推近",
		Beats: []task.ShotBeat{
			{Time: 0, Action: "画面：石昊背部中景。", ImagePrompt: "medium shot back view"},
			{Time: 5, Action: "画面：石昊面部近景怒吼。", ImagePrompt: "close-up face shouting"},
		},
	}
	if got := ClassifyShotVideoMode(shot); got != VideoModeFrames2 {
		t.Fatalf("framing jump want frames2, got %s", got)
	}
}

func TestPreflightShotMetadata_flagsRisks(t *testing.T) {
	item := task.StoryboardItem{
		ShotNumber:     2,
		ActionContinue: "上镜末：指甲抠紧树皮 → 本镜起始：双手维持抠抓姿态",
		Beats: []task.ShotBeat{
			{Time: 0, Action: "背部中景", ImagePrompt: "back view", ImageURL: "/output/t/shot_002_k0.png"},
			{Time: 5, Action: "面部特写长啸", ImagePrompt: "close-up face", ImageURL: "/output/t/shot_002_k1.png"},
		},
	}
	anoms := preflightShotMetadata(item)
	codes := map[string]bool{}
	for _, a := range anoms {
		codes[a.Code] = true
	}
	if !codes["large_framing_jump"] {
		t.Fatalf("expected large_framing_jump, got %#v", anoms)
	}
	if !codes["action_continue_mismatch"] {
		t.Fatalf("expected action_continue_mismatch, got %#v", anoms)
	}
}

func TestKeyframePreflightReport_confirmGate(t *testing.T) {
	r := &KeyframePreflightReport{
		Anomalies: []KeyframeAnomaly{
			{Severity: KeyframeAnomalyWarn, Code: "pixel_jump", Message: "diff"},
		},
	}
	if r.Passed {
		t.Fatal("warn report should not be passed until confirm")
	}
	if !r.NeedsManualConfirm() {
		t.Fatal("warn should need confirm")
	}
	if r.HasBlockers() {
		t.Fatal("warn is not blocker")
	}
	r.Anomalies = append(r.Anomalies, KeyframeAnomaly{Severity: KeyframeAnomalyBlock, Code: "incomplete_keyframes"})
	if !r.HasBlockers() {
		t.Fatal("block severity should block")
	}
}

func TestBuildShotVideoPrompt_framingJumpUsesReframeWording(t *testing.T) {
	shot := &storyboard.ShotMeta{
		Description: "石昊爆发",
		Camera:      "急速推近",
		Beats: []task.ShotBeat{
			{Time: 0, Action: "背部中景", ImagePrompt: "back view"},
			{Time: 5, Action: "面部特写", ImagePrompt: "close-up face"},
		},
		Duration: 10,
	}
	pos, _ := buildShotVideoPrompt(shot, "3D动漫", "", "", true)
	if !strings.Contains(pos, "reframe") {
		t.Fatalf("expected reframe wording: %s", pos)
	}
	if strings.Contains(pos, "multi-keyframe continuous") {
		t.Fatalf("should not use multiframe for framing jump: %s", pos)
	}
}
