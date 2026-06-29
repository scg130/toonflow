package service

import (
	"context"
	"fmt"
	"strings"
	"toonflow/adapter"
	"toonflow/skill"
	"toonflow/task"
)

// ParseScript uses the LLM to parse a raw script into storyboard items.
func ParseScript(ctx context.Context, script, style string, skillMgr *skill.Manager, v adapter.Vendor) ([]task.StoryboardItem, error) {
	systemPrompt := `你是专业短剧分镜师。将剧本拆分为多个独立镜头。
必须只输出 JSON 数组，不要 markdown 说明文字。每项字段：
- shot_number (int) 镜头序号
- scene (string) 场景名
- description (string) 中文画面描述
- camera (string) 运镜，如固定镜头/推镜/摇镜
- duration (float) 秒数，默认3
- prompt (string) 英文 AI 绘画提示词，含画风与构图

示例：[{"shot_number":1,"scene":"柳树下","description":"...","camera":"特写","duration":3,"prompt":"..."}]`

	systemPrompt += "\n\n" + skillMgr.Get("art_skills")
	systemPrompt += "\n" + skillMgr.Get("production_execution")
	systemPrompt += "\n" + skillMgr.Get("story_skills")
	if style != "" {
		systemPrompt += fmt.Sprintf("\n画风: %s，prompt 中需体现。\n", style)
	}

	resp, err := v.TextRequest(ctx, adapter.DefaultTextModel, adapter.TextParams{
		Messages: []adapter.TextMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: "请将以下剧本拆分为分镜 JSON 数组：\n\n" + script},
		},
		Temperature: 0.5,
		MaxTokens:   12000,
	})
	if err != nil {
		return nil, fmt.Errorf("text request: %w", err)
	}

	items, err := parseStoryboardResponse(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if len(items) <= 1 && strings.Contains(resp.Content, "Shot") {
		if md := parseMarkdownShots(resp.Content); len(md) > len(items) {
			items = md
		}
	}
	return NormalizeStoryboardItems(items), nil
}
