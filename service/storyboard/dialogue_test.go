package storyboard

import (
	"strings"
	"testing"

	"toonflow/task"
)

func TestDialogueForTTS(t *testing.T) {
	cases := []struct {
		in       *task.ShotDialogue
		speaker  string
		text     string
		ignore   bool
	}{
		{nil, "", "", true},
		{&task.ShotDialogue{}, "", "", true},
		{&task.ShotDialogue{Lines: []task.DialogueLine{{Speaker: "石昊", Text: "这一战，我不会退。"}}}, "石昊", "这一战，我不会退。", false},
		{&task.ShotDialogue{Lines: []task.DialogueLine{{Speaker: "环境音", Text: "风声"}}}, "", "", true},
	}
	for _, c := range cases {
		got := DialogueForTTS(c.in)
		if got.Ignorable != c.ignore {
			t.Fatalf("input %+v ignorable=%v want %v", c.in, got.Ignorable, c.ignore)
		}
		if !c.ignore {
			if got.Speaker != c.speaker {
				t.Fatalf("input %+v speaker=%q want %q", c.in, got.Speaker, c.speaker)
			}
			if got.PureText != c.text {
				t.Fatalf("input %+v text=%q want %q", c.in, got.PureText, c.text)
			}
		}
	}
}

func TestDialogueLinesForTTS_multi(t *testing.T) {
	d := &task.ShotDialogue{Lines: []task.DialogueLine{
		{Speaker: "石昊", Text: "住手！"},
		{Speaker: "柳神", Text: "退下。"},
	}}
	lines := DialogueLinesForTTS(d)
	if len(lines) != 2 || lines[1].Speaker != "柳神" {
		t.Fatalf("unexpected lines: %+v", lines)
	}
}

func TestParseDialogueUserInput(t *testing.T) {
	got, err := ParseDialogueUserInput("石昊|这一战，我不会退。")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.LinesNormalized()) != 1 || got.LinesNormalized()[0].Speaker != "石昊" {
		t.Fatalf("unexpected %+v", got)
	}
	multi, err := ParseDialogueUserInput("石昊|第一句\n柳神|第二句")
	if err != nil || len(multi.LinesNormalized()) != 2 {
		t.Fatalf("multi-line: %+v %v", multi, err)
	}
}

func TestShotDialogueUnmarshalJSON(t *testing.T) {
	var d task.ShotDialogue
	if err := d.UnmarshalJSON([]byte(`{"lines":[{"speaker":"石昊","text":"走了"}]}`)); err != nil {
		t.Fatal(err)
	}
	if len(d.LinesNormalized()) != 1 || d.LinesNormalized()[0].Text != "走了" {
		t.Fatalf("object: %+v", d.LinesNormalized())
	}
	if err := d.UnmarshalJSON([]byte(`[{"speaker":"石昊","text":"走了"}]`)); err != nil {
		t.Fatal(err)
	}
	if err := d.UnmarshalJSON([]byte(`"石昊|走了"`)); err != nil {
		t.Fatal(err)
	}
	if d.LinesNormalized()[0].Speaker != "石昊" {
		t.Fatalf("pipe string: %+v", d.LinesNormalized())
	}
}

func TestExplainComposeSkipReason(t *testing.T) {
	got := ExplainComposeSkipReason(1, nil, ParsedDialogue{Ignorable: true})
	if !strings.Contains(got, "未填写对白") {
		t.Fatalf("expected empty dialogue hint, got %q", got)
	}
}
