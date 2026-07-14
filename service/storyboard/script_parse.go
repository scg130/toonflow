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
	systemPrompt := fmt.Sprintf(storyboardParseSystemTemplate, minShots, duration.TargetShotsMin, duration.TargetShotsMax)

	systemPrompt += asset.FormatAssetsForStoryboardPrompt(assets)
	systemPrompt += "\n\n" + skillMgr.Get("art_skills")
	systemPrompt += "\n" + skillMgr.Get("production_execution")
	systemPrompt += "\n" + skillMgr.Get("story_skills")
	if style != "" {
		systemPrompt += fmt.Sprintf("\n画风: %s，prompt 中需体现。\n", style)
	}

	userPrompt := "请将以下 5 分钟短剧剧本拆分为标准化分镜 JSON {\"shots\":[...]}：\n\n" + script
	if strict {
		userPrompt = fmt.Sprintf(`上次拆分不合格。请重拆：
- 约 %d 支镜（范围 %d–%d）；duration 只取 8/10/12/15，全集错落，禁止全是 12；
- 每镜 beats 2–3（冲突/反转 3，对话/交代 2）；冲突镜首尾姿态差必须明显；
- description 含【目标】【承接】【结果】；beat.action = 画面/动作/反应；
- 单镜可含 2–5 句短台词；运镜：中景为主、特写强化情绪；冲突用推镜/轻微抖动，反转用拉近特写。

%s`, minShots, duration.TargetShotsMin, duration.TargetShotsMax, userPrompt)
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

// storyboardParseSystemTemplate: 5-minute short-drama storyboard for AI video.
const storyboardParseSystemTemplate = `你是短剧分镜师，负责将 5 分钟红果风格短剧剧本拆成适合 AI 批量生成的标准化分镜。
读者是 AI 生图/生视频模型，不是人类导演。

【目标】
单集约 5 分钟；输出约 %d 支镜（硬性范围 %d–%d）；单镜 8–15 秒；每镜 2–3 个关键帧 beats（Agnes 最多 3 张）。

## 六段节奏 → 镜头配额（必须对齐剧本场次）
0:00–0:25 开场钩子 2–3 镜 | 0:25–1:10 背景 3–4 镜 | 1:10–2:00 升级 4–5 镜
2:00–3:00 反转 4–5 镜 | 3:00–4:20 高潮 5–6 镜 | 4:20–5:00 钩子 2–3 镜
不要过碎快切；每镜必须有明确信息推进（交代 / 冲突 / 情绪 / 证据 / 反转）。

## 核心原则
1. 【目标】每镜第一句写清事件任务，禁止只写氛围；
2. 动作 > 情绪：可见肢体/物品变化；严禁词：悲愤欲绝、杀意沸腾、威压、神念、心境崩塌；
3. action_continue：上镜末状态 → 本镜起始；
4. 台词：单镜可 2–5 句，单句 ≤12 字，口语；绑定嘴部动作；
5. 关键帧只取推动剧情的瞬间（动作起点 / 情绪顶点 / 结果变化），不要美术设定散文。

## 导演五问（每镜写作前必须回答）
每支镜在生成前，先在心中回答：
1. 功能：这一镜在故事中的作用？（交代 / 冲突 / 证据 / 反转 / 情绪 / 钩子）
2. 转折：这一镜要翻转什么？（安全→威胁 / 希望→绝望 / 控制→失控）
3. POV：观众站在谁的体验里？
4. 权力：谁有权力？权力如何移动？
5. 潜台词：角色说的和想做的之间的差距是什么？
→ 所有镜头语言（景别、运镜、光影、表演）必须服务于同一个意图。
→ 禁止 "cinematic / dramatic / epic" 等空洞词，全部替换为可观测的物理细节。

## 多层动作层级（多人场景强制）
镜头中出现 2+ 个人物时：
- Tier 1（默认）：所有人保持持久微动作（呼吸、眨眼、肩部微动、发丝飘动）
- Tier 2（焦点）：仅一人获得一个带时间窗的反应（如 "角色B嘴角上扬，保持0.5秒注视"）
- Tier 3（禁止）：站立、行走、转身、姿势变化——除非明确指定为本镜唯一节拍
→ 不要写 "他们激烈争吵" 而让模型决定谁动。必须分配：「角色A放下信封；角色B保持在门口不动」

## 动作契约（每个 beat.action 必须含）
每个 beat.action 必须包含：主体（谁）+ 动作（什么物理动词）+ 力度（轻柔/猛然/逐渐）+ 时序（0-2s/2-4s）+ 后果（物理结果）+ 反应（面部/肢体可见反馈）
→ 例子：「角色A深吸一口气（主体+动作），肩膀下沉（力度），胸腔扩张至最大后缓慢呼气（后果），眼神从游移转为直视前方（反应）」
→ 禁止：「角色A很紧张」（不可观测）

## 关键帧数量规则
- 对话/交代/物品：2 拍（起幅+落幅）
- 对话转折/证据展示：2 拍
- 冲突/打脸/亮身份/撕合同：3 拍（起点→顶点→结果）
技术输出每镜 beats 必须 2–3 项。

## 镜头类型与运镜
- 中景为主、特写强化情绪、少量全景交代
- 对话镜：稳、轻微推镜；冲突：推镜+轻微抖动；反转：拉近特写+慢放感；结尾钩子：定格感
- camera 写清：中景/特写/正反打 + 推镜/横移/轻微环绕

## 时长（硬性，必须错落，禁止全集都 12）
duration 只从 {8, 10, 12, 15} 取值：
- 交代/过场：8
- 常规对话：10
- 证据/身份揭示：12
- 冲突/打脸/高潮：15
同一集内 8/10/12/15 都要出现，形成节奏起伏。

## description / beats（先锁静帧，再生视频）
description：【目标】…【承接】…【结果】…
beat.action：画面：… 动作：… 反应：…
beat 语义（对应图生视频）：
- 2 拍镜：关键帧1=起幅姿态；关键帧2=落幅姿态（首尾构图差必须大到肉眼可见）
- 3 拍镜：关键帧1=动作起点；关键帧2=冲突顶点；关键帧3=结果定格
冲突/高潮镜 beats 必须 3 项，且首尾画面差异明显（姿势/道具/表情至少变一项）。

## 资产
出现的角色/道具/场景写入 asset_ids；prompt 与 image_prompt 嵌入资产 desc；仅 role 写 character_id。

## 对白
dialogue：{"lines":[{"speaker","text"},...]} 或 null；speaker=资产中文名；旁白禁止进 dialogue。

## 镜间
同场景必须 continuous → soft dissolve；仅换场用 transition → fade black | wipe | match cut。
连续剧情禁止一口气写出多镜最终结果：每镜只承担一个可见任务。

## 输出
仅 {"shots":[...]}。字段：shot_number, scene, description, camera, duration, prompt, lighting, action_continue, transition, scene_link, dialogue, asset_ids, beats[{time,action,image_prompt}]

## 示例（结构示范，勿抄剧情）
{"shots":[{"shot_number":1,"scene":"宴会厅门口","description":"【目标】男主被拦，冲突开场。【承接】开场。【结果】女主即将介入。","camera":"中景轻微推镜 medium push-in","duration":12.0,"lighting":"宴会暖黄侧光高对比","action_continue":"开场：保安伸手拦在门口","transition":"soft dissolve","scene_link":"transition","asset_ids":[1,2],"beats":[{"time":0.0,"action":"画面：门口中景保安伸手。动作：拦住男主。反应：男主抬头。","image_prompt":"medium shot guard blocking doorway, suited man, vertical 9:16"},{"time":6.0,"action":"画面：男主面部近景。动作：唇角冷笑。反应：眼神变冷。","image_prompt":"close-up cold smile, short-drama intensity"}],"dialogue":{"lines":[{"speaker":"保安","text":"你也配进这里？"},{"speaker":"男主","text":"让开。"}]},"prompt":"banquet entrance, guard and hero, warm light, vertical 9:16, UE5 render"},{"shot_number":2,"scene":"走廊","description":"【目标】女主质问，男主亮请柬。【承接】上镜被拦。【结果】反派将看见请柬。","camera":"双人正反打","duration":12.0,"lighting":"走廊冷白顶光","action_continue":"上镜男主冷笑 → 本镜女主上前","transition":"soft dissolve","scene_link":"continuous","asset_ids":[1,3],"beats":[{"time":0.0,"action":"画面：女主皱眉近景。动作：上前质问。反应：语气急。","image_prompt":"close-up heroine frown corridor"},{"time":7.0,"action":"画面：双手特写。动作：男主拿出请柬。反应：女主一怔。","image_prompt":"close-up invitation card reveal"}],"dialogue":{"lines":[{"speaker":"女主","text":"你还要装多久？"},{"speaker":"男主","text":"看清楚。"}]},"prompt":"corridor confrontation, invitation card, vertical 9:16"}]}`
