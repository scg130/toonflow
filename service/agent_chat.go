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
	Reply   string      `json:"reply"`
	Action  *ChatAction `json:"action,omitempty"`
	Work    interface{} `json:"work,omitempty"`
}

// AgentChat orchestrates conversational workflow for a project.
type AgentChat struct {
	DB       *sql.DB
	Vendor   adapter.Vendor
	SkillMgr *skill.Manager
}

// HandleMessage processes user chat and may auto-execute workflow steps.
func (a *AgentChat) HandleMessage(ctx context.Context, projectID, episodeID, stage, userMsg string) (*ChatResponse, error) {
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
	})
	if err != nil {
		ReportProgress(ctx, "chat", 0, "AI 请求失败")
		return nil, err
	}

	ReportProgress(ctx, "chat", 25, "AI 回复完成")

	reply, actionType := parseActionFromReply(resp.Content)
	out := &ChatResponse{Reply: reply}

	if actionType != "" {
		ReportProgress(ctx, actionType, 30, "开始执行: "+actionLabel(actionType))
		action, work, err := a.executeAction(ctx, projectID, episodeID, stage, actionType, userMsg)
		if err != nil {
			ReportProgress(ctx, actionType, 0, "执行失败: "+err.Error())
			out.Action = &ChatAction{Type: actionType, Error: err.Error()}
			out.Reply += "\n\n⚠️ 执行失败: " + err.Error()
		} else {
			if actionType == "generate_storyboard" {
				work = a.refineStoryboardWork(ctx, projectID, episodeID, resp.Content, work)
			}
			ReportProgress(ctx, actionType, 100, "执行完成: "+actionLabel(actionType))
			out.Action = action
			out.Work = work
		}
	} else {
		ReportProgress(ctx, "chat", 100, "完成")
	}

	if out.Work == nil && episodeID != "" && LooksLikeStoryboardTable(reply) {
		if items, _ := parseStoryboardResponse(reply); StoryboardScore(items) > 1 {
			items = NormalizeStoryboardItems(items)
			a.persistStoryboard(projectID, episodeID, items)
			out.Work = items
			logger.CtxInfo(ctx, "storyboard auto-saved from chat reply shots=%d", len(items))
		}
	}

	a.saveMessage(projectID, episodeID, "user", userMsg, "")
	a.saveMessage(projectID, episodeID, "assistant", out.Reply, actionType)

	return out, nil
}

func actionLabel(actionType string) string {
	labels := map[string]string{
		"analyze_events":      "事件分析",
		"split_episodes":      "AI 分集",
		"generate_skeleton":   "生成故事骨架",
		"generate_strategy":   "生成改编策略",
		"generate_script":     "生成剧本",
		"generate_storyboard": "生成分镜",
		"extract_assets":      "提取资产",
	}
	if l, ok := labels[actionType]; ok {
		return l
	}
	return actionType
}

func (a *AgentChat) buildSystemPrompt(stage, projectCtx string) string {
	return fmt.Sprintf(`你是 ToonFlow 短剧创作助手，帮助用户完成「原文导入 → 分集 → 剧本 → 分镜 → 出片」全流程。
当前阶段: %s

%s

可用自动执行动作（在回复末尾单独一行输出 ACTION:动作名）：
- ACTION:analyze_events — 分析已导入原文，提取章节事件
- ACTION:split_episodes — 根据原文和事件 AI 自动分集并设置每集参数
- ACTION:generate_skeleton — 为当前集生成故事骨架
- ACTION:generate_strategy — 为当前集生成改编策略
- ACTION:generate_script — 为当前集生成完整剧本
- ACTION:generate_storyboard — 将当前集剧本解析为分镜
- ACTION:extract_assets — 从剧本提取角色/场景/道具资产

规则：
1. 用中文回复，简洁专业，说明下一步建议
2. 当用户明确要求执行某步骤，或上下文已满足条件时，在回复最后一行输出 ACTION:xxx
3. 分集前需先导入原文；生成剧本前需先分集；生成分镜前需先有剧本
4. 不要输出虚假结果，执行动作由系统自动完成`, stage, projectCtx)
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
			fmt.Fprintf(&b, "已有%s\n", wt)
		}
	}
	return b.String()
}

func parseActionFromReply(text string) (reply, action string) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(strings.ToUpper(line), "ACTION:") {
			action = strings.TrimSpace(line[7:])
			lines = append(lines[:i], lines[i+1:]...)
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), strings.ToLower(strings.TrimSpace(action))
}

func (a *AgentChat) executeAction(ctx context.Context, projectID, episodeID, stage, actionType, userMsg string) (*ChatAction, interface{}, error) {
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
		n, err := ExtractAssetsFromEpisode(ctx, a.DB, a.Vendor, projectID, episodeID)
		return &ChatAction{Type: actionType, EpisodeID: episodeID, Result: map[string]interface{}{"assets": n}}, nil, err
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
			{Role: "system", Content: a.buildSystemPrompt("planning", a.buildProjectContext(projectID, episodeID, "planning"))},
			{Role: "user", Content: prompt},
		},
		MaxTokens: 8000,
	})
	if err != nil {
		return "", err
	}
	content := strings.TrimSpace(resp.Content)
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
	content := strings.TrimSpace(resp.Content)
	a.saveAgentWork(projectID, episodeID, "script", content)
	_, _ = a.DB.Exec("UPDATE o_episode SET script_content = ?, status = 'script_ready', updated_at = CURRENT_TIMESTAMP WHERE id = ?", content, episodeID)
	return content, nil
}

func (a *AgentChat) generateStoryboard(ctx context.Context, projectID, episodeID string) ([]task.StoryboardItem, error) {
	if fromChat := a.storyboardFromRecentChat(projectID, episodeID, 10); StoryboardScore(fromChat) > 1 {
		fromChat = NormalizeStoryboardItems(fromChat)
		a.persistStoryboard(projectID, episodeID, fromChat)
		logger.CtxInfo(ctx, "storyboard loaded from chat history shots=%d", len(fromChat))
		return fromChat, nil
	}

	script := a.loadWork(projectID, episodeID, "script")
	if script == "" {
		var s string
		_ = a.DB.QueryRow("SELECT script_content FROM o_episode WHERE id = ?", episodeID).Scan(&s)
		script = s
	}
	if script == "" {
		return nil, fmt.Errorf("请先生成剧本")
	}

	var artStyle string
	_ = a.DB.QueryRow("SELECT art_style FROM o_project WHERE id = ?", projectID).Scan(&artStyle)

	items, err := ParseScript(ctx, script, artStyle, a.SkillMgr, a.Vendor)
	if err != nil {
		return nil, err
	}
	items = NormalizeStoryboardItems(items)
	a.persistStoryboard(projectID, episodeID, items)
	return items, nil
}

func (a *AgentChat) refineStoryboardWork(ctx context.Context, projectID, episodeID, aiContent string, work interface{}) interface{} {
	items, _ := work.([]task.StoryboardItem)
	fromAI, _ := parseStoryboardResponse(strings.TrimSpace(aiContent))
	fromChat := a.storyboardFromRecentChat(projectID, episodeID, 10)

	best := PickBestStoryboard(items, fromAI, fromChat)
	if StoryboardScore(best) > StoryboardScore(items) {
		best = NormalizeStoryboardItems(best)
		logger.CtxInfo(ctx, "storyboard refined shots=%d", len(best))
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
