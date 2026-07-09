package storyboard

import (
	"context"
	"fmt"
	"strings"

	"toonflow/adapter"
	"toonflow/logger"
	"toonflow/skill"
	"toonflow/task"
	"toonflow/service/asset"
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
		return items, fmt.Errorf("分镜仅 %d 镜，剧本至少需要 %d 镜，请重试生成分镜", len(items), minShots)
	}
	return items, nil
}

func parseScriptOnce(ctx context.Context, script, style string, assets []asset.ProjectAsset, skillMgr *skill.Manager, v adapter.Vendor, minShots int, strict bool) ([]task.StoryboardItem, error) {
	systemPrompt := fmt.Sprintf(storyboardParseSystemTemplate, minShots)

	systemPrompt += asset.FormatAssetsForStoryboardPrompt(assets)
	systemPrompt += "\n\n" + skillMgr.Get("art_skills")
	systemPrompt += "\n" + skillMgr.Get("production_execution")
	systemPrompt += "\n" + skillMgr.Get("story_skills")
	if style != "" {
		systemPrompt += fmt.Sprintf("\n画风: %s，prompt 中需体现。\n", style)
	}

	userPrompt := "请将以下剧本拆分为分镜，输出 JSON 对象 {\"shots\":[...]}：\n\n" + script
	if strict {
		userPrompt = fmt.Sprintf("上次拆分不符合要求（镜头过少、beats 超过 3、或剧情描述过薄）。请重拆：每镜 5–18s，beats 严格 2–3 项，description 写满剧情张力，约需 %d 支镜，覆盖全部场次。\n\n%s", minShots, userPrompt)
	}

	resp, err := v.TextRequest(ctx, adapter.DefaultTextModel, adapter.TextParams{
		Messages: []adapter.TextMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.5,
		MaxTokens:   12000,
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

// storyboardParseSystemTemplate is the LLM system prompt for script→storyboard parsing.
const storyboardParseSystemTemplate = `你是专业抖音 AI 短剧分镜师。采用「少镜、长镜、关键帧驱动」策略：把连续剧情合并成一支 5–18 秒的长镜，每镜规划 2–3 个关键帧拍点（Agnes 单次最多 3 张图），单次生成一镜到底、动作连贯不中断。

## 硬性镜头拆分
1. 输出约 %d 支镜（可上下浮动 1–2 支），shot_number 从 1 连续编号；
2. **禁止碎拆**：同场景、时间连续的动作/对白/打斗回合合并为一支长镜，用 beats 标关键帧；不要把连续动作拆成多支短镜；
3. **拆镜边界**：换场景、时间跳跃/闪回、硬切视角、段落分隔；同一对话线程、同一打斗、同一情绪弧线 = 一支镜；
4. 同场景可只有 1 支镜；同场景内需换景别且动作有明显断点时才拆第 2 支（scene_link=continuous）。

## 时长与关键帧（硬性）
1. duration：**5.0–18.0 秒**（0.5 步进），最短 5 秒、最长 18 秒；
   - 快节奏/情绪点：5–8s；
   - 常规叙事/对白：10–15s；
   - 打斗高潮/多情节：15–18s；
2. **beats 上限 3 个**（严禁第 4 个）：5–11s 用 2 拍（开端+收束），12s+ 用 3 拍（开端+转折+收束）；
3. 每个 beat 的 action 覆盖该时段**完整情节推进**（非单一特效变化），打斗/互动须写清连续动作链，禁止动作中断或跳切描述。

## 剧情描述（description）— 必须丰富
1. description 用中文写**整镜剧情弧**，不是关键帧列表或特效变动清单；
2. **远景/建立镜头**须交代场面全貌：环境、人物站位、局势关系、氛围张力；
3. **角色互动/打斗**须写实、连贯：起手→交锋→结果，写清肢体互动与情绪变化；
4. 禁止只写「金光闪烁」「镜头推近」等空泛效果而无剧情信息。

## 资产一致性（硬性）
项目资产清单中已有的角色/道具/场景，凡在本镜或某 beat 出现，必须：
1. 写入 shot 的 asset_ids；
2. 在 shot.prompt 与对应 beat 的 image_prompt 中**嵌入该资产的 desc 原文要点**（发型、服饰、道具外观、场景特征），确保全片视觉一致；
3. 角色镜写 character_id（仅 type=role）+ style: consistent；道具/场景用英文物体/环境描述，禁止把道具/场景名写入 character_id。

## 对白
1. 镜内有人物开口说话时，dialogue 必填：{"lines":[{"speaker":"角色名","text":"台词"}, ...]}；多角色对话用多条 lines；text 仅中文口播台词；
2. 无对白时 dialogue 为 null；第三人称旁白禁止写入 dialogue；
3. text 字数与 duration 匹配（约 duration×3.5 字）。

## 镜间衔接
1. scene_link=continuous（同场景续接）：transition 填 soft dissolve（淡入淡出衔接）；
2. scene_link=transition（换场景/硬切）：transition 填 fade black | wipe | match cut 等转场特效；
3. action_continue 承接上一镜末拍状态，保证剧情连贯；首镜写「开场」。

## 字段隔离
1. 对白只在 dialogue；description 禁止写台词原文；
2. description 禁止以景别词冒号开头，景别写入 camera。

## 输出 JSON 字段
最终仅输出 {"shots":[...]}，禁止 Markdown。每镜：
1. shot_number：int；
2. scene：string；
3. description：string 中文剧情概要（丰富、连贯）；
4. camera：string 中文运镜 + 英文术语；
5. duration：float，5.0–18.0；
6. prompt：string 英文首帧构图；含 UE5 render、frame-to-frame continuity、vertical 9:16；嵌入资产 desc；
7. lighting：string；
8. action_continue：string；
9. transition：string（见镜间衔接）；
10. scene_link：continuous | transition；
11. dialogue：{"lines":[{"speaker","text"}, ...]} 或 null；
12. asset_ids：int[]；
13. beats：2–3 项，每项 {
    "time": float 从 0 递增且 < duration,
    "action": string 该时段完整剧情/动作（中文，连贯不断裂）,
    "image_prompt": string 英文关键帧构图；须嵌入本拍涉及资产的 desc 要点；远景写清场面全貌，打斗写清肢体互动
}

## 额外规则
1. 全局色调锁定；角色外观全程不变；
2. 运镜以匀速推拉/环绕为主，服务剧情而非炫技。

示例：{"shots":[{"shot_number":1,"scene":"混沌虚空","description":"死寂虚空中，焦黑树桩如墓碑矗立，金色粒子自地底升腾，镜头从俯瞰全场缓缓推近，压迫感渐转为神秘希望","camera":"俯瞰全景 slow crane down 再推近","duration":12.0,"lighting":"cold gray ambient","action_continue":"开场","transition":"fade black","scene_link":"transition","asset_ids":[3],"beats":[{"time":0.0,"action":"远景俯瞰虚空全貌，树桩剪影居中，死寂压抑","image_prompt":"wide aerial establishing shot, full void panorama, charred tree stump centered, asset desc embedded, golden particles rising, oppressive scale"},{"time":6.0,"action":"推近树桩，裂纹渗出金光，粒子盘旋上升","image_prompt":"medium push-in on stump bark cracks, spiraling golden particles, shallow depth"},{"time":11.0,"action":"近景定格树桩正面，金光微亮，氛围转折","image_prompt":"close-up stump front face, faint golden glow, mood shift"}],"prompt":"3D anime, wide establishing, charred tree stump prop per asset desc, dead gray void, Unreal Engine 5 render, frame-to-frame continuity, vertical 9:16","dialogue":null},{"shot_number":2,"scene":"界海边缘","description":"废墟之上石昊跪地悲恸，起身封印残魂后眼神转冷，凝聚神力一击贯穿敌影，整套动作一气呵成","dialogue":{"speaker":"石昊","text":"柳神，这一战我不会退。"},"camera":"环绕转推 slow orbit into push","duration":16.0,"lighting":"warm golden key","action_continue":"承接树桩金光","transition":"soft dissolve","scene_link":"transition","asset_ids":[12],"beats":[{"time":0.0,"action":"石昊跪地凝视废墟，悲恸压抑，全场废墟尽收眼底","image_prompt":"low-angle wide-medium, ShiHao kneeling on ruins panorama, character_id ShiHao per asset desc, grief"},{"time":7.5,"action":"起身封印残魂，眼神转冷，掌心聚金光，连续动作无中断","image_prompt":"medium shot rising action, ShiHao standing sealing soul, golden energy in palms, fluid motion"},{"time":14.5,"action":"挥臂能量束贯穿敌人，强光爆开，站定收势","image_prompt":"dynamic action shot, energy beam piercing enemy, explosive backlight, combat continuity"}],"prompt":"3D anime, character_id: ShiHao per asset desc, style: consistent, Unreal Engine 5 render, vertical 9:16"}]}`
