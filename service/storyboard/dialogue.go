package storyboard

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"toonflow/service/voice"
	"toonflow/task"
)

// DialogueFormatHint describes manual pipe format (one line per row).
const DialogueFormatHint = "每行：角色名|台词"

// ParsedDialogue is one normalized speakable line for TTS / video prompts.
type ParsedDialogue struct {
	Speaker   string
	PureText  string
	Ignorable bool
}

// DialogueLinesForTTS returns all speakable lines in order.
func DialogueLinesForTTS(d *task.ShotDialogue) []ParsedDialogue {
	if d == nil || d.IsEmpty() {
		return nil
	}
	var out []ParsedDialogue
	for _, ln := range d.LinesNormalized() {
		p := parseDialogueLine(ln)
		if !p.Ignorable {
			out = append(out, p)
		}
	}
	return out
}

// DialogueForTTS returns the first speakable line (compat).
func DialogueForTTS(d *task.ShotDialogue) ParsedDialogue {
	lines := DialogueLinesForTTS(d)
	if len(lines) == 0 {
		return ParsedDialogue{Ignorable: true}
	}
	return lines[0]
}

// HasSpeakableDialogue reports whether any line can be voiced.
func HasSpeakableDialogue(d *task.ShotDialogue) bool {
	return len(DialogueLinesForTTS(d)) > 0
}

func parseDialogueLine(ln task.DialogueLine) ParsedDialogue {
	speaker := voice.NormalizeSpeakerName(strings.TrimSpace(ln.Speaker))
	text := strings.TrimSpace(ln.Text)
	ignorable := isIgnorableSpeaker(speaker) || text == "" || isIgnorableText(text)
	return ParsedDialogue{Speaker: speaker, PureText: text, Ignorable: ignorable}
}

// ParseDialogueUserInput parses multiline manual edits (角色|台词 per line).
func ParseDialogueUserInput(s string) (task.ShotDialogue, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return task.ShotDialogue{}, nil
	}
	var lines []task.DialogueLine
	for _, row := range strings.Split(s, "\n") {
		row = strings.TrimSpace(row)
		if row == "" {
			continue
		}
		parts := strings.SplitN(row, "|", 2)
		if len(parts) != 2 {
			return task.ShotDialogue{}, fmt.Errorf("对白格式应为「%s」，例如：石昊|这一战，我不会退。", DialogueFormatHint)
		}
		speaker := strings.TrimSpace(parts[0])
		text := strings.TrimSpace(parts[1])
		if speaker == "" || text == "" {
			return task.ShotDialogue{}, fmt.Errorf("每行角色名和台词均不能为空")
		}
		lines = append(lines, task.DialogueLine{Speaker: speaker, Text: text})
	}
	if len(lines) == 0 {
		return task.ShotDialogue{}, nil
	}
	return task.ShotDialogue{Lines: lines}, nil
}

// ParseDialogueLines validates structured lines from API/UI.
func ParseDialogueLines(lines []task.DialogueLine) (*task.ShotDialogue, error) {
	if len(lines) == 0 {
		return nil, nil
	}
	out := make([]task.DialogueLine, 0, len(lines))
	for _, ln := range lines {
		sp := strings.TrimSpace(ln.Speaker)
		tx := strings.TrimSpace(ln.Text)
		if sp == "" && tx == "" {
			continue
		}
		if sp == "" || tx == "" {
			return nil, fmt.Errorf("每条对白须同时填写角色名与台词")
		}
		out = append(out, task.DialogueLine{Speaker: sp, Text: tx})
	}
	if len(out) == 0 {
		return nil, nil
	}
	return &task.ShotDialogue{Lines: out}, nil
}

// ParseDialogueFlexible imports dialogue from markdown/table cells or legacy strings.
func ParseDialogueFlexible(s string) *task.ShotDialogue {
	b, err := json.Marshal(strings.TrimSpace(s))
	if err != nil {
		return nil
	}
	var d task.ShotDialogue
	if err := d.UnmarshalJSON(b); err != nil {
		return nil
	}
	if d.IsEmpty() {
		return nil
	}
	return &d
}

// FormatDialogueDisplay formats dialogue for logs and narration references.
func FormatDialogueDisplay(d *task.ShotDialogue) string {
	if d == nil || d.IsEmpty() {
		return ""
	}
	var parts []string
	for _, ln := range d.LinesNormalized() {
		parts = append(parts, strings.TrimSpace(ln.Speaker)+"|"+strings.TrimSpace(ln.Text))
	}
	return strings.Join(parts, " / ")
}

// ExplainComposeSkipReason returns a user-facing hint when dialogue cannot be composed.
func ExplainComposeSkipReason(shotNumber int, d *task.ShotDialogue, parsed ParsedDialogue) string {
	if d == nil || d.IsEmpty() {
		return fmt.Sprintf("第 %d 镜未填写对白。请在分镜「对白」添加角色与台词。", shotNumber)
	}
	raw := FormatDialogueDisplay(d)
	if parsed.Speaker != "" && isIgnorableSpeaker(parsed.Speaker) {
		return fmt.Sprintf("第 %d 镜对白为「%s」，属于音效/环境音，无需 TTS 配音", shotNumber, raw)
	}
	if isIgnorableText(parsed.PureText) {
		return fmt.Sprintf("第 %d 镜对白「%s」无法配音，请填写具体台词", shotNumber, raw)
	}
	if parsed.PureText == "" {
		return fmt.Sprintf("第 %d 镜对白格式有误，当前：%s", shotNumber, raw)
	}
	return fmt.Sprintf("第 %d 镜对白无法合成：%s", shotNumber, raw)
}

func normalizeDialogue(d *task.ShotDialogue) *task.ShotDialogue {
	if d == nil {
		return nil
	}
	lines := d.LinesNormalized()
	if len(lines) == 0 {
		return nil
	}
	return &task.ShotDialogue{Lines: lines}
}

func isIgnorableSpeaker(s string) bool {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	for _, x := range []string{"环境音", "环境声", "音效", "效果音", "sfx", "sound effect", "bgm", "背景音", "背景音乐", "ambient"} {
		if s == x || lower == strings.ToLower(x) {
			return true
		}
	}
	return false
}

func isIgnorableText(t string) bool {
	t = strings.TrimSpace(t)
	lower := strings.ToLower(t)
	for _, x := range []string{"无", "无对白", "无台词", "无旁白", "无需配音", "none", "null", "n/a", "na", "环境音", "音效", "bgm", "sfx", "ambient"} {
		if t == x || lower == strings.ToLower(x) {
			return true
		}
	}
	return false
}

// DialogueLineWeight returns a relative duration weight for one line (by rune count).
func DialogueLineWeight(text string) int {
	n := utf8.RuneCountInString(strings.TrimSpace(text))
	if n < 1 {
		return 1
	}
	return n
}
