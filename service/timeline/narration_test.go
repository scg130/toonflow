package timeline

import (
	"strings"
	"testing"
)

func TestTimelineVideoDuration(t *testing.T) {
	tl := &TimelineEdit{
		ExportSettings: &TimelineExportSettings{
			DefaultTransition:  "fade",
			TransitionDuration: 0.15,
			TrimHeadFrames:     1,
			TrimTailFrames:     1,
		},
		Tracks: []TimelineTrack{{
			Type: "video",
			Clips: []TimelineClip{
				{Start: 0, End: 3, Duration: 5},
				{Start: 1, End: 4, Duration: 5},
			},
		}},
	}
	got := TimelineVideoDuration(tl)
	want := (3.0 - 2.0/24.0) + (3.0 - 2.0/24.0) - 0.15
	if got < want-0.001 || got > want+0.001 {
		t.Fatalf("expected ~%v, got %v", want, got)
	}
}

func TestParseNarrationSegments(t *testing.T) {
	raw := `说明文字 [{"start":0,"end":3,"text":"测试旁白"}] 结尾`
	segs, err := parseNarrationSegments(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 || segs[0].Text != "测试旁白" {
		t.Fatalf("unexpected segments: %+v", segs)
	}
}

func TestSplitNarrationTextPreservesContent(t *testing.T) {
	long := strings.Repeat("这是一段很长的旁白文案。", 20)
	chunks := splitNarrationText(long)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	var joined strings.Builder
	for _, c := range chunks {
		joined.WriteString(c)
	}
	if strings.TrimSpace(joined.String()) != long {
		t.Fatal("split chunks lost content")
	}
}

func TestEnsureNarrationCoverage(t *testing.T) {
	tl := &TimelineEdit{
		ExportSettings: &TimelineExportSettings{TrimHeadFrames: 0, TrimTailFrames: 0, DefaultTransition: "none"},
		Tracks: []TimelineTrack{{
			Type: "video",
			Clips: []TimelineClip{
				{ShotNumber: 1, Label: "第 1 镜", Start: 0, End: 3, Duration: 3},
				{ShotNumber: 2, Label: "第 2 镜", Start: 0, End: 4, Duration: 4},
			},
		}},
	}
	segs := []NarrationSegment{{Start: 0, End: 3, Text: "只有第一镜", ShotNum: 1}}
	out := ensureNarrationCoverage(segs, tl, 7)
	if len(out) < 2 {
		t.Fatalf("expected coverage for second clip, got %d segments", len(out))
	}
}
