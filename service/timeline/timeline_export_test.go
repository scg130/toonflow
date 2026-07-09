package timeline

import (
	"testing"

	"toonflow/task"
)

func TestClipTrimRange(t *testing.T) {
	settings := DefaultExportSettings()
	clip := TimelineClip{Start: 0, End: 3, Duration: 3}
	start, end := clipTrimRange(clip, settings)
	if start <= 0 || end <= start {
		t.Fatalf("unexpected range start=%v end=%v", start, end)
	}
}

func TestEffectiveTransitionAfter(t *testing.T) {
	settings := DefaultExportSettings()
	if effectiveTransitionAfter(TimelineClip{}, settings) != "fade" {
		t.Fatal("expected default fade")
	}
	if effectiveTransitionAfter(TimelineClip{Transition: "none"}, settings) != "none" {
		t.Fatal("expected none")
	}
	if effectiveTransitionAfter(TimelineClip{Transition: "dip"}, settings) != "dip" {
		t.Fatal("expected dip")
	}
}

func TestTimelineTransitionForShot(t *testing.T) {
	if timelineTransitionForShot(task.SceneLinkContinuous, "") != "fade" {
		t.Fatal("continuous same-scene should fade")
	}
	if timelineTransitionForShot(task.SceneLinkContinuous, "fade black") != "dip" {
		t.Fatal("continuous with fade black hint should dip")
	}
	if timelineTransitionForShot(task.SceneLinkTransition, "wipe") != "wipe" {
		t.Fatal("scene change wipe")
	}
}

func TestXfadeTransitionName(t *testing.T) {
	if xfadeTransitionName("dip") != "fadeblack" {
		t.Fatal("dip mapping")
	}
	if xfadeTransitionName("wipe") != "wipeleft" {
		t.Fatal("wipe mapping")
	}
}
