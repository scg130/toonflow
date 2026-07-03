package api

import (
	"context"
	"fmt"
	"time"

	"toonflow/logger"
	"toonflow/service"
	"toonflow/task"
)

func (r *Router) submitShotVideoTask(userID, projectID, episodeID string, shotNum int) (*task.Task, error) {
	id := fmt.Sprintf("task_%d", time.Now().UnixNano())
	t := task.NewTask(id, projectID, "", "", 3, "1280x720", 24, 15*time.Minute)
	t.UserID = userID
	t.Mode = "video"
	t.EpisodeID = episodeID
	t.GenerateShots = []int{shotNum}
	service.EnrichTaskMeta(r.db.DB, t)
	t.SetState(task.StateWaiting, t.Title)

	r.queue.Submit(t, func(ctx context.Context, tk *task.Task) error {
		ctx = logger.WithID(ctx, tk.ID)
		tk.SetState(task.StateVideoGen, tk.Title)
		tk.UpdateProgress(5)
		r.broadcastTaskUpdate(tk, "视频生成中")

		clip, err := service.GenerateShotClip(ctx, r.db.DB, r.resolveVendor(), r.outputDir, projectID, episodeID, shotNum, nil)
		if err != nil {
			logger.CtxError(ctx, err, "shot video task failed shot=%d", shotNum)
			r.broadcastTaskUpdate(tk, service.UserMessageWithLogID(err, tk.ID))
			return err
		}
		tk.UpdateProgress(100)
		tk.SetState(task.StateDone, tk.Title)
		r.broadcastTaskUpdate(tk, "视频生成完成")
		logger.CtxTrace(ctx, "shot video task done shot=%d version=%d source=%s", shotNum, clip.Version, clip.Source)
		return nil
	})
	return t, nil
}

func (r *Router) submitImageGenerationTask(userID, projectID, episodeID string, shotNumbers []int) (*task.Task, error) {
	if r.pipeline == nil {
		return nil, fmt.Errorf("生成服务不可用")
	}
	if episodeID == "" {
		return nil, fmt.Errorf("请先选择一集")
	}
	if len(shotNumbers) == 0 {
		return nil, fmt.Errorf("请指定要生成的镜号")
	}
	if err := service.RequireProjectAssets(r.db.DB, projectID); err != nil {
		return nil, err
	}
	items, err := service.LoadStoryboardItems(r.db.DB, projectID, episodeID)
	if err != nil || len(items) == 0 {
		return nil, fmt.Errorf("请先生成分镜")
	}

	var artStyle, videoRatio string
	_ = r.db.QueryRow("SELECT art_style, video_ratio FROM o_project WHERE id = ?", projectID).Scan(&artStyle, &videoRatio)
	resolution := "1280x720"
	if videoRatio == "9:16" {
		resolution = "720x1280"
	}

	id := fmt.Sprintf("task_%d", time.Now().UnixNano())
	t := task.NewTask(id, projectID, "", artStyle, 3, resolution, 24, r.cfg.TaskTimeout)
	t.UserID = userID
	t.Mode = "images"
	t.EpisodeID = episodeID
	t.GenerateShots = shotNumbers
	t.Storyboard = items
	service.EnrichTaskMeta(r.db.DB, t)
	t.SetState(task.StateWaiting, t.Title)

	r.queue.Submit(t, func(ctx context.Context, tk *task.Task) error {
		ctx = logger.WithID(ctx, tk.ID)
		err := r.pipeline.Execute(ctx, tk)
		if err != nil {
			r.broadcastTaskUpdate(tk, service.UserMessageWithLogID(err, tk.ID))
			return err
		}
		if tk.ProjectID != "" && len(tk.Storyboard) > 0 {
			_ = service.SaveStoryboardItems(r.db.DB, tk.ProjectID, tk.EpisodeID, tk.Storyboard)
		}
		return nil
	})
	r.broadcastTaskUpdate(t, "任务已接收")
	return t, nil
}
