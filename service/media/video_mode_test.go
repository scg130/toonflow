package media

import (
	"testing"

	"toonflow/service/storyboard"
	"toonflow/task"
)

func TestClassifyShotVideoMode(t *testing.T) {
	dlg := makeShotMeta(2, "中景稳镜", "【目标】交代身份。【承接】开场。【结果】女主上场。", false)
	if got := ClassifyShotVideoMode(dlg); got != VideoModeFrames2 {
		t.Fatalf("dialogue shot want frames2, got %s", got)
	}
	fight := makeShotMeta(3, "手持急速推镜", "【目标】石昊突袭反击。【承接】对峙。【结果】羽帝受创。", true)
	if got := ClassifyShotVideoMode(fight); got != VideoModeMultiframe {
		t.Fatalf("fight shot want multiframe, got %s", got)
	}
}

func TestSelectKeyframesForMode(t *testing.T) {
	urls := []string{"a", "b", "c"}
	got := SelectKeyframesForMode(urls, VideoModeFrames2)
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Fatalf("frames2 want [a,c], got %v", got)
	}
	got = SelectKeyframesForMode(urls, VideoModeMultiframe)
	if len(got) != 3 {
		t.Fatalf("multiframe want 3, got %v", got)
	}
}

func makeShotMeta(beats int, cam, desc string, actionBeats bool) *storyboard.ShotMeta {
	s := &storyboard.ShotMeta{Camera: cam, Description: desc}
	for i := 0; i < beats; i++ {
		act := "画面：角色站立。动作：微微点头。反应：平静。"
		if actionBeats {
			act = "画面：中景对峙。动作：一拳打出。反应：受创后退。"
		}
		s.Beats = append(s.Beats, task.ShotBeat{Time: float64(i) * 4, Action: act})
	}
	return s
}
