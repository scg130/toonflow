package ws

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
	"toonflow/task"
)

// PipelineRunner executes generation tasks.
type PipelineRunner interface {
	Execute(ctx context.Context, t *task.Task) error
}

// GenerationService handles WebSocket-triggered generation jobs.
type GenerationService struct {
	Pipeline  PipelineRunner
	Queue     *task.Queue
	DB        *sql.DB
	Timeout   time.Duration
	OutputDir string
}

// NewGenerationService creates a generation service wired to the pipeline.
func NewGenerationService(p PipelineRunner, q *task.Queue, db *sql.DB, outputDir string, timeout time.Duration) *GenerationService {
	return &GenerationService{
		Pipeline:  p,
		Queue:     q,
		DB:        db,
		Timeout:   timeout,
		OutputDir: outputDir,
	}
}

func (gs *GenerationService) handleStartGenerate(cm *ConnManager, req *WSRequest) {
	if gs == nil || gs.Pipeline == nil || gs.Queue == nil {
		cm.Broadcast(WSResponse{Code: 1, Msg: "generation service unavailable", Step: "error"})
		return
	}
	if req.Script == "" && req.Mode != "images" && req.Mode != "video" {
		cm.Broadcast(WSResponse{Code: 1, Msg: "script is required", Step: "error"})
		return
	}

	frameDuration := req.FrameDuration
	if frameDuration <= 0 {
		frameDuration = 3
	}
	resolution := req.Resolution
	if resolution == "" {
		resolution = "1280x720"
	}
	fps := req.FPS
	if fps <= 0 {
		fps = 24
	}

	mode := req.Mode
	if mode == "" {
		mode = "full"
	}

	id := fmt.Sprintf("task_%d", time.Now().UnixNano())
	t := task.NewTask(id, req.ProjectID, req.Script, req.Style, frameDuration, resolution, fps, gs.Timeout)
	t.Mode = mode
	t.EpisodeID = req.EpisodeID
	t.GenerateShots = req.ShotNumbers

	if mode == "images" || mode == "video" {
		if err := gs.loadStoryboardFromDB(t); err != nil {
			cm.Broadcast(WSResponse{Code: 1, Msg: err.Error(), Step: "error"})
			return
		}
	}

	cm.Broadcast(WSResponse{
		Code: 0, Msg: "任务已接收", Step: "waiting", Progress: 0,
		Data: MustMarshalJSON(map[string]string{"task_id": id, "action": "started"}),
	})

	gs.Queue.Submit(t, func(ctx context.Context, tk *task.Task) error {
		err := gs.Pipeline.Execute(ctx, tk)
		if err != nil {
			cm.Broadcast(WSResponse{
				Code: 1, Msg: err.Error(), Step: "error", Progress: tk.Progress,
				Data: MustMarshalJSON(map[string]string{"task_id": tk.ID}),
			})
			return err
		}
		if tk.ProjectID != "" && len(tk.Storyboard) > 0 {
			gs.saveStoryboardToDB(tk)
		}
		return nil
	})
}

func (gs *GenerationService) loadStoryboardFromDB(t *task.Task) error {
	if t.ProjectID == "" {
		return fmt.Errorf("project_id is required for this mode")
	}
	var shotsJSON string
	var err error
	if t.EpisodeID != "" {
		sbID := fmt.Sprintf("sb_%s_%s", t.ProjectID, t.EpisodeID)
		err = gs.DB.QueryRow("SELECT shots FROM o_storyboard WHERE id = ?", sbID).Scan(&shotsJSON)
	} else {
		err = gs.DB.QueryRow(
			"SELECT shots FROM o_storyboard WHERE project_id = ? ORDER BY updated_at DESC LIMIT 1",
			t.ProjectID,
		).Scan(&shotsJSON)
	}
	if err == sql.ErrNoRows {
		return fmt.Errorf("no storyboard found for project")
	}
	if err != nil {
		return fmt.Errorf("load storyboard: %w", err)
	}
	var items []task.StoryboardItem
	if err := json.Unmarshal([]byte(shotsJSON), &items); err != nil {
		return fmt.Errorf("parse storyboard: %w", err)
	}
	if len(items) == 0 {
		return fmt.Errorf("storyboard is empty")
	}
	t.Storyboard = items
	return nil
}

func (gs *GenerationService) saveStoryboardToDB(t *task.Task) {
	shotsJSON, err := json.Marshal(t.Storyboard)
	if err != nil {
		return
	}
	sbID := fmt.Sprintf("sb_%s", t.ProjectID)
	if t.EpisodeID != "" {
		sbID = fmt.Sprintf("sb_%s_%s", t.ProjectID, t.EpisodeID)
	}
	_, _ = gs.DB.Exec(`
		INSERT INTO o_storyboard (id, project_id, scene_name, shots, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET shots = excluded.shots, updated_at = CURRENT_TIMESTAMP
	`, sbID, t.ProjectID, "episode", string(shotsJSON))
}
