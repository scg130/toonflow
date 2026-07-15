package storyboard

import (
	"context"
	"fmt"
	"strings"

	"toonflow/adapter"
	"toonflow/logger"
	"toonflow/service/asset"
	"toonflow/service/internal/duration"
	"toonflow/skill"
	"toonflow/task"
)

// ParseScript uses the LLM to parse a raw script into storyboard items.
func ParseScript(ctx context.Context, script, style string, assets []asset.ProjectAsset, skillMgr *skill.Manager, v adapter.Vendor) ([]task.StoryboardItem, error) {
	minShots := MinShotsForScript(script)
	items, err := parseScriptOnce(ctx, script, style, assets, skillMgr, v, minShots, false)
	if err != nil {
		return nil, err
	}
	if len(items) < minShots {
		logger.CtxTrace(ctx, "storyboard parse retry got=%d need=%d", len(items), minShots)
		retry, err := parseScriptOnce(ctx, script, style, assets, skillMgr, v, minShots, true)
		if err == nil && len(retry) > len(items) {
			items = retry
		}
	}
	items = asset.LinkStoryboardAssets(NormalizeStoryboardItems(items), assets)
	if len(items) < minShots {
		return items, fmt.Errorf("分镜仅 %d 镜，剧本至少需要 %d 镜（5分钟短剧目标 %d–%d 镜），请重试生成分镜",
			len(items), minShots, duration.TargetShotsMin, duration.TargetShotsMax)
	}
	return items, nil
}

func parseScriptOnce(ctx context.Context, script, style string, assets []asset.ProjectAsset, skillMgr *skill.Manager, v adapter.Vendor, minShots int, strict bool) ([]task.StoryboardItem, error) {
	systemPrompt := storyboardSystemPrompt(skillMgr, minShots)

	if skillMgr != nil {
		systemPrompt += asset.FormatAssetsForStoryboardPrompt(assets)
		systemPrompt += "\n\n" + skillMgr.Get("art_skills")
		systemPrompt += "\n" + skillMgr.Get("production_execution")
		systemPrompt += "\n" + skillMgr.Get("story_skills")
	} else {
		systemPrompt += asset.FormatAssetsForStoryboardPrompt(assets)
	}
	if style != "" {
		systemPrompt += fmt.Sprintf("\n画风: %s，prompt 中需体现。\n", style)
	}

	userPrompt := "请将以下 5 分钟短剧剧本拆分为标准化分镜 JSON {\"shots\":[...]}：\n\n" + script
	if strict {
		userPrompt = storyboardRetryPrompt(skillMgr, minShots, userPrompt)
	}

	resp, err := v.TextRequest(ctx, adapter.DefaultTextModel, adapter.TextParams{
		Messages: []adapter.TextMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.45,
		MaxTokens:   16000,
		JSONMode:    true,
	})
	if err != nil {
		return nil, fmt.Errorf("text request: %w", err)
	}

	items, err := ParseStoryboardResponse(resp.Content)
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

func storyboardSystemPrompt(skillMgr *skill.Manager, minShots int) string {
	args := []any{minShots, duration.TargetShotsMin, duration.TargetShotsMax}
	var base string
	if skillMgr != nil {
		base = strings.TrimSpace(skillMgr.Format("prompts/storyboard_parse.md", args...))
	}
	if base == "" {
		base = strings.TrimSpace(skill.Format("prompts/storyboard_parse.md", args...))
	}
	if base == "" {
		base = fmt.Sprintf("你是短剧分镜师。输出约 %d 支镜（范围 %d–%d），JSON {\"shots\":[...]}，beat.action 只写可见肢体动作，并写全分镜七要素。", args...)
	}
	seven := ""
	if skillMgr != nil {
		seven = strings.TrimSpace(skillMgr.File("prompts/storyboard_seven.md"))
	}
	if seven == "" {
		seven = strings.TrimSpace(skill.File("prompts/storyboard_seven.md"))
	}
	if seven != "" {
		base += "\n\n" + seven
	}
	return base
}

func storyboardRetryPrompt(skillMgr *skill.Manager, minShots int, baseUser string) string {
	args := []any{minShots, duration.TargetShotsMin, duration.TargetShotsMax, baseUser}
	if skillMgr != nil {
		if s := strings.TrimSpace(skillMgr.Format("prompts/storyboard_parse_retry.md", args...)); s != "" {
			return s
		}
	}
	if s := strings.TrimSpace(skill.Format("prompts/storyboard_parse_retry.md", args...)); s != "" {
		return s
	}
	return fmt.Sprintf("请重拆：约 %d 镜（%d–%d），beat 只写可见动作。\n\n%s", args...)
}
