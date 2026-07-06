package service

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PipelineUIState is persisted pipeline progress for one episode (UI restore).
type PipelineUIState struct {
	EpisodeID   string   `json:"episode_id"`
	Paused      bool     `json:"paused"`
	Done        bool     `json:"done"`
	Progress    float32  `json:"progress"`
	ProgressMsg string   `json:"progress_msg"`
	Lines       []string `json:"lines"`
}

// InitPipelineUIState resets UI state when a pipeline starts.
func InitPipelineUIState(db *sql.DB, projectID, episodeID, initialLine string) error {
	if db == nil || projectID == "" || episodeID == "" {
		return nil
	}
	lines := []string{}
	if strings.TrimSpace(initialLine) != "" {
		lines = append(lines, initialLine)
	}
	return savePipelineUIState(db, projectID, episodeID, false, false, 2, "流水线启动中...", lines)
}

// AppendPipelineUIProgress records a progress line for the UI.
func AppendPipelineUIProgress(db *sql.DB, projectID, episodeID string, progress float32, message string) error {
	if db == nil || projectID == "" || episodeID == "" {
		return nil
	}
	st, err := loadPipelineUIState(db, projectID, episodeID)
	if err != nil {
		return err
	}
	if st == nil {
		st = &PipelineUIState{EpisodeID: episodeID, Lines: []string{}}
	}
	st.Progress = progress
	if message != "" {
		st.ProgressMsg = message
		appendPipelineLine(&st.Lines, message)
	}
	st.Done = false
	return savePipelineUIState(db, projectID, episodeID, st.Paused, st.Done, st.Progress, st.ProgressMsg, st.Lines)
}

// SetPipelineUIPaused updates pause flag and appends a status line.
func SetPipelineUIPaused(db *sql.DB, projectID, episodeID string, paused bool) error {
	if db == nil || projectID == "" || episodeID == "" {
		return nil
	}
	st, _ := loadPipelineUIState(db, projectID, episodeID)
	if st == nil {
		st = &PipelineUIState{EpisodeID: episodeID, Lines: []string{}}
	}
	st.Paused = paused
	if paused {
		appendPipelineLine(&st.Lines, "⏸ 流水线已暂停")
	} else {
		appendPipelineLine(&st.Lines, "▶ 流水线已继续")
	}
	return savePipelineUIState(db, projectID, episodeID, st.Paused, st.Done, st.Progress, st.ProgressMsg, st.Lines)
}

// FinalizePipelineUIState marks a pipeline finished and stores the final line.
func FinalizePipelineUIState(db *sql.DB, projectID, episodeID, finalLine string) error {
	if db == nil || projectID == "" || episodeID == "" {
		return nil
	}
	st, _ := loadPipelineUIState(db, projectID, episodeID)
	if st == nil {
		st = &PipelineUIState{EpisodeID: episodeID, Lines: []string{}}
	}
	if finalLine != "" {
		appendPipelineLine(&st.Lines, finalLine)
	}
	st.Done = true
	st.Paused = false
	st.Progress = 100
	if finalLine != "" {
		st.ProgressMsg = finalLine
	}
	return savePipelineUIState(db, projectID, episodeID, false, true, st.Progress, st.ProgressMsg, st.Lines)
}

// ListPipelineUIStates returns saved UI states merged with active in-memory runs.
func ListPipelineUIStates(db *sql.DB, projectID string) ([]PipelineUIState, error) {
	if db == nil || projectID == "" {
		return nil, nil
	}
	byEpisode := map[string]*PipelineUIState{}
	rows, err := db.Query(`
		SELECT episode_id, paused, done, progress, progress_msg, lines_json
		FROM o_pipeline_ui_state WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var epID, progressMsg, linesJSON string
		var paused, done int
		var progress float64
		if rows.Scan(&epID, &paused, &done, &progress, &progressMsg, &linesJSON) != nil {
			continue
		}
		st := &PipelineUIState{
			EpisodeID:   epID,
			Paused:      paused != 0,
			Done:        done != 0,
			Progress:    float32(progress),
			ProgressMsg: progressMsg,
			Lines:       decodePipelineLines(linesJSON),
		}
		byEpisode[epID] = st
	}
	for _, active := range EpisodePipelines.ListByProject(projectID) {
		st, ok := byEpisode[active.EpisodeID]
		if !ok {
			st = &PipelineUIState{EpisodeID: active.EpisodeID, Lines: []string{}}
			byEpisode[active.EpisodeID] = st
		}
		st.Paused = active.Paused
		st.Done = false
	}
	out := make([]PipelineUIState, 0, len(byEpisode))
	for epID, st := range byEpisode {
		active := EpisodePipelines.Get(projectID, epID) != nil
		if len(st.Lines) == 0 && !active {
			continue
		}
		out = append(out, *st)
	}
	return out, nil
}

func appendPipelineLine(lines *[]string, message string) {
	if message == "" {
		return
	}
	if len(*lines) > 0 && (*lines)[len(*lines)-1] == message {
		return
	}
	inProgress := strings.HasPrefix(message, "正在")
	lastLine := ""
	if len(*lines) > 0 {
		lastLine = (*lines)[len(*lines)-1]
	}
	if inProgress && strings.HasPrefix(lastLine, "正在") {
		(*lines)[len(*lines)-1] = message
		return
	}
	*lines = append(*lines, message)
}

func loadPipelineUIState(db *sql.DB, projectID, episodeID string) (*PipelineUIState, error) {
	var paused, done int
	var progress float64
	var progressMsg, linesJSON string
	err := db.QueryRow(`
		SELECT paused, done, progress, progress_msg, lines_json
		FROM o_pipeline_ui_state WHERE project_id = ? AND episode_id = ?`,
		projectID, episodeID).Scan(&paused, &done, &progress, &progressMsg, &linesJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &PipelineUIState{
		EpisodeID:   episodeID,
		Paused:      paused != 0,
		Done:        done != 0,
		Progress:    float32(progress),
		ProgressMsg: progressMsg,
		Lines:       decodePipelineLines(linesJSON),
	}, nil
}

func savePipelineUIState(db *sql.DB, projectID, episodeID string, paused, done bool, progress float32, progressMsg string, lines []string) error {
	if lines == nil {
		lines = []string{}
	}
	linesJSON, err := json.Marshal(lines)
	if err != nil {
		return fmt.Errorf("marshal pipeline lines: %w", err)
	}
	pausedInt, doneInt := 0, 0
	if paused {
		pausedInt = 1
	}
	if done {
		doneInt = 1
	}
	_, err = db.Exec(`
		INSERT INTO o_pipeline_ui_state (project_id, episode_id, paused, done, progress, progress_msg, lines_json, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id, episode_id) DO UPDATE SET
			paused = excluded.paused,
			done = excluded.done,
			progress = excluded.progress,
			progress_msg = excluded.progress_msg,
			lines_json = excluded.lines_json,
			updated_at = excluded.updated_at`,
		projectID, episodeID, pausedInt, doneInt, progress, progressMsg, string(linesJSON), time.Now().Format(time.RFC3339))
	return err
}

func decodePipelineLines(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var lines []string
	if json.Unmarshal([]byte(raw), &lines) != nil {
		return []string{}
	}
	return lines
}
