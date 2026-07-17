package media

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"toonflow/adapter"
	"toonflow/logger"
	"toonflow/service/core"
	"toonflow/service/internal/fsutil"
	"toonflow/service/storyboard"
)

const (
	// preBatchVideoCooldown lets Agnes recover after a burst of image requests.
	preBatchVideoCooldown = 45 * time.Second
	// betweenShotCooldown respects Agnes video rate limit (~2 req/min).
	betweenShotCooldown = 35 * time.Second
)

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
		opts = &ShotClipOptions{ContinuityImageURL: continuityURL, Versions: 1}
	} else {
		opts = &ShotClipOptions{Versions: 1}
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

	// scene_link / same-scene decide continuity: chain from accepted previous end frame.
	linkByShot := map[int]string{}
	sceneByShot := map[int]string{}
	if items, ldErr := storyboard.LoadStoryboardItems(db, projectID, episodeID); ldErr == nil {
		for _, it := range items {
			linkByShot[it.ShotNumber] = it.SceneLink
			sceneByShot[it.ShotNumber] = strings.TrimSpace(it.Scene)
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

	logger.CtxTrace(ctx, "batch video start shots=%d pre_cooldown=%s between_shots=%s", total, preBatchVideoCooldown, betweenShotCooldown)
	if err := core.WaitWithProgress(ctx, preBatchVideoCooldown, 0,
		"生图刚结束，冷却中，%d 秒后开始批量生视频…"); err != nil {
		return nil, err
	}

	for i, shotNum := range ordered {
		if err := core.WaitIfPaused(ctx); err != nil {
			return outcome, err
		}
		status.SetShot(shotNum, i+1, total)
		localPct := float32(i) / float32(total) * 100
		if i > 0 {
			logger.CtxTrace(ctx, "batch video cooldown %s before shot=%d", betweenShotCooldown, shotNum)
			if err := core.WaitWithProgress(ctx, betweenShotCooldown, localPct,
				fmt.Sprintf("第 %d 镜视频冷却中，还剩 %%d 秒…", shotNum)); err != nil {
				return outcome, fmt.Errorf("任务已取消（已完成 %d/%d 镜）", len(outcome.Clips), total)
			}
		}

		core.ReportStepProgress(ctx, localPct,
			fmt.Sprintf("正在生成第 %d 镜视频 (%d/%d)", shotNum, i+1, total))

		// Only continue from the previous accepted frame when this shot should chain.
		cont := ""
		if i > 0 && continuityURL != "" {
			prevScene := ""
			if i > 0 {
				prevScene = sceneByShot[ordered[i-1]]
			}
			if ShouldChainVideoContinuity(linkByShot[shotNum], sceneByShot[shotNum], prevScene) {
				cont = continuityURL
				logger.CtxTrace(ctx, "batch video shot=%d chaining accepted previous last frame", shotNum)
			}
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

		// Seedance continuation: next clip must start from ACCEPTED footage end-state.
		// Do NOT fall back to planned storyboard keyframes — those often mismatch the
		// next shot's first beat (e.g. tree-bark macro vs battlefield back view).
		continuityURL = ""
		if i+1 < total && ShouldChainVideoContinuity(linkByShot[ordered[i+1]], sceneByShot[ordered[i+1]], sceneByShot[shotNum]) {
			local, ok := fsutil.PublicURLToLocal(outputDir, clip.FileURL)
			if ok {
				nextURL, pubErr := ContinuityFrameFromClip(ctx, v, outputDir, local, workDir, shotNum)
				if pubErr != nil {
					logger.CtxTrace(ctx, "continuity frame failed shot=%d: %v", shotNum, pubErr)
				} else {
					continuityURL = nextURL
					logger.CtxTrace(ctx, "batch video shot=%d accepted last-frame for next shot", shotNum)
				}
			}
			if continuityURL == "" {
				logger.CtxTrace(ctx, "batch video shot=%d no accepted continuity for next; next shot uses own keyframes", shotNum)
			}
		}
	}

	if len(outcome.Clips) == 0 {
		return outcome, fmt.Errorf("批量视频未生成任何片段")
	}
	return outcome, nil
}
