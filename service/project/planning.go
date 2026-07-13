package project

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"toonflow/adapter"
	"toonflow/logger"
	"toonflow/service/core"
	"toonflow/service/internal/jsonutil"
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

// EpisodeParams holds per-episode generation settings.
type EpisodeParams struct {
	TargetDurationMin float64 `json:"target_duration_minutes"`
	VideoRatio        string  `json:"video_ratio"`
	ArtStyle          string  `json:"art_style"`
	TargetWords       int     `json:"target_words"`
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
		core.ReportProgress(ctx, "analyze_events", pct, fmt.Sprintf("分析章节 %d/%d: %s", i+1, total, ch.name))
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
	core.ReportProgress(ctx, "analyze_events", 100, fmt.Sprintf("事件分析完成，共 %d 章", count))
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

	core.ReportProgress(ctx, "split_episodes", 20, "AI 规划分集方案...")

	prompt := fmt.Sprintf(`根据以下小说原文和事件，规划短剧分集方案。
默认画风: %s，画面比例: %s，每集目标时长 5 分钟（4分40秒–5分10秒，红果风格）。

%s

请输出 JSON 数组，每项包含:
- episode_num (int)
- title (string) 如 "EP01: xxx"
- events_ref (string) 本集涵盖的事件摘要
- target_duration_minutes (float) 默认 5
- target_words (int) 约 800–1200
- video_ratio (string)
- art_style (string)

每集须能支撑：开场钩子→背景→矛盾升级→反转→高潮→结尾钩子，约 18–25 个镜头。
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
	text := jsonutil.ExtractJSONArray(resp.Content)
	if err := json.Unmarshal([]byte(text), &plans); err != nil {
		return nil, fmt.Errorf("parse episodes: %w (raw: %s)", err, truncate(resp.Content, 200))
	}
	if len(plans) == 0 {
		return nil, fmt.Errorf("AI 未生成分集方案")
	}

	core.ReportProgress(ctx, "split_episodes", 70, fmt.Sprintf("写入 %d 集分集数据...", len(plans)))

	_, _ = db.Exec("DELETE FROM o_episode WHERE project_id = ?", projectID)

	var episodes []Episode
	for _, p := range plans {
		if p.TargetDurationMinutes <= 0 {
			p.TargetDurationMinutes = 5
		}
		if p.TargetWords <= 0 {
			p.TargetWords = 900
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
	core.ReportProgress(ctx, "split_episodes", 100, fmt.Sprintf("分集完成，共 %d 集", len(episodes)))
	return episodes, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
