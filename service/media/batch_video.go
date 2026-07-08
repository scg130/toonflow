package media

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"toonflow/adapter"
	"toonflow/logger"
	"toonflow/service/core"
	"toonflow/service/internal/fsutil"
	"toonflow/service/storyboard"
	"toonflow/task"
)

const betweenShotCooldown = 12 * time.Second

// BatchVideoOutcome is the result of a sequential batch video run.
type BatchVideoOutcome struct {
	Clips []*ShotClip
}

// BatchVideoTaskTimeout returns a task timeout sized for N sequential Agnes video jobs.
func BatchVideoTaskTimeout(shotCount int) time.Duration {
	if shotCount <= 0 {
		shotCount = 1
	}
	d := time.Duration(shotCount) * 25 * time.Minute
	if d < 30*time.Minute {
		d = 30 * time.Minute
	}
	const maxBatch = 12 * time.Hour
	if d > maxBatch {
		d = maxBatch
	}
	return d
}

func generateShotClipUntilSuccess(ctx context.Context, db *sql.DB, v adapter.Vendor, outputDir, projectID, episodeID string, shotNumber int, continuityURL string) (*ShotClip, error) {
	var opts *ShotClipOptions
	if continuityURL != "" {
		opts = &ShotClipOptions{ContinuityImageURL: continuityURL, Versions: 2}
	} else {
		opts = &ShotClipOptions{Versions: 2}
	}
	return GenerateShotClip(ctx, db, v, outputDir, projectID, episodeID, shotNumber, opts)
}

// GenerateShotClipsSequential generates clips in shot order; each镜阶梯重试直到成功。
func GenerateShotClipsSequential(ctx context.Context, db *sql.DB, v adapter.Vendor, outputDir, projectID, episodeID string, shotNumbers []int) (*BatchVideoOutcome, error) {
	ordered := SortShotNumbers(shotNumbers)
	if len(ordered) == 0 {
		return nil, fmt.Errorf("请至少选择一个分镜")
	}
	if err := storyboard.PersistStoryboardDurations(db, projectID, episodeID); err != nil {
		return nil, err
	}

	// scene_link per shot decides continuity: only "continuous" shots inherit the
	// previous shot's last frame; "transition" shots render from their own image.
	linkByShot := map[int]string{}
	if items, ldErr := storyboard.LoadStoryboardItems(db, projectID, episodeID); ldErr == nil {
		for _, it := range items {
			linkByShot[it.ShotNumber] = it.SceneLink
		}
	}

	workDir, err := os.MkdirTemp(filepath.Join(outputDir, "clips", projectID, episodeID), "chain_")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)

	outcome := &BatchVideoOutcome{}
	var continuityURL string
	total := len(ordered)
	status := core.PipelineStatusFromContext(ctx)

	for i, shotNum := range ordered {
		if err := core.WaitIfPaused(ctx); err != nil {
			return outcome, err
		}
		status.SetShot(shotNum, i+1, total)
		if i > 0 {
			logger.CtxTrace(ctx, "batch video cooldown %s before shot=%d", betweenShotCooldown, shotNum)
			select {
			case <-ctx.Done():
				return outcome, fmt.Errorf("任务已取消（已完成 %d/%d 镜）", len(outcome.Clips), total)
			case <-time.After(betweenShotCooldown):
			}
		}

		localPct := float32(i) / float32(total) * 100
		core.ReportStepProgress(ctx, localPct,
			fmt.Sprintf("正在生成第 %d 镜视频 (%d/%d)", shotNum, i+1, total))

		// Only continue from the previous frame when THIS shot is a same-scene link.
		cont := ""
		if i > 0 && continuityURL != "" && linkByShot[shotNum] == task.SceneLinkContinuous {
			cont = continuityURL
			logger.CtxTrace(ctx, "batch video shot=%d continuous, chaining previous last frame", shotNum)
		}

		clip, err := generateShotClipUntilSuccess(ctx, db, v, outputDir, projectID, episodeID, shotNum, cont)
		if err != nil {
			logger.CtxError(ctx, err, "batch video failed shot=%d", shotNum)
			return outcome, fmt.Errorf("第 %d 镜视频生成失败: %w", shotNum, err)
		}
		outcome.Clips = append(outcome.Clips, clip)
		donePct := float32(i+1) / float32(total) * 100
		core.ReportStepProgress(ctx, donePct,
			fmt.Sprintf("第 %d 镜视频完成 (%d/%d)", shotNum, i+1, total))

		// Prepare continuity keyframe for the next same-scene shot.
		continuityURL = ""
		if i+1 < total && linkByShot[ordered[i+1]] == task.SceneLinkContinuous {
			if prevShot, ldErr := storyboard.LoadShot(db, projectID, episodeID, shotNum); ldErr == nil {
				if u := LastBeatCDNURL(prevShot); u != "" {
					continuityURL = u
					logger.CtxTrace(ctx, "batch video shot=%d last beat keyframe for next shot", shotNum)
					continue
				}
			}
			local, ok := fsutil.PublicURLToLocal(outputDir, clip.FileURL)
			if !ok {
				continue
			}
			nextURL, pubErr := ContinuityFrameFromClip(ctx, v, outputDir, local, workDir, shotNum)
			if pubErr != nil {
				logger.CtxTrace(ctx, "continuity frame failed shot=%d: %v", shotNum, pubErr)
				continue
			}
			continuityURL = nextURL
		}
	}

	if len(outcome.Clips) == 0 {
		return outcome, fmt.Errorf("批量视频未生成任何片段")
	}
	return outcome, nil
}
