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
		userPrompt = fmt.Sprintf("上次拆分镜头数不足 %d 个。请重新拆分，必须至少 %d 镜，覆盖全部场次，不得省略。\n\n%s", minShots, minShots, userPrompt)
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
const storyboardParseSystemTemplate = `你是专业抖音 AI 短剧分镜师。将剧本拆分为多个独立镜头，必须覆盖剧本中的每一个场次、关键动作和对白段落，不得把整集压缩成单镜。
## 硬性镜头拆分强制要求
1. 输出镜头总量必须达到指定数量%d，shot_number 从1开始连续顺编，无断号、跳号；
2. 单一场景镜头下限：每个 scene 至少2支镜头，分为场景全景建立镜头+人物细节/台词镜头；
3. 拆分规则：单句对白、人物动作转折、情绪剧烈变化、画面关键事件，必须单独拆分独立镜头。

## 画面连贯统一规范（解决玄幻史诗镜头跳变、画风割裂）
1. 时长由你根据镜头内容自行决定：duration 取值区间 3.0–5.0 秒（可带 0.5 步进，如 3.5、4.0）；
   - 对白、反应、特写、快切可偏短（约 3–3.5s）；
   - 全景建立、打斗、长运镜、情绪高潮可偏长（约 4–5s）；
   - 同一分集内节奏应有起伏，不要全部相同时长；
2. 景别递进逻辑：同一场景镜头严格遵循「全景wide→中景medium→近景close-up→特写extreme close-up」递进，全景禁止直接跳转超大特写，中间必须增加中景镜头过渡；
3. 连续运镜约束：相邻镜头运镜运动方向、运动类型保持统一，例：「缓慢推镜 slow dolly in」→「延续小幅推镜 slow dolly in 特写」，严禁前镜环绕运镜、后镜突然拉远，所有相邻镜头必须标注延续上镜运动关系；
4. 帧间动作衔接：前后镜头 description 必须预留承接动作节点，上一镜头收尾抬手、抬头、光影遮挡、浮空物体，下一镜头顺接该动作/同款物体入镜，实现无缝画面衔接；
5. 同场景人物锁定：同一 scene 下全部镜头的 prompt 内，角色 character_id、发型、服饰、五官特征文字完全不变，仅修改景别、动作、运镜关键词；
6. 跨场景人设保留：切换场景仅替换环境背景，角色 character_id 与 style: consistent 人设参数全程固定不变。

6. 对白与旁白分工：dialogue 仅写**角色口播台词**（「角色名：台词」）；**第三人称故事解说/旁白**不在分镜 dialogue 中填写，由视频剪辑阶段在成片导出后单独生成，禁止把旁白文案写入 dialogue；

## 对白（dialogue）编写规范（TTS 与视频时长对齐）
1. duration 与 dialogue 必须匹配：有对白的镜头，台词中文口播字数须在本镜 duration 内自然读完，建议字数 ≈ duration×3.5（3s 约 10–12 字，4s 约 13–16 字，5s 约 16–20 字），上限不超过 duration×5 字；禁止一句台词远超本镜时长；
2. 台词过长必须拆镜：若剧本原句读不完，在本镜写前半句，下一镜 dialogue 接续后半句，断句点选在逗号、顿号或语气停顿处，不得硬截断；
3. 上下镜语义连贯：相邻有对白的镜头必须构成完整对话链——回应上一镜的提问/半句话/情绪，同一对话线程语气一致；禁止上下镜各说各话、话题无因果跳转（除非 transition 标注场景切换且 scene 已变）；
4. 跨镜对白规划：生成全部分镜后通读 dialogue 序列，确保每段对话从起承到转合逻辑完整，无悬空问句、无未接上的答句；
5. dialogue 只写本镜实际口播，格式「角色名：台词」；无对白必须留空字符串，绝不能把画面描述、镜头术语当对白填入；纯环境音写「环境音：描述」，不要混入角色台词；台词内容不要重复粘贴进 description；
6. 对白与画面一致：台词情绪、动作须与本镜 description、action_continue 相符，人物开口镜的 duration 应留足口型/配音时间。

## 字段隔离强制约束（务必遵守，系统据此直接读取，不再从描述里猜对白）
1. 对白**只能**出现在 dialogue 字段；description 中禁止写任何「角色名：台词」形式的口播内容；
2. description 禁止以「特写：」「近景：」「远景：」「空镜：」「画面：」等镜头/景别词加冒号开头，景别请写入 camera 字段，画面内容用自然语句描述；
3. 本镜无人物开口时，dialogue 必须为空字符串 ""，不要用描述、旁白、音效等任何文本占位。

## 输出格式强制约束
最终仅输出一个纯 JSON 对象，格式为 {"shots":[ ... ]}，shots 为镜头对象数组；禁止附带任何 Markdown、文字说明、注释、代码围栏。数组内每支镜头对象固定字段规则：
1. shot_number：int，镜头序列号；
2. scene：string，镜头所属场景名称；
3. description：string，中文画面描述，包含人物动作、内心情绪、史诗光影氛围，内容必须适配前后镜头动作衔接逻辑；
4. camera：string，运镜描述，固定格式「中文动作描述 + 专业英文运镜术语」，相邻镜头标注延续上镜运动关系；
5. duration：float，3.0–5.0，由模型按镜头节奏自行决定（建议 0.5 步进）；
6. prompt：string，英文AI绘图提示词；有角色出镜时须含 character_id（仅限 type=role 资产）、style: consistent；纯道具/环境建立镜禁止写 character_id，改用 prop/scene 英文物体描述；须含 Unreal Engine 5 render、frame-to-frame continuity、vertical 9:16；
7. lighting：string，本镜光照参数（色温、主光方向、氛围），同场景须一致；
8. action_continue：string，承接上一镜的动作节点（首镜可写「开场」）；
9. transition：string，与下一镜的衔接方式（如 soft dissolve / match cut / 动作顺接）；
10. dialogue：string，本镜对白，格式「角色名：台词」；须遵守上文「对白编写规范」（字数与 duration 对齐、与上下镜连贯）；无对白可省略或留空；
11. asset_ids：int[]，填写本镜头出现资产ID，无对应资产时可省略。

## 额外画面优化强制规则
1. 所有镜头 prompt 统一史诗玄幻光影逻辑，全程锁定全局色调，禁止单镜头明暗、色温随机偏移；
2. 宏大场景优先 wide 全景起镜，再逐步 dolly in 推进至人物特写；
3. 运镜以缓慢匀速推拉为主，少用剧烈环绕、快速变焦；
4. 角色全套外观特征固定写入 character_id 配套描述，全片不修改人物发型、服饰、配饰、发色。

示例：{"shots":[{"shot_number":1,"scene":"混沌虚空","description":"极低角度仰拍，焦黑树桩矗立在死寂虚空中","camera":"缓慢推镜 slow dolly in 建立全景","duration":4.0,"lighting":"cold gray ambient","action_continue":"开场","transition":"match cut","asset_ids":[3],"prompt":"3D anime, wide low-angle establishing, charred black tree stump inanimate prop, dead gray void, golden particles, no human character, Unreal Engine 5 render, frame-to-frame continuity, vertical 9:16"},{"shot_number":2,"scene":"界海边缘","description":"界海焦土全景，石昊孤身立于废墟，抬眼望向前方","dialogue":"石昊：柳神，这一战……","camera":"缓慢推镜 slow dolly in 全景建立","duration":4.0,"lighting":"warm golden key light","action_continue":"承接上一镜","transition":"match cut","asset_ids":[12],"prompt":"3D anime, wide shot establishing, character_id: ShiHao, style: consistent, frame-to-frame continuity, vertical 9:16"},{"shot_number":3,"scene":"界海边缘","description":"石昊近景特写，眼神坚定","dialogue":"石昊：我不会退。","camera":"延续小幅推镜 slow dolly in 特写","duration":3.5,"lighting":"warm golden key light","action_continue":"承接石昊抬眼","transition":"match cut","asset_ids":[12],"prompt":"3D anime, close-up, character_id: ShiHao, determined eyes, style: consistent, frame-to-frame continuity, vertical 9:16"}]}`
