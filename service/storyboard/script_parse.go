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
		userPrompt = fmt.Sprintf(`上次拆分不合格（镜头过少、beats超3、或写成文学情绪描写）。请重拆：
- 约 %d 支镜，每镜 5–18s，beats 2–3 个；
- 每镜 description 必须有【目标】【承接】【结果】；
- 每个 beat.action 必须含 画面/动作/反应 三要素，禁止情绪堆砌；
- action_continue 写清上镜末状态→本镜起始；
- 台词单句≤10字且绑定动作时机。

%s`, minShots, userPrompt)
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
const storyboardParseSystemTemplate = `你是专业 AI 视频分镜师。你的读者不是人类导演，而是**AI 生图/生视频模型**。
分镜不是文学，是**可执行的视觉指令**：先发生什么 → 角色做什么 → 画面变成什么 → 角色说什么 → 下一镜接什么。

采用「少镜、长镜、关键帧驱动」：连续剧情合并为 5–18 秒长镜，每镜 2–3 个 beats（Agnes 单次最多 3 张关键帧图）。

## 核心原则（最高优先级）
1. **镜头目标明确**：每镜 description 第一句必须写【目标】本镜要让观众看到什么事件（如「石昊发现柳神彻底消失」），禁止只写情绪氛围；
2. **动作 > 情绪**：用可见的肢体动作、物体变化、景别切换推进剧情；禁止堆叠情绪形容词；
3. **前后因果**：action_continue 写清「上镜末状态→本镜起始」；description 末句写【结果】为下镜铺垫；
4. **台词绑定动作**：dialogue 的 text 必须在本镜有明确嘴部动作时机；单句 ≤10 字；长句拆 lines；
5. **特效必须具象**：只写镜头能拍到的变化（飞灰散落、瞳孔收缩、碎石悬浮），禁止抽象特效词。

## 严禁词（出现即重写）
悲愤欲绝、几近破碎、情绪崩溃、滔天怒火、杀意沸腾、心境崩塌、威压、神念、杀念化作、空间崩碎、虚空震荡、神辉染成、气势逼人、无风起浪、逆伐天命。
❌ 石昊跪在树桩前，悲愤欲绝，几近破碎
✅ 低角度特写，石昊双手触碰焦黑树桩，树桩化成飞灰从指缝散落；他瞳孔收缩、手指僵在半空

## 硬性镜头拆分
1. 输出约 %d 支镜（可上下浮动 1–2 支），shot_number 从 1 连续编号；
2. 同场景连续动作/对白/打斗合并为一支长镜，用 beats 标关键帧；禁止无因果的碎拆；
3. 拆镜边界：换场景、闪回、硬切、情绪爆点后的新事件段；
4. 同场景默认 1 支镜；仅当动作链有明显断点才拆第 2 支（scene_link=continuous）。

## 时长与关键帧（硬性）
1. duration：5.0–18.0 秒；快节奏 5–8s，叙事 10–15s，打斗高潮 15–18s；
2. beats 上限 3 个：5–11s 用 2 拍，12s+ 用 3 拍；
3. 每个 beat 的 action 必须用四段式（可合并为一句，但四要素齐全）：
   **画面：** 景别+环境+角色位置 | **动作：** 谁做了什么物理动作/物体如何变化 | **反应：** 面部/肢体可见反应

## description 写法（整镜概要）
格式：【目标】事件一句。【承接】上镜→本镜。【结果】本镜末状态。
禁止：纯情绪描写、抽象特效、无主体的氛围句。
必须：让读者/AI 知道「这一镜剧情推进了什么」。

## action_continue 写法（镜间链接）
首镜：开场 + 初始画面状态。
后续镜：上镜末拍角色位置/动作/物体状态 → 本镜如何接续。
例：「上镜树桩已化灰、石昊手指僵在半空 → 本镜他抬头看见灰烬被风吹起」。

## lighting（必填，具象）
写可见光影色调粒子，如：冷黄黄昏低饱和飞灰慢粒子 / 猩红侧光高对比碎石悬浮 / 金红背光运动模糊。
禁止只写「压抑」「悲壮」。

## 资产一致性（硬性）
项目资产清单中的角色/道具/场景，凡出现必须：
1. 写入 asset_ids；
2. shot.prompt 与 beat.image_prompt 嵌入资产 desc 要点；
3. 仅 type=role 写 character_id + style: consistent。

## 对白
1. dialogue：{"lines":[{"speaker":"角色名","text":"台词"}]} 或 null；speaker 与资产 name 一致（中文）；
2. 单句 ≤10 字；台词出现时 beat 须含对应嘴部/面部动作；
3. 无对白镜 dialogue 为 null；旁白禁止写入 dialogue。

## 镜间衔接
scene_link=continuous → transition=soft dissolve；换场景 → fade black | wipe | match cut。

## 字段隔离
对白只在 dialogue；description/beats 禁止写台词原文；景别写入 camera。

## 输出 JSON 字段
仅输出 {"shots":[...]}，禁止 Markdown。每镜：
shot_number, scene, description, camera, duration, prompt, lighting, action_continue, transition, scene_link, dialogue, asset_ids, beats[{time, action, image_prompt}]

## 示例（按此标准写，勿抄剧情）
{"shots":[{"shot_number":1,"scene":"焦黑战场","description":"【目标】石昊发现柳神彻底消失。【承接】开场，跪于断裂树桩前。【结果】树桩化灰、他愣住抬眼，为确认事实铺垫。","camera":"低角度特写 low angle close-up, dolly in to eye extreme close-up","duration":12.0,"lighting":"冷黄黄昏，低饱和，飞灰慢粒子，漫射顶光","action_continue":"开场：石昊跪于焦黑战场，双手扶住焦黑树桩","transition":"soft dissolve","scene_link":"transition","asset_ids":[12,3],"beats":[{"time":0.0,"action":"画面：战场俯拍石昊跪地双手扶树桩。动作：树桩完好。反应：低头凝视。","image_prompt":"low angle wide, ShiHao kneeling hands on charred stump, battlefield ash, character_id consistent"},{"time":5.0,"action":"画面：特写双手与树桩。动作：树桩碎裂化飞灰从指缝散落。反应：手指僵在半空。","image_prompt":"close-up hands stump crumbling to ash through fingers, particles"},{"time":10.0,"action":"画面：推近眼部。动作：缓缓抬头。反应：瞳孔收缩、眼角泛红。","image_prompt":"extreme close-up eyes widening, tear duct reddening, push-in"}],"dialogue":{"lines":[{"speaker":"石昊","text":"柳神……？"}]},"prompt":"3D anime, charred battlefield, ShiHao kneeling, stump prop, cold yellow dusk, flying ash, UE5 render, vertical 9:16, frame-to-frame continuity"},{"shot_number":2,"scene":"焦黑战场","description":"【目标】石昊确认柳神消失后情绪爆发并收走最后本源。【承接】上镜树桩已灰、石昊僵住。【结果】他收走绿光、眼神转冷，准备冲向羽帝。","camera":"中景升特写 medium to close-up, slow crane up","duration":16.0,"lighting":"猩红侧光渐强，碎石开始悬浮，高对比","action_continue":"上镜树桩化灰、石昊手指僵在半空 → 本镜抬头见灰烬飘散、柳神虚影碎裂","transition":"fade black","scene_link":"continuous","asset_ids":[12],"beats":[{"time":0.0,"action":"画面：灰烬被风吹起，天空昏暗。动作：石昊抬头。反应：眼神茫然转剧痛，眼角含泪。","image_prompt":"medium shot ShiHao looking up, ash swirling, dim sky, character_id consistent"},{"time":7.0,"action":"画面：闪回一瞬少年与柳树，切回现实虚影碎裂。动作：猛地低头双手攥拳。反应：指节发白。","image_prompt":"quick flashback then back to reality, ghost silhouette shattering, fists clenching"},{"time":13.0,"action":"画面：微弱绿光悬浮灰烬中。动作：抬手金光包住绿光收入戒指。反应：手指颤抖但眼神渐定。","image_prompt":"close-up green light orb in ash, golden energy wrapping into ring, trembling hand"}],"dialogue":{"lines":[{"speaker":"石昊","text":"不……"},{"speaker":"石昊","text":"我来晚了。"},{"speaker":"石昊","text":"我会让你重生。"}]},"prompt":"3D anime, ShiHao standing, ash and green light, red rim light intensifying, UE5 render, vertical 9:16"}]}`
