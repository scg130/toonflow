package media

import (
	"strings"
	"testing"

	"toonflow/task"
)

func TestShouldChainVideoContinuity(t *testing.T) {
	if !ShouldChainVideoContinuity(task.SceneLinkContinuous, "走廊", "大厅") {
		t.Fatal("continuous link must chain even across scene rename")
	}
	if !ShouldChainVideoContinuity(task.SceneLinkTransition, "走廊", "走廊") {
		t.Fatal("same scene name should chain for soft identity lock")
	}
	if ShouldChainVideoContinuity(task.SceneLinkTransition, "走廊", "宴会厅") {
		t.Fatal("hard scene change must not chain")
	}
	if ShouldChainVideoContinuity(task.SceneLinkTransition, "", "") {
		t.Fatal("empty scenes without continuous must not chain")
	}
}

func TestApplyContinuityToKeyframes(t *testing.T) {
	urls := []string{"a", "b", "c"}
	got := ApplyContinuityToKeyframes(urls, "prev", VideoModeFrames2)
	if len(got) != 2 || got[0] != "prev" || got[1] != "c" {
		t.Fatalf("frames2 continuity want [prev,c], got %v", got)
	}
	got = ApplyContinuityToKeyframes(urls, "prev", VideoModeMultiframe)
	if len(got) < 2 || got[0] != "prev" {
		t.Fatalf("multiframe continuity should start with prev, got %v", got)
	}
	if len(got) > 3 {
		t.Fatalf("Agnes max 3 keyframes, got %v", got)
	}
}

func TestPrependAcceptedContinuityDirective(t *testing.T) {
	got := prependAcceptedContinuityDirective("motion: standing up")
	if !strings.Contains(got, "accepted previous-clip") || !strings.Contains(got, "preserve face identity") {
		t.Fatalf("continuity directive incomplete: %q", got)
	}
	if !strings.Contains(got, "motion: standing up") {
		t.Fatalf("original prompt lost: %q", got)
	}
}
