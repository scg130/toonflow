package service

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var reActionLine = regexp.MustCompile(`(?i)^\*{0,2}ACTION\s*[:：]\s*([a-z0-9_]+(?::\d+)?)\*{0,2}$`)

var (
	reShotBeforeJing = regexp.MustCompile(`第\s*([0-9一二三四五六七八九十两百千]+)\s*镜`)
	reJingBeforeNum  = regexp.MustCompile(`镜\s*([0-9一二三四五六七八九十两]+)`)
)

// ChatActionIntent is a whitelisted workflow command parsed from AI reply or UI.
type ChatActionIntent struct {
	Type   string
	Params map[string]string
}

var allowedChatActions = map[string]struct{}{
	"analyze_events":      {},
	"split_episodes":      {},
	"generate_skeleton":   {},
	"generate_strategy":   {},
	"generate_script":     {},
	"generate_storyboard": {},
	"extract_assets":      {},
	"generate_shot_image": {},
}

// Allowed reports whether the action is in the whitelist.
func (i *ChatActionIntent) Allowed() bool {
	if i == nil || i.Type == "" {
		return false
	}
	_, ok := allowedChatActions[i.Type]
	return ok
}

// Validate checks hard preconditions before execution.
func (i *ChatActionIntent) Validate(episodeID string) error {
	if i == nil || !i.Allowed() {
		return fmt.Errorf("不允许的执行动作")
	}
	needsEpisode := map[string]bool{
		"generate_skeleton": true, "generate_strategy": true, "generate_script": true,
		"generate_storyboard": true, "extract_assets": true, "generate_shot_image": true,
	}
	if needsEpisode[i.Type] && episodeID == "" {
		return fmt.Errorf("请先选择一集")
	}
	if i.Type == "generate_shot_image" {
		n, err := i.ShotNumber()
		if err != nil {
			return err
		}
		if n <= 0 {
			return fmt.Errorf("生图需指定镜号：在 ACTION 前一行输出 SHOT:镜号")
		}
	}
	return nil
}

// ShotNumber parses shot_number param.
func (i *ChatActionIntent) ShotNumber() (int, error) {
	if i == nil || i.Params == nil {
		return 0, fmt.Errorf("生图需指定镜号：在 ACTION 前一行输出 SHOT:镜号")
	}
	raw := strings.TrimSpace(i.Params["shot_number"])
	if raw == "" {
		return 0, fmt.Errorf("生图需指定镜号：在 ACTION 前一行输出 SHOT:镜号")
	}
	if n, err := strconv.Atoi(raw); err == nil && n > 0 {
		return n, nil
	}
	if n, ok := parseChineseShotNumber(raw); ok && n > 0 {
		return n, nil
	}
	return 0, fmt.Errorf("镜号无效: %s", raw)
}

// SanitizeWorkContent removes ACTION/SHOT control lines from planning or script output.
func SanitizeWorkContent(text string) string {
	reply, _ := parseActionFromReply(text)
	lines := strings.Split(reply, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.Trim(line, "`"))
		if act, _, ok := parseActionLine(trimmed); ok && act != "" {
			continue
		}
		upper := strings.ToUpper(trimmed)
		if strings.HasPrefix(upper, "SHOT:") || strings.HasPrefix(upper, "SHOT：") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// PlanningActionWorkType maps generate_* action to o_agent_work work_type.
func PlanningActionWorkType(actionType string) (string, bool) {
	switch actionType {
	case "generate_skeleton":
		return "skeleton", true
	case "generate_strategy":
		return "strategy", true
	case "generate_script":
		return "script", true
	default:
		return "", false
	}
}

// IsSubstantialWorkContent reports whether text looks like generated planning/script body.
func IsSubstantialWorkContent(text string) bool {
	return len([]rune(strings.TrimSpace(text))) >= 150
}

// EnrichIntentFromUserMessage fills missing params when the user already named them in chat.
func EnrichIntentFromUserMessage(intent *ChatActionIntent, userMsg string) {
	if intent == nil || intent.Type != "generate_shot_image" {
		return
	}
	if intent.Params == nil {
		intent.Params = map[string]string{}
	}
	if strings.TrimSpace(intent.Params["shot_number"]) != "" {
		return
	}
	if n, ok := inferShotNumberFromUserMessage(userMsg); ok {
		intent.Params["shot_number"] = strconv.Itoa(n)
	}
}

func inferShotNumberFromUserMessage(userMsg string) (int, bool) {
	userMsg = strings.TrimSpace(userMsg)
	if userMsg == "" {
		return 0, false
	}
	for _, re := range []*regexp.Regexp{reShotBeforeJing, reJingBeforeNum} {
		if m := re.FindStringSubmatch(userMsg); len(m) >= 2 {
			if n, ok := parseShotNumberToken(m[1]); ok && n > 0 {
				return n, true
			}
		}
	}
	return 0, false
}

func parseShotNumberToken(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil {
		return n, n > 0
	}
	n, ok := parseChineseShotNumber(s)
	return n, ok && n > 0
}

func parseActionFromReply(text string) (reply string, intent *ChatActionIntent) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	actionIdx := -1
	var parsed *ChatActionIntent

	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		line = strings.Trim(line, "`")
		if act, params, ok := parseActionLine(line); ok {
			parsed = &ChatActionIntent{Type: act, Params: params}
			actionIdx = i
			break
		}
	}
	if actionIdx == -1 || parsed == nil {
		return strings.TrimSpace(text), nil
	}

	if parsed.Params == nil {
		parsed.Params = map[string]string{}
	}
	for i := actionIdx - 1; i >= 0 && i >= actionIdx-3; i-- {
		line := strings.TrimSpace(lines[i])
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "SHOT:") || strings.HasPrefix(upper, "SHOT：") {
			if sep := strings.IndexAny(line, ":："); sep >= 0 {
				parsed.Params["shot_number"] = strings.TrimSpace(line[sep+1:])
			}
		}
	}

	lines = append(lines[:actionIdx], lines[actionIdx+1:]...)
	reply = strings.TrimSpace(strings.Join(lines, "\n"))

	if !parsed.Allowed() {
		return reply, nil
	}
	return reply, parsed
}

func parseActionLine(line string) (action string, params map[string]string, ok bool) {
	line = strings.TrimSpace(line)
	if m := reActionLine.FindStringSubmatch(line); len(m) == 2 {
		return splitActionToken(strings.ToLower(strings.TrimSpace(m[1])))
	}
	upper := strings.ToUpper(line)
	if strings.HasPrefix(upper, "ACTION:") {
		return splitActionToken(strings.ToLower(strings.TrimSpace(line[7:])))
	}
	if strings.HasPrefix(upper, "ACTION：") {
		return splitActionToken(strings.ToLower(strings.TrimSpace(line[len("ACTION："):])))
	}
	return "", nil, false
}

func splitActionToken(token string) (string, map[string]string, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", nil, false
	}
	params := map[string]string{}
	if i := strings.LastIndex(token, ":"); i > 0 {
		base := token[:i]
		if base == "generate_shot_image" {
			params["shot_number"] = token[i+1:]
			return base, params, true
		}
	}
	return token, params, true
}

func parseChineseShotNumber(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n, true
	}
	digits := map[rune]int{
		'零': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4,
		'五': 5, '六': 6, '七': 7, '八': 8, '九': 9,
	}
	runes := []rune(s)
	if len(runes) == 1 {
		if n, ok := digits[runes[0]]; ok {
			return n, true
		}
	}
	if strings.Contains(s, "十") {
		if len(runes) == 1 && runes[0] == '十' {
			return 10, true
		}
		if runes[0] == '十' && len(runes) == 2 {
			if n, ok := digits[runes[1]]; ok {
				return 10 + n, true
			}
		}
		if len(runes) == 2 && runes[1] == '十' {
			if n, ok := digits[runes[0]]; ok {
				return n * 10, true
			}
		}
		if len(runes) == 3 && runes[1] == '十' {
			tens, okT := digits[runes[0]]
			ones, okO := digits[runes[2]]
			if okT && okO {
				return tens*10 + ones, true
			}
		}
	}
	return 0, false
}

// ShouldBlockChatAction reports whether the user's message is consultative or requests
// an unsupported operation — AI must not trigger workflow in these cases.
func ShouldBlockChatAction(userMsg string) bool {
	msg := strings.TrimSpace(userMsg)
	if msg == "" {
		return false
	}
	if isUnsupportedChatOperation(msg) {
		return true
	}
	return isConsultativeChatMessage(msg)
}

// IsExplicitExecutionRequest reports whether the user clearly asks to run a workflow step now.
// Chat must not execute actions unless this returns true (buttons use WS directly).
func IsExplicitExecutionRequest(userMsg string) bool {
	msg := strings.TrimSpace(userMsg)
	if msg == "" {
		return false
	}
	if isUnsupportedChatOperation(msg) {
		return false
	}
	if isConsultativeChatMessage(msg) {
		return false
	}
	strong := []string{
		"帮我生成", "帮我做", "请生成", "请做", "现在生成", "现在开始", "开始生成", "立即生成",
		"重新生成", "生成一下", "执行", "事件分析", "分析事件", "自动分集", "AI分集", "AI 分集",
		"提取资产", "生成分镜", "现在分镜", "开始分镜", "生成图片", "生图", "出图", "生成视频", "从剧本提取",
	}
	for _, k := range strong {
		if strings.Contains(msg, k) {
			return true
		}
	}
	return containsChatExecutionVerb(msg)
}

func isUnsupportedChatOperation(userMsg string) bool {
	unsupported := []string{"删除", "移除", "改掉", "修改", "重命名", "导出", "下载", "清空"}
	for _, k := range unsupported {
		if strings.Contains(userMsg, k) {
			return true
		}
	}
	return false
}

func isConsultativeChatMessage(userMsg string) bool {
	consultative := []string{
		"有什么看法", "怎么看", "如何看", "你觉得", "你认为", "评价一下", "评价下",
		"好不好", "怎么样", "是什么", "为什么", "什么意思", "能不能", "可以吗",
		"要不要", "有什么建议", "给点建议", "建议吗", "讨论一下", "聊聊", "说说看",
		"有什么看法", "有何看法", "有何评价", "有什么想法", "有什么意见",
	}
	for _, k := range consultative {
		if strings.Contains(userMsg, k) {
			return true
		}
	}
	if strings.HasSuffix(userMsg, "吗") || strings.HasSuffix(userMsg, "吗？") || strings.HasSuffix(userMsg, "？") {
		return !containsChatExecutionVerb(userMsg)
	}
	return false
}

func containsChatExecutionVerb(userMsg string) bool {
	execVerbs := []string{
		"生成", "重新生成", "提取资产", "事件分析", "分析事件", "分集", "出图", "生图", "执行",
	}
	for _, v := range execVerbs {
		if strings.Contains(userMsg, v) {
			return true
		}
	}
	return false
}

// ChatActionRulesText returns the rule block injected into the system prompt.
func ChatActionRulesText() string {
	return `【流程执行白名单 — 不在名单内一律纯聊天】

仅以下 8 个动作可被系统执行（必须拼写完全一致）：
- analyze_events — 分析已导入原文事件
- split_episodes — AI 自动分集
- generate_skeleton — 生成故事骨架
- generate_strategy — 生成改编策略
- generate_script — 生成剧本
- generate_storyboard — 从剧本生成分镜
- extract_assets — 从剧本提取资产
- generate_shot_image — 为指定镜号生成图片（SHOT:镜号 或 ACTION:generate_shot_image:镜号）

【何时可以输出 ACTION（须全部满足）】
1. 用户在本轮消息里用祈使、明确的执行意图要求立刻做某一步（如「帮我生成改编策略」「现在分镜」「提取资产」）
2. 用户只是在了解、比较、讨论、提问（含「是什么」「能不能」「应该先做哪个」「有什么看法」）→ 只回答，绝不输出 ACTION
3. 用户仅提到步骤名称但未要求现在执行（如「改编策略很重要」）→ 不输出 ACTION
4. 用户要求删除/修改/导出等不在白名单内的操作 → 说明无法通过聊天执行，禁止 ACTION
5. 已有内容不是拒绝理由；用户明确要求重做时才输出对应 ACTION

【何时禁止输出 ACTION（默认）】
- 任何疑问句、咨询句、评价句、闲聊
- 用户未用明确执行意图（帮我/请/现在/开始/重新生成 + 动作）要求执行
- 想执行的动作不在白名单内
- 硬性条件不满足：需选集但未选集；无原文却 analyze/split

【输出格式（执行时）】
聊天区只写 1～2 句确认语（如「好的，正在生成故事骨架」），最后一行单独输出：
ACTION:动作名
生图时任选其一：上一行 SHOT:镜号（如 SHOT:2），或 ACTION:generate_shot_image:镜号
正文（骨架/策略/剧本/分镜/资产）一律由系统在右侧面板生成展示，聊天里不要写正文

【底线】
- 默认纯聊天；只有用户明确要执行白名单动作时才输出 ACTION
- 禁止输出白名单外的 ACTION
- 禁止猜测、擅自替用户决定下一步
- 禁止在聊天里输出骨架/策略/剧本/分镜表/资产列表等长正文`
}
