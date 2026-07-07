package timeline

import "testing"

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

func TestXfadeTransitionName(t *testing.T) {
	if xfadeTransitionName("dip") != "fadeblack" {
		t.Fatal("dip mapping")
	}
	if xfadeTransitionName("wipe") != "wipeleft" {
		t.Fatal("wipe mapping")
	}
}
