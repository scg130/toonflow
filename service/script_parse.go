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
## 硬性镜头拆分强制要求
1. 输出镜头总量必须达到指定数量`%d`，`shot_number`从1开始连续顺编，无断号、跳号；
2. 单一场景镜头下限：每个scene至少2支镜头，分为场景全景建立镜头+人物细节/台词镜头；
3. 拆分规则：单句对白、人物动作转折、情绪剧烈变化、画面关键事件，必须单独拆分独立镜头。

## 画面连贯统一规范（解决玄幻史诗镜头跳变、画风割裂）
1. 时长统一标准：所有基础镜头固定2.5秒；单句短台词对应1支2.5s镜头；长篇对白拆分为2支镜头，单支时长区间2.5–5秒，全片时长规格统一，不混用杂乱时长；
2. 景别递进逻辑：同一场景镜头严格遵循「全景wide→中景medium→近景close-up→特写extreme close-up」递进，全景禁止直接跳转超大特写，中间必须增加中景镜头过渡；
3. 连续运镜约束：相邻镜头运镜运动方向、运动类型保持统一，例：「缓慢推镜 slow dolly in」→「延续小幅推镜 slow dolly in 特写」，严禁前镜环绕运镜、后镜突然拉远，所有相邻镜头必须标注延续上镜运动关系；
4. 帧间动作衔接：前后镜头`description`必须预留承接动作节点，上一镜头收尾抬手、抬头、光影遮挡、浮空物体，下一镜头顺接该动作/同款物体入镜，实现无缝画面衔接；
5. 同场景人物锁定：同一`scene`下全部镜头的prompt内，角色`character_id`、发型、服饰、五官特征文字完全不变，仅修改景别、动作、运镜关键词；
6. 跨场景人设保留：切换场景仅替换环境背景，角色基础`character_id`、`style: consistent`人设参数全程固定不变。

## 输出格式强制约束
最终仅输出纯JSON数组，禁止附带任何Markdown、文字说明、注释内容，数组内每支镜头对象固定字段规则：
1. `shot_number`：int，镜头序列号；
2. `scene`：string，镜头所属场景名称；
3. `description`：string，中文画面描述，包含人物动作、内心情绪、史诗光影氛围，适配东方玄幻史诗氛围，可包含浮空仙城、金色符文锁链、云海雷云、漫天修士等画面元素描述，内容必须适配前后镜头动作衔接逻辑；
4. `camera`：string，运镜描述，固定格式「中文动作描述 + 专业英文运镜术语」，相邻镜头标注延续上镜运动关系，示例：延续上镜缓慢推镜 slow dolly in、固定机位 locked-off；
5. `duration`：float，默认值2.5，最大值不超过5；
6. `prompt`：string，英文AI绘图提示词，强制包含以下全部要素，适配抖音9:16竖版玄幻国漫质感：
    - 对应景别关键词：wide / medium / close-up / extreme close-up；
    - 和camera字段完全匹配的英文运镜术语；
    - PBR材质、高精度纹理渲染细节；
    - 出场角色固定标识 `character_id: [角色名], style: consistent`，全片统一复用角色特征关键词，禁止每镜重复撰写完整人设；
    - 固定渲染锚点：`Unreal Engine 5 render, Octane Render, high fidelity, consistent global lighting`；
    - 画幅强制标注`vertical 9:16`，统一标注全局暖金色史诗色调/冷暗雷云色调；
    - 连贯锁参数：`frame-to-frame continuity, zero model mutation, no random color shift, smooth motion transition`，杜绝人物变形、光影跳变；
    - 基础画风锚定：3D oriental epic fantasy anime, perfect world manhua aesthetic, glowing golden rune chains, floating ancient celestial city, storm dark sky, soft volumetric light；
    - 安全内容约束：禁止blood/gore/nudity/裸露/血腥/残忍伤害；打斗、玄幻对战画面使用`stylized oriental anime action, golden energy rune effects, dynamic heroic pose`；
7. `asset_ids`：int[]，填写本镜头出现资产ID，无对应资产时可省略该字段。

## 额外画面优化强制规则
1. 所有镜头prompt统一史诗玄幻光影逻辑，全程锁定全局色调，禁止单镜头明暗、色温随机偏移；
2. 宏大浮空城、群仙对战场景优先使用wide全景起镜，再逐步dolly in推进至人物特写；
3. 运镜以缓慢匀速推拉为主，少用剧烈环绕、快速变焦，贴合抖音短剧舒缓史诗镜头质感；
4. 角色全套外观特征固定写入character_id配套描述，全片所有镜头不修改人物发型、服饰、配饰、发色。

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
