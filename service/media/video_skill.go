package media

import (
	"strings"
	"sync"

	"toonflow/skill"
)

const videoI2VSkill = "prompts/video_i2v.md"

var (
	videoSkillOnce sync.Once
	videoSkillBody string
)

func videoI2VBody() string {
	videoSkillOnce.Do(func() {
		videoSkillBody = skill.File(videoI2VSkill)
	})
	// Allow hot reload if SetDefault happens after first call in tests.
	if videoSkillBody == "" {
		videoSkillBody = skill.File(videoI2VSkill)
	}
	return videoSkillBody
}

// resetVideoI2VSkillCache is for tests after SetDefault.
func resetVideoI2VSkillCache() {
	videoSkillOnce = sync.Once{}
	videoSkillBody = ""
}

func videoI2VSection(heading string) string {
	return skill.SectionText(videoI2VBody(), heading)
}

func videoI2VLines(heading string, fallback []string) []string {
	lines := skill.SectionLines(videoI2VBody(), heading)
	if len(lines) == 0 {
		return fallback
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(line, ",") && !strings.Contains(line, "%s") {
			for _, p := range strings.Split(line, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					out = append(out, p)
				}
			}
			continue
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func videoI2VCSV(heading string, fallback string) string {
	if s := strings.TrimSpace(videoI2VSection(heading)); s != "" {
		// Prefer comma-separated body; also accept one-item-per-line.
		if strings.Contains(s, ",") {
			return collapseCSV(s)
		}
		lines := skill.SectionLines(videoI2VBody(), heading)
		if len(lines) > 0 {
			return strings.Join(lines, ", ")
		}
		return collapseCSV(s)
	}
	return fallback
}

func collapseCSV(s string) string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}

func videoI2VOneLine(heading, fallback string) string {
	if s := strings.TrimSpace(videoI2VSection(heading)); s != "" {
		// first non-empty line
		for _, line := range strings.Split(s, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "- ") {
				line = strings.TrimSpace(line[2:])
			}
			if line != "" && !strings.HasPrefix(line, "#") {
				return line
			}
		}
	}
	return fallback
}
