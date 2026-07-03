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
func ParseScript(ctx context.Context, script, style string, assets []ProjectAsset, skillMgr *skill.Manager, v adapter.Vendor) ([]task.StoryboardItem, error) {
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
	items = LinkStoryboardAssets(NormalizeStoryboardItems(items), assets)
	if len(items) < minShots {
		return items, fmt.Errorf("分镜仅 %d 镜，剧本至少需要 %d 镜，请重试生成分镜", len(items), minShots)
	}
	return items, nil
}

func parseScriptOnce(ctx context.Context, script, style string, assets []ProjectAsset, skillMgr *skill.Manager, v adapter.Vendor, minShots int, strict bool) ([]task.StoryboardItem, error) {
	systemPrompt := fmt.Sprintf(`你是专业抖音 AI 短剧分镜师。将剧本拆分为多个独立镜头，必须覆盖剧本中的每一个场次、关键动作和对白段落，不得把整集压缩成单镜。

硬性要求：
- 至少输出 %d 个镜头（shot_number 从 1 连续编号）
- 每个场次至少 2 个镜头（建立镜头 + 细节/对白镜头）
- 对白、动作转折、情绪变化处应单独成镜

连贯分镜规则（减少镜间割裂）：
- 单镜时长统一 2.5 秒；一句短台词 = 1 镜 (2.5s)；长台词拆成 2 镜 (各 2.5~5s)，禁止混用混乱时长
- 景别递进：同场景内优先 全景→中景→近景→特写，禁止全景直接跳超大特写（中间加中景过渡）
- 连续运镜：相邻镜头运镜方向一致，如「缓慢 dolly in」→「继续小幅 dolly in 特写」，避免一镜环绕下一镜拉远
- 转场衔接：相邻镜 description 预留动作衔接点（上一镜转头/抬手/遮挡 → 下一镜顺接该动作或同物体入镜）
- 同场景：scene 相同的多镜，prompt 中人物服装发型 character_id 描述一字不改，仅换景别与动作
- 跨场景：只换背景/场景，人物基础 character_id 与 style: consistent 保持不变

必须只输出 JSON 数组，不要 markdown 说明文字。每项字段：
- shot_number (int) 镜头序号
- scene (string) 场景名
- description (string) 中文画面描述（含动作、情绪、光影氛围；与上一镜动作可衔接）
- camera (string) 运镜：中文简述 + 英文术语，如「缓慢推镜 slow dolly in」「固定 locked-off」；相邻镜标注连续运镜关系
- duration (float) 秒数，默认 2.5，上限 5
- prompt (string) 英文 AI 绘画提示词，须含：景别( wide/medium/close-up )、与 camera 一致的运镜术语、PBR 渲染细节
- asset_ids (int[]) 本镜出现的资产 id，必须从下方资产清单选取（无资产时可省略）
- prompt 须通过内容安全：禁止 blood/gore/nudity/裸露/血腥/残忍伤害；战斗用 stylized anime action、energy effects、dynamic pose
- prompt 中出场角色须含 character_id: [name], style: consistent 及特征关键词（全片统一复制，勿每镜重写人设）
- prompt 须含渲染锚点：Unreal Engine 5 render, Octane Render, high fidelity, consistent lighting，并标明 16:9 或 9:16 与统一暖/冷色调

示例：[{"shot_number":1,"scene":"界海边缘","description":"石昊猛然起身，赤红双目","camera":"缓慢推镜 slow dolly in","duration":2.5,"asset_ids":[12],"prompt":"3D anime, wide shot, character_id: ShiHao, style: consistent, Unreal Engine 5 render, consistent lighting, vertical 9:16, slow dolly in..."}]`, minShots)

	systemPrompt += FormatAssetsForStoryboardPrompt(assets)
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
