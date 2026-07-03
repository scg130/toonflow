package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"toonflow/adapter"
	"toonflow/logger"
	"toonflow/skill"
	"toonflow/task"
)

// EpisodeParams holds per-episode generation settings.
type EpisodeParams struct {
	TargetDurationMin float64 `json:"target_duration_minutes"`
	VideoRatio        string  `json:"video_ratio"`
	ArtStyle          string  `json:"art_style"`
	TargetWords       int     `json:"target_words"`
}

// ChatAction describes an auto-executed workflow step.
type ChatAction struct {
	Type      string                 `json:"type"`
	EpisodeID string                 `json:"episode_id,omitempty"`
	Result    map[string]interface{} `json:"result,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

// ChatResponse is returned from the AI chat endpoint.
type ChatResponse struct {
	Reply  string      `json:"reply"`
	Action *ChatAction `json:"action,omitempty"`
	Work   interface{} `json:"work,omitempty"`
}

// AgentChat orchestrates conversational workflow for a project.
type AgentChat struct {
	DB       *sql.DB
	Vendor   adapter.Vendor
	SkillMgr *skill.Manager
}

// HandleMessage processes user chat. Workflow runs only when AI outputs a whitelisted ACTION.
func (a *AgentChat) HandleMessage(ctx context.Context, userID, projectID, episodeID, stage, userMsg string) (*ChatResponse, error) {
	if a.Vendor == nil {
		return nil, fmt.Errorf("AI vendor not configured")
	}

	ReportProgress(ctx, "chat", 5, "AI 思考中...")

	history, _ := a.loadRecentMessages(projectID, episodeID, 20)
	projectCtx := a.buildProjectContext(projectID, episodeID, stage)
	systemPrompt := a.buildSystemPrompt(stage, projectCtx)
	messages := []adapter.TextMessage{{Role: "system", Content: systemPrompt}}
	for _, m := range history {
		messages = append(messages, adapter.TextMessage{Role: m.Role, Content: m.Content})
	}
	messages = append(messages, adapter.TextMessage{Role: "user", Content: userMsg})

	resp, err := a.Vendor.TextRequest(ctx, adapter.DefaultTextModel, adapter.TextParams{
		Messages:    messages,
		Temperature: 0.7,
		MaxTokens:   8000,
		OnDelta:     TextStreamDelta(ctx),
	})
	if err != nil {
		ReportProgress(ctx, "chat", 0, "AI 请求失败")
		return nil, err
	}
	ReportStreamEnd(ctx)
	ReportProgress(ctx, "chat", 25, "AI 回复完成")

	reply, intent := parseActionFromReply(resp.Content)
	if intent != nil && (ShouldBlockChatAction(userMsg) || !IsExplicitExecutionRequest(userMsg)) {
		intent = nil
		if strings.TrimSpace(reply) == "" {
			reply = SanitizeWorkContent(resp.Content)
		}
	}
	if intent != nil {
		EnrichIntentFromUserMessage(intent, userMsg)
		if err := intent.Validate(episodeID); err != nil {
			out := &ChatResponse{Reply: strings.TrimSpace(reply + "\n\n⚠️ " + UserMessageFromContext(ctx, err))}
			a.saveMessage(projectID, episodeID, "user", userMsg, "")
			a.saveMessage(projectID, episodeID, "assistant", out.Reply, "")
			ReportProgress(ctx, "chat", 100, "完成")
			return out, nil
		}
		return a.handleWorkflowAction(ctx, userID, projectID, episodeID, stage, userMsg, intent, reply, resp.Content)
	}

	out := &ChatResponse{Reply: reply}
	ReportProgress(ctx, "chat", 100, "完成")
	a.saveMessage(projectID, episodeID, "user", userMsg, "")
	a.saveMessage(projectID, episodeID, "assistant", out.Reply, "")
	return out, nil
}

// RunAction executes a whitelisted workflow action directly (UI buttons), without AI.
func (a *AgentChat) RunAction(ctx context.Context, userID, projectID, episodeID, stage string, intent *ChatActionIntent) (*ChatResponse, error) {
	if intent == nil || !intent.Allowed() {
		return nil, fmt.Errorf("不允许的执行动作")
	}
	if err := intent.Validate(episodeID); err != nil {
		return nil, err
	}
	return a.handleWorkflowAction(ctx, userID, projectID, episodeID, stage, actionLabel(intent.Type), intent, "", "")
}

func (a *AgentChat) handleWorkflowAction(ctx context.Context, userID, projectID, episodeID, stage, userMsg string, intent *ChatActionIntent, chatReply, aiContent string) (*ChatResponse, error) {
	actionType := intent.Type
	out := &ChatResponse{Reply: actionPendingReply(actionType)}
	ReportProgress(ctx, actionType, 30, "开始执行: "+actionLabel(actionType))

	var action *ChatAction
	var work interface{}
	var err error

	if workType, ok := PlanningActionWorkType(actionType); ok && IsSubstantialWorkContent(chatReply) {
		ReportProgress(ctx, actionType, 60, "正在保存"+actionLabel(actionType)+"...")
		content := SanitizeWorkContent(chatReply)
		a.persistPlanningWork(projectID, episodeID, workType, content)
		action = &ChatAction{Type: actionType, EpisodeID: episodeID}
		work = content
	} else {
		action, work, err = a.executeAction(ctx, userID, projectID, episodeID, stage, intent, userMsg)
	}

	out.Reply = actionChatReply(ctx, actionType, err)
	if err != nil {
		userErr := UserMessageFromContext(ctx, err)
		ReportProgress(ctx, actionType, 0, "执行失败: "+userErr)
		out.Action = &ChatAction{Type: actionType, Error: userErr}
	} else {
		if actionType == "generate_storyboard" && aiContent != "" {
			work = a.refineStoryboardWork(ctx, projectID, episodeID, aiContent, work)
		}
		ReportProgress(ctx, actionType, 100, "执行完成: "+actionLabel(actionType))
		out.Action = action
		out.Work = work
	}

	a.saveMessage(projectID, episodeID, "user", userMsg, "")
	a.saveMessage(projectID, episodeID, "assistant", out.Reply, actionType)
	return out, nil
}

func actionLabel(actionType string) string {
	labels := map[string]string{
		"analyze_events":       "事件分析",
		"split_episodes":       "AI 分集",
		"generate_skeleton":    "生成故事骨架",
		"generate_strategy":    "生成改编策略",
		"generate_script":      "生成剧本",
		"generate_storyboard":  "生成分镜",
		"extract_assets":       "提取资产",
		"generate_shot_image":  "生成分镜图片",
	}
	if l, ok := labels[actionType]; ok {
		return l
	}
	return actionType
}

func actionPendingReply(actionType string) string {
	switch actionType {
	case "generate_skeleton":
		return "好的，正在生成故事骨架…"
	case "generate_strategy":
		return "好的，正在生成改编策略…"
	case "generate_script":
		return "好的，正在生成剧本…"
	case "generate_storyboard":
		return "好的，正在生成分镜…"
	case "extract_assets":
		return "好的，正在从剧本提取资产…"
	case "generate_shot_image":
		return "好的，正在生成分镜图片…"
	default:
		return "好的，正在执行「" + actionLabel(actionType) + "」…"
	}
}

func actionChatReply(ctx context.Context, actionType string, err error) string {
	if err != nil {
		return fmt.Sprintf("⚠️ %s失败：%s", actionLabel(actionType), UserMessageFromContext(ctx, err))
	}
	switch actionType {
	case "generate_skeleton":
		return "✅ 故事骨架已生成，请在右侧「故事骨架」标签查看。"
	case "generate_strategy":
		return "✅ 改编策略已生成，请在右侧「改编策略」标签查看。"
	case "generate_script":
		return "✅ 剧本已生成，请在右侧「剧本」标签查看。"
	case "generate_storyboard":
		return "✅ 分镜已生成，请在「分镜」面板查看。"
	case "extract_assets":
		return "✅ 资产已提取，请在「资产」面板查看。"
	case "generate_shot_image":
		return "✅ 分镜图片任务已提交，请在分镜面板查看进度。"
	case "analyze_events":
		return "✅ 事件分析完成，请在「原文」面板查看各章事件。"
	case "split_episodes":
		return "✅ 分集完成，请在「剧本列表」查看各集。"
	default:
		return "✅ " + actionLabel(actionType) + "已完成。"
	}
}

func (a *AgentChat) buildSystemPrompt(stage, projectCtx string) string {
	return fmt.Sprintf(`你是 ToonFlow 短剧创作助手，帮助用户完成「原文导入 → 分集 → 剧本 → 分镜 → 出片」全流程。
当前阶段: %s

%s

%s`, stage, projectCtx, ChatActionRulesText())
}

func (a *AgentChat) buildWorkGenerationSystemPrompt(projectID, episodeID string) string {
	return fmt.Sprintf(`你是专业的短剧策划编剧。根据项目资料生成高质量 Markdown 正文。

项目背景:
%s

要求：
- 只输出正文（骨架/策略/剧本内容），使用 Markdown 格式
- 禁止输出 ACTION、SHOT 等控制指令
- 禁止输出「请在右侧面板查看」等元提示`, a.buildProjectContext(projectID, episodeID, "planning"))
}

func (a *AgentChat) buildProjectContext(projectID, episodeID, stage string) string {
	var name, artStyle, ratio string
	_ = a.DB.QueryRow("SELECT name, art_style, video_ratio FROM o_project WHERE id = ?", projectID).Scan(&name, &artStyle, &ratio)

	var sourceCount, episodeCount int
	_ = a.DB.QueryRow("SELECT COUNT(*) FROM o_source_text WHERE project_id = ?", projectID).Scan(&sourceCount)
	_ = a.DB.QueryRow("SELECT COUNT(*) FROM o_episode WHERE project_id = ?", projectID).Scan(&episodeCount)

	b := strings.Builder{}
	fmt.Fprintf(&b, "项目: %s\n画风: %s\n比例: %s\n已导入原文: %d 章\n已分集: %d 集\n", name, artStyle, ratio, sourceCount, episodeCount)

	if episodeID != "" {
		var title, status, script string
		_ = a.DB.QueryRow("SELECT title, status, script_content FROM o_episode WHERE id = ?", episodeID).Scan(&title, &status, &script)
		fmt.Fprintf(&b, "当前集: %s (%s)\n剧本长度: %d 字\n", title, status, len([]rune(script)))
	}

	for _, wt := range []string{"skeleton", "strategy", "script"} {
		var content string
		err := a.DB.QueryRow(
			"SELECT content FROM o_agent_work WHERE project_id = ? AND episode_id = ? AND work_type = ?",
			projectID, episodeID, wt,
		).Scan(&content)
		if err == nil && content != "" {
			fmt.Fprintf(&b, "已保存%s（用户可随时重新生成并覆盖）\n", workTypeLabel(wt))
		}
	}
	return b.String()
}

func workTypeLabel(workType string) string {
	switch workType {
	case "skeleton":
		return "故事骨架"
	case "strategy":
		return "改编策略"
	case "script":
		return "剧本"
	default:
		return workType
	}
}

// GenerateWork runs a planning work generation step directly (skeleton/strategy/script).
func (a *AgentChat) GenerateWork(ctx context.Context, projectID, episodeID, workType string) (string, error) {
	switch workType {
	case "skeleton":
		return a.generateWork(ctx, projectID, episodeID, "skeleton", skeletonPrompt())
	case "strategy":
		return a.generateWork(ctx, projectID, episodeID, "strategy", strategyPrompt())
	case "script":
		return a.generateScript(ctx, projectID, episodeID)
	default:
		return "", fmt.Errorf("unsupported work type: %s", workType)
	}
}

func (a *AgentChat) executeAction(ctx context.Context, userID, projectID, episodeID, stage string, intent *ChatActionIntent, userMsg string) (*ChatAction, interface{}, error) {
	actionType := intent.Type
	switch actionType {
	case "analyze_events":
		n, err := AnalyzeSourceEvents(ctx, a.DB, a.Vendor, projectID)
		return &ChatAction{Type: actionType, Result: map[string]interface{}{"analyzed": n}}, nil, err
	case "split_episodes":
		eps, err := SplitEpisodes(ctx, a.DB, a.Vendor, a.SkillMgr, projectID)
		return &ChatAction{Type: actionType, Result: map[string]interface{}{"episodes": len(eps)}}, eps, err
	case "generate_skeleton":
		ReportProgress(ctx, "generate_skeleton", 40, "正在生成故事骨架...")
		content, err := a.generateWork(ctx, projectID, episodeID, "skeleton", skeletonPrompt())
		return &ChatAction{Type: actionType, EpisodeID: episodeID}, content, err
	case "generate_strategy":
		ReportProgress(ctx, "generate_strategy", 40, "正在生成改编策略...")
		content, err := a.generateWork(ctx, projectID, episodeID, "strategy", strategyPrompt())
		return &ChatAction{Type: actionType, EpisodeID: episodeID}, content, err
	case "generate_script":
		ReportProgress(ctx, "generate_script", 40, "正在生成剧本...")
		content, err := a.generateScript(ctx, projectID, episodeID)
		return &ChatAction{Type: actionType, EpisodeID: episodeID}, content, err
	case "generate_storyboard":
		ReportProgress(ctx, "generate_storyboard", 40, "正在生成分镜...")
		items, err := a.generateStoryboard(ctx, projectID, episodeID)
		return &ChatAction{Type: actionType, EpisodeID: episodeID, Result: map[string]interface{}{"shots": len(items)}}, items, err
	case "extract_assets":
		ReportProgress(ctx, "extract_assets", 40, "正在提取资产...")
		n, err := ExtractAssetsFromEpisode(ctx, a.DB, a.Vendor, userID, projectID, episodeID)
		return &ChatAction{Type: actionType, EpisodeID: episodeID, Result: map[string]interface{}{"assets": n}}, nil, err
	case "generate_shot_image":
		shot, err := intent.ShotNumber()
		if err != nil {
			return nil, nil, err
		}
		return &ChatAction{
			Type:      actionType,
			EpisodeID: episodeID,
			Result:    map[string]interface{}{"shot_number": shot},
		}, nil, nil
	default:
		return nil, nil, fmt.Errorf("unknown action: %s", actionType)
	}
}

func skeletonPrompt() string {
	return "请根据项目原文和当前集事件，生成「故事骨架」：包含核心冲突、人物弧线、三幕结构要点。使用 Markdown 格式。"
}

func strategyPrompt() string {
	return "请根据故事骨架，生成「改编策略」：说明如何从小说改编为短剧、节奏压缩方案、视觉风格建议。使用 Markdown 格式。"
}

func (a *AgentChat) generateWork(ctx context.Context, projectID, episodeID, workType, instruction string) (string, error) {
	if episodeID == "" {
		return "", fmt.Errorf("请先选择一集")
	}
	contextText := a.episodeContext(projectID, episodeID)
	prompt := instruction + "\n\n" + contextText

	resp, err := a.Vendor.TextRequest(ctx, adapter.DefaultTextModel, adapter.TextParams{
		Messages: []adapter.TextMessage{
			{Role: "system", Content: a.buildWorkGenerationSystemPrompt(projectID, episodeID)},
			{Role: "user", Content: prompt},
		},
		MaxTokens: 8000,
	})
	if err != nil {
		return "", err
	}
	content := SanitizeWorkContent(strings.TrimSpace(resp.Content))
	a.saveAgentWork(projectID, episodeID, workType, content)
	return content, nil
}

func (a *AgentChat) generateScript(ctx context.Context, projectID, episodeID string) (string, error) {
	if episodeID == "" {
		return "", fmt.Errorf("请先选择一集")
	}

	var paramsJSON string
	var title string
	_ = a.DB.QueryRow("SELECT title, params_json FROM o_episode WHERE id = ?", episodeID).Scan(&title, &paramsJSON)
	var params EpisodeParams
	_ = json.Unmarshal([]byte(paramsJSON), &params)

	skeleton := a.loadWork(projectID, episodeID, "skeleton")
	strategy := a.loadWork(projectID, episodeID, "strategy")
	contextText := a.episodeContext(projectID, episodeID)

	prompt := fmt.Sprintf(`为「%s」生成完整短剧剧本。
目标时长: %.0f 分钟，目标字数约 %d 字，画面比例 %s，画风 %s。

%s

故事骨架:
%s

改编策略:
%s

输出格式要求：Markdown，含场次、场景描述、对白、镜头提示。`, title, params.TargetDurationMin, params.TargetWords, params.VideoRatio, params.ArtStyle, contextText, skeleton, strategy)

	resp, err := a.Vendor.TextRequest(ctx, adapter.DefaultTextModel, adapter.TextParams{
		Messages: []adapter.TextMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens: 12000,
	})
	if err != nil {
		return "", err
	}
	content := SanitizeWorkContent(strings.TrimSpace(resp.Content))
	a.saveAgentWork(projectID, episodeID, "script", content)
	_, _ = a.DB.Exec("UPDATE o_episode SET script_content = ?, status = 'script_ready', updated_at = CURRENT_TIMESTAMP WHERE id = ?", content, episodeID)
	return content, nil
}

func (a *AgentChat) generateStoryboard(ctx context.Context, projectID, episodeID string) ([]task.StoryboardItem, error) {
	script := a.loadWork(projectID, episodeID, "script")
	if script == "" {
		var s string
		_ = a.DB.QueryRow("SELECT script_content FROM o_episode WHERE id = ?", episodeID).Scan(&s)
		script = s
	}
	if script == "" {
		return nil, fmt.Errorf("请先生成剧本")
	}

	minShots := MinShotsForScript(script)
	logger.CtxTrace(ctx, "storyboard parse from script min_shots=%d script_len=%d", minShots, len([]rune(script)))

	var artStyle, videoRatio string
	_ = a.DB.QueryRow("SELECT art_style, video_ratio FROM o_project WHERE id = ?", projectID).Scan(&artStyle, &videoRatio)

	assets, _ := LoadProjectAssets(a.DB, projectID)
	items, err := ParseScript(ctx, script, artStyle, assets, a.SkillMgr, a.Vendor)
	if err != nil {
		return nil, err
	}
	items = applyStoryboardStyleAnchors(items, videoRatio, artStyle)
	a.persistStoryboard(projectID, episodeID, items)
	logger.CtxTrace(ctx, "storyboard parsed from script shots=%d", len(items))
	return items, nil
}

func (a *AgentChat) refineStoryboardWork(ctx context.Context, projectID, episodeID, aiContent string, work interface{}) interface{} {
	items, _ := work.([]task.StoryboardItem)
	script := a.loadWork(projectID, episodeID, "script")
	if script == "" {
		var s string
		_ = a.DB.QueryRow("SELECT script_content FROM o_episode WHERE id = ?", episodeID).Scan(&s)
		script = s
	}
	minShots := MinShotsForScript(script)

	fromAI, _ := parseStoryboardResponse(strings.TrimSpace(aiContent))
	best := PickBestStoryboard(items, fromAI)
	if IsAdequateStoryboard(best, minShots) && StoryboardScore(best) > StoryboardScore(items) {
		best = NormalizeStoryboardItems(best)
		logger.CtxTrace(ctx, "storyboard refined shots=%d", len(best))
		a.persistStoryboard(projectID, episodeID, best)
		return best
	}
	return items
}

func (a *AgentChat) storyboardFromRecentChat(projectID, episodeID string, limit int) []task.StoryboardItem {
	return StoryboardFromRecentChat(a.DB, projectID, episodeID, limit)
}

// StoryboardFromRecentChat parses the best storyboard from recent assistant messages.
func StoryboardFromRecentChat(db *sql.DB, projectID, episodeID string, limit int) []task.StoryboardItem {
	rows, err := db.Query(`
		SELECT content FROM o_chat_message
		WHERE project_id = ? AND (episode_id = ? OR episode_id = '') AND role = 'assistant'
		ORDER BY created_at DESC LIMIT ?`, projectID, episodeID, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var best []task.StoryboardItem
	for rows.Next() {
		var content string
		if rows.Scan(&content) != nil {
			continue
		}
		parsed, _ := parseStoryboardResponse(content)
		if StoryboardScore(parsed) > StoryboardScore(best) {
			best = parsed
		}
	}
	return best
}

func (a *AgentChat) persistStoryboard(projectID, episodeID string, items []task.StoryboardItem) {
	if existing, err := LoadStoryboardItems(a.DB, projectID, episodeID); err == nil && len(existing) > 0 {
		items = MergeStoryboardMedia(existing, items)
	}
	shotsJSON, _ := json.Marshal(items)
	sbID := fmt.Sprintf("sb_%s_%s", projectID, episodeID)
	_, _ = a.DB.Exec(`
		INSERT INTO o_storyboard (id, project_id, scene_name, segment_num, shots, updated_at)
		VALUES (?, ?, ?, (SELECT episode_num FROM o_episode WHERE id = ?), ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET shots = excluded.shots, updated_at = CURRENT_TIMESTAMP
	`, sbID, projectID, "episode", episodeID, string(shotsJSON))
	_, _ = a.DB.Exec("UPDATE o_episode SET status = 'storyboard_ready', updated_at = CURRENT_TIMESTAMP WHERE id = ?", episodeID)
}

func (a *AgentChat) episodeContext(projectID, episodeID string) string {
	var title, eventsRef, paramsJSON string
	_ = a.DB.QueryRow("SELECT title, events_ref, params_json FROM o_episode WHERE id = ?", episodeID).Scan(&title, &eventsRef, &paramsJSON)

	rows, _ := a.DB.Query("SELECT chapter_name, content, events FROM o_source_text WHERE project_id = ? ORDER BY sort_num", projectID)
	defer func() {
		if rows != nil {
			rows.Close()
		}
	}()

	var b strings.Builder
	fmt.Fprintf(&b, "当前集: %s\n事件: %s\n参数: %s\n\n相关原文:\n", title, eventsRef, paramsJSON)
	if rows != nil {
		for rows.Next() {
			var ch, content, events string
			if rows.Scan(&ch, &content, &events) == nil {
				preview := content
				if len([]rune(preview)) > 500 {
					preview = string([]rune(preview)[:500]) + "..."
				}
				fmt.Fprintf(&b, "【%s】%s\n事件:%s\n\n", ch, preview, events)
			}
		}
	}
	return b.String()
}

func (a *AgentChat) loadWork(projectID, episodeID, workType string) string {
	var content string
	_ = a.DB.QueryRow(
		"SELECT content FROM o_agent_work WHERE project_id = ? AND episode_id = ? AND work_type = ?",
		projectID, episodeID, workType,
	).Scan(&content)
	return content
}

func (a *AgentChat) saveAgentWork(projectID, episodeID, workType, content string) {
	id := fmt.Sprintf("work_%s_%s_%s", projectID, episodeID, workType)
	_, _ = a.DB.Exec(`
		INSERT INTO o_agent_work (id, project_id, episode_id, work_type, content, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET content = excluded.content, updated_at = CURRENT_TIMESTAMP
	`, id, projectID, episodeID, workType, content)
}

func (a *AgentChat) persistPlanningWork(projectID, episodeID, workType, content string) {
	content = SanitizeWorkContent(content)
	a.saveAgentWork(projectID, episodeID, workType, content)
	if workType == "script" {
		_, _ = a.DB.Exec("UPDATE o_episode SET script_content = ?, status = 'script_ready', updated_at = CURRENT_TIMESTAMP WHERE id = ?", content, episodeID)
	}
}

type chatMsg struct {
	Role    string
	Content string
}

func (a *AgentChat) loadRecentMessages(projectID, episodeID string, limit int) ([]chatMsg, error) {
	rows, err := a.DB.Query(`
		SELECT role, content FROM o_chat_message
		WHERE project_id = ? AND (episode_id = ? OR episode_id = '')
		ORDER BY created_at DESC LIMIT ?`, projectID, episodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []chatMsg
	for rows.Next() {
		var m chatMsg
		if rows.Scan(&m.Role, &m.Content) == nil {
			msgs = append([]chatMsg{m}, msgs...)
		}
	}
	return msgs, nil
}

func (a *AgentChat) saveMessage(projectID, episodeID, role, content, action string) {
	id := fmt.Sprintf("msg_%d", time.Now().UnixNano())
	_, _ = a.DB.Exec(
		"INSERT INTO o_chat_message (id, project_id, episode_id, role, content, action_json) VALUES (?, ?, ?, ?, ?, ?)",
		id, projectID, episodeID, role, content, action,
	)
}
