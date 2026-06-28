package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"toonflow/adapter"
	"toonflow/skill"
	"toonflow/task"
)

// ParseScript uses the LLM to parse a raw script into storyboard items.
func ParseScript(ctx context.Context, script, style string, skillMgr *skill.Manager, v adapter.Vendor) ([]task.StoryboardItem, error) {
	systemPrompt := "You are a professional short drama storyboard artist. Parse the script into numbered shots with scene, description, camera, and duration.\n\n"
	systemPrompt += skillMgr.Get("art_skills") + "\n"
	systemPrompt += skillMgr.Get("production_execution") + "\n"
	systemPrompt += skillMgr.Get("story_skills") + "\n"
	if style != "" {
		systemPrompt += fmt.Sprintf("\nArt style: %s. Maintain consistency across all shots.\n", style)
	}

	resp, err := v.TextRequest(ctx, "gpt-4o", adapter.TextParams{
		Messages: []adapter.TextMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: script},
		},
		Temperature: 0.7,
		MaxTokens:   8000,
	})
	if err != nil {
		return nil, fmt.Errorf("text request: %w", err)
	}

	items, err := parseStoryboardResponse(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return items, nil
}

func parseStoryboardResponse(text string) ([]task.StoryboardItem, error) {
	// Try JSON first
	var items []task.StoryboardItem
	if err := json.Unmarshal([]byte(text), &items); err == nil {
		return items, nil
	}

	// Try extracting from markdown code block
	text = strings.TrimSpace(text)
	if start := strings.Index(text, "["); start != -1 {
		if end := strings.LastIndex(text, "]"); end != -1 {
			if err := json.Unmarshal([]byte(text[start:end+1]), &items); err == nil {
				return items, nil
			}
		}
	}

	return fallbackParseShots(text), nil
}

func fallbackParseShots(text string) []task.StoryboardItem {
	var items []task.StoryboardItem
	lines := strings.Split(text, "\n")
	var current *task.StoryboardItem
	shotNum := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "镜头") || strings.HasPrefix(line, "Shot ") || strings.HasPrefix(line, "Shot\n") {
			if current != nil {
				items = append(items, *current)
			}
			shotNum++
			current = &task.StoryboardItem{ShotNumber: shotNum, Duration: 3.0}
			continue
		}

		if current == nil {
			current = &task.StoryboardItem{ShotNumber: shotNum, Description: line, Duration: 3.0}
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
				fmt.Sscanf(val, "%f", &current.Duration)
			case "Prompt":
				current.Prompt = val
			}
		} else {
			current.Description += " " + line
		}
	}

	if current != nil {
		items = append(items, *current)
	}

	if len(items) == 0 {
		items = append(items, task.StoryboardItem{ShotNumber: 1, Description: text, Duration: 3.0})
	}

	return items
}
