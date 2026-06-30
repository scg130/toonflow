package service

import (
	"context"
	"fmt"
	"strings"

	"toonflow/adapter"
	"toonflow/logger"
	"toonflow/skill"
	"toonflow/task"
)

// ParseScript uses the LLM to parse a raw script into storyboard items.
func ParseScript(ctx context.Context, script, style string, skillMgr *skill.Manager, v adapter.Vendor) ([]task.StoryboardItem, error) {
	minShots := MinShotsForScript(script)
	items, err := parseScriptOnce(ctx, script, style, skillMgr, v, minShots, false)
	if err != nil {
		return nil, err
	}
	if len(items) < minShots {
		logger.CtxTrace(ctx, "storyboard parse retry got=%d need=%d", len(items), minShots)
		retry, err := parseScriptOnce(ctx, script, style, skillMgr, v, minShots, true)
		if err == nil && len(retry) > len(items) {
			items = retry
		}
	}
	items = NormalizeStoryboardItems(items)
	if len(items) < minShots {
		return items, fmt.Errorf("分镜仅 %d 镜，剧本至少需要 %d 镜，请重试生成分镜", len(items), minShots)
	}
	return items, nil
}

func parseScriptOnce(ctx context.Context, script, style string, skillMgr *skill.Manager, v adapter.Vendor, minShots int, strict bool) ([]task.StoryboardItem, error) {
	systemPrompt := fmt.Sprintf(`你是专业短剧分镜师。将剧本拆分为多个独立镜头，必须覆盖剧本中的每一个场次、关键动作和对白段落，不得把整集压缩成单镜。

硬性要求：
- 至少输出 %d 个镜头（shot_number 从 1 连续编号）
- 每个场次至少 2 个镜头（建立镜头 + 细节/对白镜头）
- 对白、动作转折、情绪变化处应单独成镜

必须只输出 JSON 数组，不要 markdown 说明文字。每项字段：
- shot_number (int) 镜头序号
- scene (string) 场景名
- description (string) 中文画面描述
- camera (string) 运镜，如固定镜头/推镜/摇镜
- duration (float) 秒数，默认3
- prompt (string) 英文 AI 绘画提示词，含画风与构图

示例：[{"shot_number":1,"scene":"柳树下","description":"...","camera":"特写","duration":3,"prompt":"..."}]`, minShots)

	systemPrompt += "\n\n" + skillMgr.Get("art_skills")
	systemPrompt += "\n" + skillMgr.Get("production_execution")
	systemPrompt += "\n" + skillMgr.Get("story_skills")
	if style != "" {
		systemPrompt += fmt.Sprintf("\n画风: %s，prompt 中需体现。\n", style)
	}

	userPrompt := "请将以下剧本拆分为分镜 JSON 数组：\n\n" + script
	if strict {
		userPrompt = fmt.Sprintf("上次拆分镜头数不足 %d 个。请重新拆分，必须至少 %d 镜，覆盖全部场次，不得省略。\n\n%s", minShots, minShots, userPrompt)
	}

	resp, err := v.TextRequest(ctx, adapter.DefaultTextModel, adapter.TextParams{
		Messages: []adapter.TextMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
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
	if len(items) <= 1 && LooksLikeStoryboardTable(resp.Content) {
		if table := parseTableStoryboard(resp.Content); len(table) > len(items) {
			items = table
		}
	}
	return NormalizeStoryboardItems(items), nil
}
