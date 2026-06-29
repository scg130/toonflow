package service

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"toonflow/task"
)

var (
	reShotHeader = regexp.MustCompile(`(?i)^(?:#{1,3}\s*)?(?:\*\*)?(?:镜头|Shot|第\s*(\d+)\s*镜|镜\s*(\d+))`)
	reShotNum    = regexp.MustCompile(`(?i)(?:Shot|镜头)\s*(\d+)(?:-(\d+))?`)
	reSceneLine  = regexp.MustCompile(`(?i)^(?:#{1,3}\s*)?(?:\*\*)?(?:场景|Scene|场次)\s*\d*[:：\s]+(.+)`)
)

// NormalizeStoryboardItems fills defaults and fixes shot numbers.
func NormalizeStoryboardItems(items []task.StoryboardItem) []task.StoryboardItem {
	out := make([]task.StoryboardItem, 0, len(items))
	for i, it := range items {
		if strings.TrimSpace(it.Description) == "" && strings.TrimSpace(it.Prompt) == "" && strings.TrimSpace(it.Scene) == "" {
			continue
		}
		if it.ShotNumber <= 0 {
			it.ShotNumber = i + 1
		}
		if it.Duration <= 0 {
			it.Duration = 3
		}
		if it.Prompt == "" {
			it.Prompt = it.Description
		}
		if it.Description == "" {
			it.Description = it.Prompt
		}
		if it.Camera == "" {
			it.Camera = "固定镜头"
		}
		out = append(out, it)
	}
	return out
}

func parseStoryboardResponse(text string) ([]task.StoryboardItem, error) {
	text = strings.TrimSpace(text)

	var items []task.StoryboardItem
	if err := json.Unmarshal([]byte(text), &items); err == nil && len(items) > 0 {
		return NormalizeStoryboardItems(items), nil
	}

	if start := strings.Index(text, "["); start != -1 {
		if end := strings.LastIndex(text, "]"); end > start {
			if err := json.Unmarshal([]byte(text[start:end+1]), &items); err == nil && len(items) > 0 {
				return NormalizeStoryboardItems(items), nil
			}
		}
	}

	items = parseMarkdownShots(text)
	if len(items) > 0 {
		return NormalizeStoryboardItems(items), nil
	}

	items = fallbackParseShots(text)
	return NormalizeStoryboardItems(items), nil
}

func parseMarkdownShots(text string) []task.StoryboardItem {
	var items []task.StoryboardItem
	var current *task.StoryboardItem
	shotNum := 0
	currentScene := ""

	flush := func() {
		if current == nil {
			return
		}
		if current.Scene == "" {
			current.Scene = currentScene
		}
		items = append(items, *current)
		current = nil
	}

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "---") {
			continue
		}
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.Trim(line, "*")

		if m := reSceneLine.FindStringSubmatch(line); len(m) > 1 {
			currentScene = strings.TrimSpace(m[1])
			continue
		}

		if isShotHeader(line) {
			flush()
			shotNum++
			if n := extractShotNumber(line); n > 0 {
				shotNum = n
			}
			current = &task.StoryboardItem{
				ShotNumber: shotNum,
				Scene:      currentScene,
				Duration:   3,
			}
			if rest := shotHeaderRemainder(line); rest != "" {
				current.Description = rest
			}
			continue
		}

		if current == nil {
			if strings.HasPrefix(line, "#") || strings.Contains(strings.ToLower(line), "storyboard breakdown") {
				continue
			}
			continue
		}

		if idx := strings.IndexAny(line, ":："); idx != -1 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			switch strings.ToLower(key) {
			case "场景", "scene":
				current.Scene = val
			case "描述", "description", "画面", "画面描述", "visual":
				current.Description = val
			case "镜头", "camera", "运镜":
				current.Camera = val
			case "时长", "duration":
				fmtScanFloat(val, &current.Duration)
			case "prompt", "绘画", "绘画prompt", "ai prompt":
				current.Prompt = val
			default:
				current.Description = strings.TrimSpace(current.Description + " " + line)
			}
		} else {
			current.Description = strings.TrimSpace(current.Description + " " + line)
		}
	}
	flush()
	return items
}

func isShotHeader(line string) bool {
	if reShotHeader.MatchString(line) {
		return true
	}
	return reShotNum.MatchString(line)
}

func extractShotNumber(line string) int {
	if m := reShotNum.FindStringSubmatch(line); len(m) > 1 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	if m := reShotHeader.FindStringSubmatch(line); len(m) > 1 {
		for i := 1; i < len(m); i++ {
			if m[i] != "" {
				n, _ := strconv.Atoi(m[i])
				if n > 0 {
					return n
				}
			}
		}
	}
	return 0
}

func shotHeaderRemainder(line string) string {
	if idx := strings.IndexAny(line, ":："); idx != -1 {
		return strings.TrimSpace(line[idx+1:])
	}
	if m := reShotNum.FindStringIndex(line); m != nil {
		return strings.TrimSpace(line[m[1]:])
	}
	return ""
}

func fmtScanFloat(val string, out *float64) {
	f, err := strconv.ParseFloat(strings.TrimSuffix(strings.Fields(val)[0], "s"), 64)
	if err == nil {
		*out = f
	}
}

func fallbackParseShots(text string) []task.StoryboardItem {
	if md := parseMarkdownShots(text); len(md) > 0 {
		return md
	}

	var items []task.StoryboardItem
	lines := strings.Split(text, "\n")
	var current *task.StoryboardItem
	shotNum := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if isShotHeader(line) {
			if current != nil {
				items = append(items, *current)
			}
			shotNum++
			if n := extractShotNumber(line); n > 0 {
				shotNum = n
			}
			current = &task.StoryboardItem{ShotNumber: shotNum, Duration: 3.0}
			if rest := shotHeaderRemainder(line); rest != "" {
				current.Description = rest
			}
			continue
		}

		if current == nil {
			continue
		}

		if idx := strings.IndexAny(line, ":："); idx != -1 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			switch key {
			case "场景", "Scene":
				current.Scene = val
			case "描述", "Description":
				current.Description = val
			case "镜头", "Camera":
				current.Camera = val
			case "时长", "Duration":
				fmtScanFloat(val, &current.Duration)
			case "Prompt", "prompt":
				current.Prompt = val
			default:
				current.Description = strings.TrimSpace(current.Description + " " + line)
			}
		} else {
			current.Description = strings.TrimSpace(current.Description + " " + line)
		}
	}
	if current != nil {
		items = append(items, *current)
	}
	if len(items) == 0 {
		items = append(items, task.StoryboardItem{ShotNumber: 1, Description: text, Duration: 3.0, Prompt: text})
	}
	return items
}
