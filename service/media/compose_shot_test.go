package media

import (
	"strings"
	"testing"

	"toonflow/service/voice"
)

func TestParseDialogueForTTS(t *testing.T) {
	cases := []struct {
		in       string
		speaker  string
		text     string
		ignore   bool
	}{
		{"", "", "", true},
		{"石昊：这一战，我不会退。", "石昊", "这一战，我不会退。", false},
		{"环境音：风声", "", "", true},
		{"无对白", "", "", true},
	}
	for _, c := range cases {
		got := ParseDialogueForTTS(c.in)
		if got.Ignorable != c.ignore {
			t.Fatalf("input %q ignorable=%v want %v", c.in, got.Ignorable, c.ignore)
		}
		if !c.ignore {
			if got.Speaker != c.speaker {
				t.Fatalf("input %q speaker=%q want %q", c.in, got.Speaker, c.speaker)
			}
			if got.PureText != c.text {
				t.Fatalf("input %q text=%q want %q", c.in, got.PureText, c.text)
			}
		}
	}
}

func TestExplainComposeSkipReason(t *testing.T) {
	got := ExplainComposeSkipReason(1, "", ParsedDialogue{Ignorable: true})
	if !strings.Contains(got, "未填写对白") {
		t.Fatalf("expected empty dialogue hint, got %q", got)
	}
	got2 := ExplainComposeSkipReason(2, "环境音：风声", ParseDialogueForTTS("环境音：风声"))
	if !strings.Contains(got2, "音效") {
		t.Fatalf("expected sfx hint, got %q", got2)
	}
}

func TestBuildAtempoChain(t *testing.T) {
	cases := []struct {
		ratio float64
		want  string
	}{
		{1.0, ""},
		{1.5, "atempo=1.500000"},
		{2.0, "atempo=2.000000"},
		{4.0, "atempo=2.0,atempo=2.000000"},
		{0.25, "atempo=0.5,atempo=0.500000"},
		{6.0, "atempo=2.0,atempo=2.0,atempo=1.500000"},
	}
	for _, c := range cases {
		got := buildAtempoChain(c.ratio)
		if got != c.want {
			t.Fatalf("ratio=%v got %q want %q", c.ratio, got, c.want)
		}
	}
}

func TestComposeAudioMatchVideoFilter(t *testing.T) {
	got := composeAudioMatchVideoFilter(4.0, 1.0)
	if !strings.Contains(got, "atempo=0.5,atempo=0.500000") {
		t.Fatalf("expected slow-down chain for short audio, got %q", got)
	}
	if strings.Contains(got, "atrim") {
		t.Fatalf("should not trim audio: %q", got)
	}
	if !strings.Contains(got, "apad=whole_dur=4.000") {
		t.Fatalf("expected pad to video duration, got %q", got)
	}

	gotLong := composeAudioMatchVideoFilter(4.0, 6.0)
	if !strings.Contains(gotLong, "atempo=1.500000") {
		t.Fatalf("expected speed-up for long audio, got %q", gotLong)
	}
}

func TestIsValidEdgeVoice(t *testing.T) {
	if !voice.IsValidEdgeVoice("zh-CN-XiaoxiaoNeural") {
		t.Fatal("expected valid voice")
	}
	if voice.IsValidEdgeVoice("invalid-voice") {
		t.Fatal("expected invalid voice")
	}
}
