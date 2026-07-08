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
		userPrompt = fmt.Sprintf("上次拆分偏碎或镜头过少（约需 %d 支长镜）。请按「少镜、长镜、多 beats」重拆：合并连续动作，每镜 10–18s 且 beats≥4，覆盖全部场次。\n\n%s", minShots, userPrompt)
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
// Field names use 「」 instead of backticks to keep the Go string valid.
const storyboardParseSystemTemplate = `你是专业抖音 AI 短剧分镜师。采用「少镜、长镜、多关键帧」策略：把相邻连续动作合并成一支 10–18 秒的长镜，用 beats 承载情节推进，让单次关键帧视频一镜到底，减少碎镜拼接对成片流畅度的破坏。
## 硬性镜头拆分强制要求
1. 输出镜头总量约为 %d 支（可上下浮动 1–2 支以保证覆盖完整），shot_number 从1开始连续顺编，无断号、跳号；
2. **禁止碎拆**：同一场景、时间连续的动作/对白，必须合并进同一支长镜，用 beats 标出时间节点；不要把「抬眼」「站起」「挥手」拆成三支独立短镜；
3. **拆镜边界仅限**：换场景、时间跳跃/闪回、视角硬切、段落分隔。同一对话线程、同一打斗回合、同一情绪弧线 = 一支镜；
4. 单一场景可以只有 1 支长镜；仅当同场景内确实需要换景别且时间/动作有明显断点时，才拆出第 2 支镜（第 2 支用 scene_link=continuous）。

## 画面连贯统一规范（长镜 + 关键帧）
1. duration 取值区间 **10.0–18.0 秒**（0.5 步进），**最短 10 秒**；
   - 常规叙事/对白长镜：12–15s；
   - 打斗、运镜高潮、多情节推进：15–18s；
   - 本集镜头总数应明显少于传统碎镜方案（典型一集 4–8 支长镜）；
2. 每支长镜必须用 beats 把剧情塞满：约每 2.5–3.5 秒一个拍点，10s≈4 拍、15s≈5–6 拍、18s≈6–7 拍；
3. 同场景人物锁定：同一 scene 下角色 character_id、发型、服饰、五官特征完全一致；
4. 跨场景仅替换环境，人物人设与 style: consistent 全程固定。

## 对白与旁白
1. dialogue 仅写角色口播「角色名：台词」；第三人称旁白禁止写入 dialogue；
2. duration 与台词匹配：字数 ≈ duration×3.5（12s 约 40 字、15s 约 50 字），上限 duration×5；台词过长可在本镜 beats 中分段口播，**不要**为了拆对白再拆镜；
3. 无对白时 dialogue 必须为 ""。

## 字段隔离强制约束
1. 对白只能出现在 dialogue；description 禁止「角色名：台词」；
2. description 禁止以「特写：」「近景：」等景别词冒号开头，景别写入 camera。

## 输出格式强制约束
最终仅输出一个纯 JSON 对象 {"shots":[ ... ]}，禁止 Markdown、说明、代码围栏。每镜字段：
1. shot_number：int；
2. scene：string 场景名；
3. description：string 中文，概括本镜整段连续情节（不是单一瞬间）；
4. camera：string 「中文 + 英文运镜术语」，可描述整段运镜变化；
5. duration：float，10.0–18.0；
6. prompt：string 英文绘图提示（首关键帧构图）；角色镜含 character_id（仅 type=role）与 style: consistent；道具/环境镜禁止 character_id；须含 Unreal Engine 5 render、frame-to-frame continuity、vertical 9:16；
7. lighting：string，同场景一致；
8. action_continue：string，承接上一镜末拍；首镜写「开场」；
9. transition：scene_link=transition 时填 soft dissolve | fade black | match cut；continuous 时留空；
10. scene_link：continuous | transition（第1镜固定 transition）；同场景连续动作 = continuous；
11. dialogue：string，「角色名：台词」或 ""；
12. asset_ids：int[]；
13. beats：**必填**，至少 4 项，建议约 duration/3 项（上限 7）。每项 {"time": float 相对本镜起点秒数，从 0 严格递增且 < duration, "action": string 该时刻具体画面动作/情绪/对白口型}。beats 是本镜情节载体，须覆盖开端→推进→转折/高潮→收束，每拍动作有具体进展，禁止复制同一句空话。

## 额外规则
1. 全局色调锁定，禁止单镜色温漂移；
2. 运镜以缓慢匀速推拉/环绕为主；
3. 角色外观全程不变。

示例：{"shots":[{"shot_number":1,"scene":"混沌虚空","description":"死寂虚空中焦黑树桩缓缓显形，金粒子升起，镜头从全景缓慢推近树桩裂纹","camera":"缓慢推镜 slow dolly in 建立全景","duration":12.0,"lighting":"cold gray ambient","action_continue":"开场","transition":"fade black","scene_link":"transition","asset_ids":[3],"beats":[{"time":0.0,"action":"虚空远景，焦黑树桩剪影立于中央"},{"time":3.5,"action":"镜头缓慢前推，树皮裂纹与焦痕显露"},{"time":7.0,"action":"金粒子从裂纹渗出，盘旋上升"},{"time":10.5,"action":"近景定在树桩正面，金光微亮"}],"prompt":"3D anime, wide low-angle establishing, charred black tree stump inanimate prop, dead gray void, golden particles, no human character, Unreal Engine 5 render, frame-to-frame continuity, vertical 9:16"},{"shot_number":2,"scene":"界海边缘","description":"石昊在废墟上从悲恸站起，封印残魂，凝聚神力一击贯穿敌人","dialogue":"石昊：柳神，这一战我不会退。","camera":"环绕转推 slow orbit into medium then push","duration":16.0,"lighting":"warm golden key light","action_continue":"承接树桩金光","transition":"soft dissolve","scene_link":"transition","asset_ids":[12],"beats":[{"time":0.0,"action":"石昊跪地，目光盯着前方废墟"},{"time":3.0,"action":"起身，掌心压住残魂光团"},{"time":6.5,"action":"残魂封印，眼神转冷，抬手聚金光"},{"time":10.0,"action":"挥臂掷出能量束"},{"time":13.5,"action":"能量贯穿敌人，强光爆开，石昊站定"}],"prompt":"3D anime, medium shot, character_id: ShiHao, gathering golden energy, style: consistent, Unreal Engine 5 render, frame-to-frame continuity, vertical 9:16"}]}`
