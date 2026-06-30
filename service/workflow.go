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
)

// SourceText represents imported original text.
type SourceText struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	Volume      string `json:"volume"`
	ChapterName string `json:"chapter_name"`
	Content     string `json:"content"`
	Events      string `json:"events"`
	SortNum     int    `json:"sort_num"`
	CreatedAt   string `json:"created_at"`
}

// Episode represents one episode in a project.
type Episode struct {
	ID            string        `json:"id"`
	ProjectID     string        `json:"project_id"`
	EpisodeNum    int           `json:"episode_num"`
	Title         string        `json:"title"`
	Params        EpisodeParams `json:"params"`
	ScriptContent string        `json:"script_content"`
	EventsRef     string        `json:"events_ref"`
	Status        string        `json:"status"`
	CreatedAt     string        `json:"created_at"`
}

// AnalyzeSourceEvents uses AI to extract events from each source chapter.
func AnalyzeSourceEvents(ctx context.Context, db *sql.DB, v adapter.Vendor, projectID string) (int, error) {
	rows, err := db.Query("SELECT id, chapter_name, content FROM o_source_text WHERE project_id = ? ORDER BY sort_num", projectID)
	if err != nil {
		return 0, err
	}

	type chapter struct {
		id, name, content string
	}
	var chapters []chapter
	for rows.Next() {
		var ch chapter
		if err := rows.Scan(&ch.id, &ch.name, &ch.content); err != nil {
			continue
		}
		chapters = append(chapters, ch)
	}
	rows.Close() // 必须先关闭，SQLite 单连接下不能在 rows 打开时 Exec

	if len(chapters) == 0 {
		return 0, fmt.Errorf("请先导入原文")
	}

	count := 0
	total := len(chapters)
	for i, ch := range chapters {
		pct := 10 + float32(i)*80/float32(total)
		ReportProgress(ctx, "analyze_events", pct, fmt.Sprintf("分析章节 %d/%d: %s", i+1, total, ch.name))
		logger.CtxTrace(ctx, "analyze chapter start name=%s id=%s", ch.name, ch.id)
		preview := ch.content
		if len([]rune(preview)) > 6000 {
			preview = string([]rune(preview)[:6000])
		}

		chCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		resp, err := v.TextRequest(chCtx, adapter.DefaultTextModel, adapter.TextParams{
			Messages: []adapter.TextMessage{
				{Role: "system", Content: "你是文学分析助手。提取章节关键事件，输出简洁的事件列表（每行一个事件，格式：|第N事件| 描述）。"},
				{Role: "user", Content: fmt.Sprintf("章节「%s」:\n\n%s", ch.name, preview)},
			},
			MaxTokens: 2000,
		})
		cancel()
		if err != nil {
			logger.CtxError(ctx, err, "analyze chapter failed name=%s", ch.name)
			return count, fmt.Errorf("章节「%s」: %w", ch.name, err)
		}
		if _, err := db.Exec("UPDATE o_source_text SET events = ? WHERE id = ?", strings.TrimSpace(resp.Content), ch.id); err != nil {
			logger.CtxError(ctx, err, "analyze chapter save failed name=%s", ch.name)
			return count, err
		}
		logger.CtxTrace(ctx, "analyze chapter done name=%s", ch.name)
		count++
	}
	ReportProgress(ctx, "analyze_events", 100, fmt.Sprintf("事件分析完成，共 %d 章", count))
	return count, nil
}

// SplitEpisodes uses AI to divide source text into episodes with parameters.
func SplitEpisodes(ctx context.Context, db *sql.DB, v adapter.Vendor, _ *skill.Manager, projectID string) ([]Episode, error) {
	var artStyle, ratio string
	_ = db.QueryRow("SELECT art_style, video_ratio FROM o_project WHERE id = ?", projectID).Scan(&artStyle, &ratio)

	rows, err := db.Query("SELECT chapter_name, content, events FROM o_source_text WHERE project_id = ? ORDER BY sort_num", projectID)
	if err != nil {
		return nil, err
	}

	var corpus strings.Builder
	for rows.Next() {
		var ch, content, events string
		if rows.Scan(&ch, &content, &events) == nil {
			preview := content
			if len([]rune(preview)) > 3000 {
				preview = string([]rune(preview)[:3000]) + "..."
			}
			fmt.Fprintf(&corpus, "【%s】\n事件:%s\n内容:%s\n\n", ch, events, preview)
		}
	}
	rows.Close() // 释放连接后再调 AI / 写库

	if corpus.Len() == 0 {
		return nil, fmt.Errorf("请先导入原文")
	}

	ReportProgress(ctx, "split_episodes", 20, "AI 规划分集方案...")

	prompt := fmt.Sprintf(`根据以下小说原文和事件，规划短剧分集方案。
默认画风: %s，画面比例: %s，每集目标时长 2-3 分钟。

%s

请输出 JSON 数组，每项包含:
- episode_num (int)
- title (string) 如 "EP01: xxx"
- events_ref (string) 本集涵盖的事件摘要
- target_duration_minutes (float)
- target_words (int)
- video_ratio (string)
- art_style (string)

只输出 JSON 数组，不要其他文字。`, artStyle, ratio, corpus.String())

	resp, err := v.TextRequest(ctx, adapter.DefaultTextModel, adapter.TextParams{
		Messages: []adapter.TextMessage{{Role: "user", Content: prompt}},
		MaxTokens: 8000,
	})
	if err != nil {
		return nil, err
	}

	type epPlan struct {
		EpisodeNum            int     `json:"episode_num"`
		Title                 string  `json:"title"`
		EventsRef             string  `json:"events_ref"`
		TargetDurationMinutes float64 `json:"target_duration_minutes"`
		TargetWords           int     `json:"target_words"`
		VideoRatio            string  `json:"video_ratio"`
		ArtStyle              string  `json:"art_style"`
	}

	var plans []epPlan
	text := extractJSONArray(resp.Content)
	if err := json.Unmarshal([]byte(text), &plans); err != nil {
		return nil, fmt.Errorf("parse episodes: %w (raw: %s)", err, truncate(resp.Content, 200))
	}
	if len(plans) == 0 {
		return nil, fmt.Errorf("AI 未生成分集方案")
	}

	ReportProgress(ctx, "split_episodes", 70, fmt.Sprintf("写入 %d 集分集数据...", len(plans)))

	_, _ = db.Exec("DELETE FROM o_episode WHERE project_id = ?", projectID)

	var episodes []Episode
	for _, p := range plans {
		if p.TargetDurationMinutes <= 0 {
			p.TargetDurationMinutes = 3
		}
		if p.TargetWords <= 0 {
			p.TargetWords = 450
		}
		if p.VideoRatio == "" {
			p.VideoRatio = ratio
		}
		if p.ArtStyle == "" {
			p.ArtStyle = artStyle
		}

		params := EpisodeParams{
			TargetDurationMin: p.TargetDurationMinutes,
			VideoRatio:        p.VideoRatio,
			ArtStyle:          p.ArtStyle,
			TargetWords:       p.TargetWords,
		}
		paramsJSON, _ := json.Marshal(params)
		id := fmt.Sprintf("ep_%d_%s", time.Now().UnixNano(), projectID)
		_, err := db.Exec(`
			INSERT INTO o_episode (id, project_id, episode_num, title, params_json, events_ref, status)
			VALUES (?, ?, ?, ?, ?, ?, 'planned')`,
			id, projectID, p.EpisodeNum, p.Title, string(paramsJSON), p.EventsRef,
		)
		if err != nil {
			return episodes, err
		}
		episodes = append(episodes, Episode{
			ID:         id,
			ProjectID:  projectID,
			EpisodeNum: p.EpisodeNum,
			Title:      p.Title,
			Params:     params,
			EventsRef:  p.EventsRef,
			Status:     "planned",
		})
	}
	ReportProgress(ctx, "split_episodes", 100, fmt.Sprintf("分集完成，共 %d 集", len(episodes)))
	return episodes, nil
}

type extractAssetItem struct {
	Name            string
	Type            string
	Desc            string
	CharacterID     string
	FeatureKeywords []string
	TurnaroundViews []extractTurnaroundView
}

type extractTurnaroundView struct {
	View string
	Desc string
}

// ExtractAssetsFromEpisode extracts characters/scenes/props from episode script.
func ExtractAssetsFromEpisode(ctx context.Context, db *sql.DB, v adapter.Vendor, userID, projectID, episodeID string) (int, error) {
	var script string
	err := db.QueryRow("SELECT script_content FROM o_episode WHERE id = ?", episodeID).Scan(&script)
	if err != nil || script == "" {
		var work string
		_ = db.QueryRow("SELECT content FROM o_agent_work WHERE project_id = ? AND episode_id = ? AND work_type = 'script'", projectID, episodeID).Scan(&work)
		script = work
	}
	if script == "" {
		return 0, fmt.Errorf("请先生成剧本")
	}

	var videoRatio, artStyle string
	_ = db.QueryRow("SELECT video_ratio, art_style FROM o_project WHERE id = ?", projectID).Scan(&videoRatio, &artStyle)

	systemPrompt := buildAssetExtractSystemPrompt(videoRatio, artStyle)
	resp, err := v.TextRequest(ctx, adapter.DefaultTextModel, adapter.TextParams{
		Messages: []adapter.TextMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: script},
		},
		MaxTokens: 6000,
	})
	if err != nil {
		return 0, err
	}

	type turnaroundView struct {
		View string `json:"view"`
		Desc string `json:"desc"`
	}
	type assetItemJSON struct {
		Name            string           `json:"name"`
		Type            string           `json:"type"`
		Desc            string           `json:"desc"`
		CharacterID     string           `json:"character_id"`
		FeatureKeywords []string         `json:"feature_keywords"`
		TurnaroundViews []turnaroundView `json:"turnaround_views"`
	}
	var raw []assetItemJSON
	text := extractJSONArray(resp.Content)
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return 0, fmt.Errorf("parse assets: %w", err)
	}
	items := make([]extractAssetItem, 0, len(raw))
	for _, r := range raw {
		it := extractAssetItem{
			Name: r.Name, Type: r.Type, Desc: r.Desc,
			CharacterID: r.CharacterID, FeatureKeywords: r.FeatureKeywords,
		}
		for _, tv := range r.TurnaroundViews {
			it.TurnaroundViews = append(it.TurnaroundViews, extractTurnaroundView{View: tv.View, Desc: tv.Desc})
		}
		items = append(items, it)
	}

	count := 0
	for _, it := range items {
		if it.Name == "" {
			continue
		}
		if it.Type == "" {
			it.Type = "role"
		}
		desc := buildMainAssetDesc(it)
		parentID, inserted, err := upsertProjectAsset(db, projectID, userID, it.Name, desc, it.Type, 0, "")
		if err != nil {
			continue
		}
		if inserted {
			count++
		}
		if it.Type != "role" {
			continue
		}
		views := it.TurnaroundViews
		if len(views) == 0 {
			views = defaultTurnaroundViews(it)
		}
		for _, tv := range views {
			viewName := fmt.Sprintf("%s·%s", it.Name, tv.View)
			viewDesc := buildTurnaroundDesc(it, tv.View, tv.Desc)
			_, childInserted, err := upsertProjectAsset(db, projectID, userID, viewName, viewDesc, "role", int(parentID), tv.View)
			if err == nil && childInserted {
				count++
			}
		}
	}
	if userID != "" {
		_, _ = db.Exec(`UPDATE o_assets SET user_id = ? WHERE project_id = ? AND (user_id IS NULL OR user_id = '')`,
			userID, projectID)
	}
	if len(items) == 0 {
		return 0, fmt.Errorf("未能从剧本解析出资产")
	}
	return count, nil
}

func buildAssetExtractSystemPrompt(videoRatio, artStyle string) string {
	ratioNote := "16:9 widescreen"
	if videoRatio == "9:16" {
		ratioNote = "9:16 vertical"
	}
	return fmt.Sprintf(`你是短剧资产策划。从剧本提取角色/场景/道具，输出 JSON 数组（仅 JSON，无说明文字）。

每项字段：
- name (string) 资产名称
- type (string) role | scene | prop
- desc (string) 中文视觉描述（服装、配色、材质、氛围）
- character_id (string) 角色唯一 ID（仅 role，如 ShiHao）
- feature_keywords (string[]) 不可变特征关键词（发型、瞳色、服装、体型等，仅 role）
- turnaround_views (array, 仅主要 role) 多角度设定卡，每项含：
  - view: front | side | back | three_quarter
  - desc: 英文 turnaround sheet 描述（T-pose 参考、%s 构图、consistent lighting）

主要角色必须输出 4 个 turnaround_views。desc 中注明 character_id 与 style: consistent。
画风锚点: %s, Unreal Engine 5 render, Octane Render, high fidelity, consistent lighting, unified color palette`, ratioNote, artStyle)
}

func buildMainAssetDesc(it extractAssetItem) string {
	cid := it.CharacterID
	if cid == "" {
		cid = CharacterIDFromName(it.Name)
	}
	parts := []string{strings.TrimSpace(it.Desc)}
	parts = append(parts, fmt.Sprintf("character_id: %s", cid))
	if len(it.FeatureKeywords) > 0 {
		parts = append(parts, "feature_keywords: "+strings.Join(it.FeatureKeywords, ", "))
	}
	parts = append(parts, "style: consistent", "turnaround sheet reference required")
	return strings.Join(parts, " | ")
}

func defaultTurnaroundViews(it extractAssetItem) []extractTurnaroundView {
	cid := it.CharacterID
	if cid == "" {
		cid = CharacterIDFromName(it.Name)
	}
	kw := strings.Join(it.FeatureKeywords, ", ")
	base := strings.TrimSpace(it.Desc)
	return []extractTurnaroundView{
		{"front", fmt.Sprintf("%s turnaround sheet front view T-pose, character_id: %s, style: consistent, %s, consistent lighting", base, cid, kw)},
		{"side", fmt.Sprintf("%s turnaround sheet side profile, character_id: %s, style: consistent, %s", base, cid, kw)},
		{"back", fmt.Sprintf("%s turnaround sheet back view, character_id: %s, style: consistent, %s", base, cid, kw)},
		{"three_quarter", fmt.Sprintf("%s turnaround sheet three-quarter view, character_id: %s, style: consistent, %s", base, cid, kw)},
	}
}

func buildTurnaroundDesc(it extractAssetItem, view, desc string) string {
	if strings.TrimSpace(desc) != "" {
		return desc
	}
	cid := it.CharacterID
	if cid == "" {
		cid = CharacterIDFromName(it.Name)
	}
	return fmt.Sprintf("%s %s turnaround, character_id: %s, style: consistent, Unreal Engine 5 render, consistent lighting",
		strings.TrimSpace(it.Desc), view, cid)
}

func upsertProjectAsset(db *sql.DB, projectID, userID, name, desc, assetType string, parentID int, derive string) (int64, bool, error) {
	var id int64
	err := db.QueryRow(`SELECT id FROM o_assets WHERE project_id = ? AND name = ?`, projectID, name).Scan(&id)
	if err == sql.ErrNoRows {
		res, insErr := db.Exec(`
			INSERT INTO o_assets (project_id, user_id, name, desc, type, parent_id, derive)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			projectID, userID, name, desc, assetType, parentID, derive)
		if insErr != nil {
			return 0, false, insErr
		}
		id, _ = res.LastInsertId()
		return id, true, nil
	}
	if err != nil {
		return 0, false, err
	}
	_, err = db.Exec(`UPDATE o_assets SET desc = ?, type = ?, parent_id = ?, derive = ? WHERE id = ?`,
		desc, assetType, parentID, derive, id)
	return id, false, err
}

func extractJSONArray(text string) string {
	text = strings.TrimSpace(text)
	if start := strings.Index(text, "["); start >= 0 {
		if end := strings.LastIndex(text, "]"); end > start {
			return text[start : end+1]
		}
	}
	return text
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
