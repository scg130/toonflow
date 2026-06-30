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

		clip, err := service.GenerateShotClip(ctx, r.db.DB, r.resolveVendor(), r.outputDir, projectID, episodeID, shotNum)
		if err != nil {
			logger.CtxError(ctx, err, "shot video task failed shot=%d", shotNum)
			r.broadcastTaskUpdate(tk, err.Error())
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
