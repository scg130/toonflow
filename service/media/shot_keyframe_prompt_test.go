package media

import (
	"strings"
	"testing"

	"toonflow/task"
)

func TestBuildBeatImagePrompt_stabilizesLiquidImpact(t *testing.T) {
	beat := task.ShotBeat{
		Time:        4,
		Action:      "一滴暗红血珠砸落，液体向四周晕开",
		ImagePrompt: "close-up liquid drop hitting charred wood surface, spreading outward",
	}
	got := BuildBeatImagePrompt(task.StoryboardItem{}, beat, "3D动漫", "9:16", "", "")
	for _, want := range []string{"settled flat", "no suspended droplet", "no upright liquid spike"} {
		if !strings.Contains(got, want) {
			t.Fatalf("impact stabilization %q missing: %s", want, got)
		}
	}
}

func TestBuildBeatImagePrompt_doesNotAlterOrdinaryAction(t *testing.T) {
	beat := task.ShotBeat{
		Time:        2,
		Action:      "角色缓慢抬头",
		ImagePrompt: "medium close-up, character slowly raises head",
	}
	got := BuildBeatImagePrompt(task.StoryboardItem{}, beat, "3D动漫", "9:16", "", "")
	if strings.Contains(got, "settled flat") {
		t.Fatalf("ordinary action received liquid stabilization: %s", got)
	}
}
