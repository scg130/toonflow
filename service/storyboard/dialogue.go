package storyboard

import (
	"regexp"
	"strings"
)

var (
	reDialogueSpeaker  = regexp.MustCompile(`^(.+?)[:：]`)
	reDialogueAnywhere = regexp.MustCompile(`([\p{Han}\w·]+)\s*[:：]\s*([^\n。；;]+)`)
	reIgnorableSpeaker = regexp.MustCompile(`^(环境音|环境声|音效|效果音|sfx|sound ?effect|bgm|背景音|背景音乐|ambient)$`)
)

// ExtractDialogueFromDescription pulls a speakable dialogue line from shot description text.
func ExtractDialogueFromDescription(desc string) string {
	lines := strings.Split(desc, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if reDialogueSpeaker.MatchString(line) {
			return line
		}
	}
	if m := reDialogueAnywhere.FindStringSubmatch(desc); len(m) >= 3 {
		speaker := strings.TrimSpace(m[1])
		if reIgnorableSpeaker.MatchString(speaker) {
			return ""
		}
		return strings.TrimSpace(m[1] + "：" + m[2])
	}
	return ""
}
