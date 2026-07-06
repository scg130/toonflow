package ws

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"toonflow/logger"
	"toonflow/service"
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

func (gs *GenerationService) handleStartGenerate(cm *ConnManager, userID string, req *WSRequest) {
	if gs == nil || gs.Pipeline == nil || gs.Queue == nil {
		cm.Broadcast(WSResponse{Code: 1, Msg: "generation service unavailable", Step: "error"})
		return
	}
	if userID == "" {
		cm.Broadcast(WSResponse{Code: 1, Msg: "unauthorized", Step: "error"})
		return
	}
	if req.ProjectID != "" && !gs.ownsProject(userID, req.ProjectID) {
		cm.Broadcast(WSResponse{Code: 1, Msg: "project not found", Step: "error"})
		return
	}
	if req.Script == "" && req.Mode != "images" && req.Mode != "video" {
		cm.Broadcast(WSResponse{Code: 1, Msg: "script is required", Step: "error"})
		return
	}

	id := fmt.Sprintf("task_%d", time.Now().UnixNano())

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

	t := task.NewTask(id, req.ProjectID, req.Script, req.Style, frameDuration, resolution, fps, gs.Timeout)
	t.UserID = userID
	t.Mode = mode
	t.EpisodeID = req.EpisodeID
	t.GenerateShots = req.ShotNumbers
	service.EnrichTaskMeta(gs.DB, t)

	logger.Default.Trace(id, fmt.Sprintf("ws generate start mode=%s project=%s episode=%s shots=%v",
		mode, req.ProjectID, req.EpisodeID, req.ShotNumbers))

	if mode == "images" || mode == "video" {
		if err := gs.loadStoryboardFromDB(t); err != nil {
			cm.Broadcast(WSResponse{Code: 1, Msg: service.UserMessageWithLogID(err, id), Step: "error"})
			return
		}
	}
	if mode == "images" {
		if err := service.RequireProjectAssets(gs.DB, req.ProjectID); err != nil {
			cm.Broadcast(WSResponse{Code: 1, Msg: service.UserMessageWithLogID(err, id), Step: "error"})
			return
		}
	}
	if mode == "video" {
		for _, item := range t.Storyboard {
			if item.ImageURL == "" {
				cm.Broadcast(WSResponse{Code: 1, Msg: fmt.Sprintf("请先生成第 %d 镜图片后再生成视频", item.ShotNumber), Step: "error"})
				return
			}
		}
	}

	cm.Broadcast(WSResponse{
		Code: 0, Msg: "任务已接收", Step: "waiting", Progress: 0,
		Data: MustMarshalJSON(map[string]interface{}{
			"task_id": id,
			"action":  "started",
			"title":   t.Title,
			"task_update": true,
		}),
	})

	gs.Queue.Submit(t, func(ctx context.Context, tk *task.Task) error {
		ctx = logger.WithID(ctx, tk.ID)
		err := gs.Pipeline.Execute(ctx, tk)
		if err != nil {
			logger.CtxError(ctx, err, "ws generate failed task=%s mode=%s", tk.ID, tk.Mode)
			msg := service.MarkTaskFailed(tk, err)
			cm.Broadcast(WSResponse{
				Code: 0, Msg: msg, Step: string(task.StateError), Progress: tk.Progress,
				Data: MustMarshalJSON(map[string]interface{}{
					"task_id":     tk.ID,
					"task_update": true,
					"title":       tk.Title,
					"state":       task.StateError,
					"mode":        tk.Mode,
					"project_id":  tk.ProjectID,
					"episode_id":  tk.EpisodeID,
				}),
			})
			return err
		}
		if tk.ProjectID != "" && len(tk.Storyboard) > 0 {
			_ = service.SaveStoryboardItems(gs.DB, tk.ProjectID, tk.EpisodeID, tk.Storyboard)
		}
		logger.CtxTrace(ctx, "ws generate done task=%s mode=%s progress=%.0f", tk.ID, tk.Mode, tk.Progress)
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
	_ = service.SaveStoryboardItems(gs.DB, t.ProjectID, t.EpisodeID, t.Storyboard)
}

func (gs *GenerationService) ownsProject(userID, projectID string) bool {
	var owner string
	err := gs.DB.QueryRow("SELECT user_id FROM o_project WHERE id = ?", projectID).Scan(&owner)
	return err == nil && owner == userID
}
