package storyboard

import (
	"strings"
	"testing"

	"toonflow/task"
)

func TestSyncSevenElements(t *testing.T) {
	it := task.StoryboardItem{ShotNumber: 1, Description: "男主抬下巴", Camera: "特写 推近"}
	SyncSevenElements(&it)
	if it.ShotSize != "特写" {
		t.Fatalf("shot_size=%q", it.ShotSize)
	}
	if it.Angle == "" || it.Motion == "" {
		t.Fatalf("angle/motion empty: %q / %q", it.Angle, it.Motion)
	}
	if it.Composition == "" || it.ColorTone == "" || it.Lighting == "" {
		t.Fatal("defaults missing")
	}
	if it.Camera == "" {
		t.Fatal("camera composite empty")
	}
	seven := FormatSevenElementsPrompt(it)
	if seven == "" || !strings.Contains(seven, "shot size") {
		t.Fatalf("seven prompt=%q", seven)
	}
}
