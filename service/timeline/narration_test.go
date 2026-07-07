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

func TestRedistributeNarrationTiming(t *testing.T) {
	total := 30.0
	segs := []NarrationSegment{
		{Text: "开场：石昊立于界海废墟。"},          // 12 runes
		{Text: "  "},                          // empty -> dropped
		{Text: "转折降临，黑发暴涨割裂天宇，气势惊人。"}, // longer
		{Text: "高潮收束，一战定乾坤。"},
	}
	out := redistributeNarrationTiming(segs, total)
	if len(out) != 3 {
		t.Fatalf("expected 3 non-empty segments, got %d", len(out))
	}
	// Continuous coverage: first starts at 0, last ends at total, no gaps.
	if out[0].Start != 0 {
		t.Fatalf("first segment must start at 0, got %v", out[0].Start)
	}
	if out[len(out)-1].End != total {
		t.Fatalf("last segment must end at total %.1f, got %v", total, out[len(out)-1].End)
	}
	for i := 1; i < len(out); i++ {
		if out[i].Start != out[i-1].End {
			t.Fatalf("segments must be back-to-back: seg %d start %v != prev end %v", i, out[i].Start, out[i-1].End)
		}
		if out[i].End <= out[i].Start {
			t.Fatalf("segment %d has non-positive duration", i)
		}
	}
	// Longer text should get a larger time share than shorter text.
	if (out[1].End - out[1].Start) <= (out[2].End - out[2].Start) {
		t.Fatalf("longer segment should get more time than shorter one")
	}
}
