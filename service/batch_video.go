package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"toonflow/adapter"
	"toonflow/logger"
)

const (
	perShotVideoTimeout = 25 * time.Minute
	betweenShotCooldown = 12 * time.Second
	maxRetriesPerShot   = 3
	retryBackoffBase    = 8 * time.Second
)

// BatchVideoOutcome is the result of a sequential batch video run.
type BatchVideoOutcome struct {
	Clips  []*ShotClip
	Failed []BatchVideoFailure
}

// BatchVideoFailure records one shot that could not be generated.
type BatchVideoFailure struct {
	ShotNumber int    `json:"shot_number"`
	Error      string `json:"error"`
}

// BatchVideoTaskTimeout returns a task timeout sized for N sequential Agnes video jobs.
func BatchVideoTaskTimeout(shotCount int) time.Duration {
	if shotCount <= 0 {
		shotCount = 1
	}
	d := time.Duration(shotCount) * perShotVideoTimeout
	if d < 30*time.Minute {
		d = 30 * time.Minute
	}
	const maxBatch = 3 * time.Hour
	if d > maxBatch {
		d = maxBatch
	}
	return d
}

func isRetryableVideoErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "429") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "timed out") ||
		strings.Contains(msg, "download failed") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "temporarily unavailable") ||
		errors.Is(err, context.DeadlineExceeded)
}

func generateShotClipWithRetry(ctx context.Context, db *sql.DB, v adapter.Vendor, outputDir, projectID, episodeID string, shotNumber int, continuityURL string) (*ShotClip, error) {
	type attempt struct {
		continuity string
		label      string
	}
	tries := []attempt{{continuity: continuityURL, label: "continuity"}}
	if continuityURL != "" {
		tries = append(tries, attempt{continuity: "", label: "storyboard"})
	}

	var lastErr error
	for ti, att := range tries {
		for retry := 0; retry < maxRetriesPerShot; retry++ {
			if retry > 0 || ti > 0 {
				wait := retryBackoffBase * time.Duration(retry+1)
				logger.CtxTrace(ctx, "shot video retry shot=%d mode=%s retry=%d wait=%s", shotNumber, att.label, retry+1, wait)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(wait):
				}
			}

			shotCtx, cancel := context.WithTimeout(ctx, perShotVideoTimeout)
			var opts *ShotClipOptions
			if att.continuity != "" {
				opts = &ShotClipOptions{ContinuityImageURL: att.continuity}
			}
			clip, err := GenerateShotClip(shotCtx, db, v, outputDir, projectID, episodeID, shotNumber, opts)
			cancel()
			if err == nil {
				if ti > 0 {
					logger.CtxTrace(ctx, "shot video ok shot=%d fallback=%s", shotNumber, att.label)
				}
				return clip, nil
			}
			lastErr = err
			if !isRetryableVideoErr(err) {
				break
			}
		}
	}
	return nil, lastErr
}

// GenerateShotClipsSequential generates clips in shot order with cooldown, retry, and continuity fallback.
func GenerateShotClipsSequential(ctx context.Context, db *sql.DB, v adapter.Vendor, outputDir, projectID, episodeID string, shotNumbers []int) (*BatchVideoOutcome, error) {
	ordered := SortShotNumbers(shotNumbers)
	if len(ordered) == 0 {
		return nil, fmt.Errorf("请至少选择一个分镜")
	}

	workDir, err := os.MkdirTemp(filepath.Join(outputDir, "clips", projectID, episodeID), "chain_")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)

	outcome := &BatchVideoOutcome{}
	var continuityURL string

	for i, shotNum := range ordered {
		if i > 0 {
			logger.CtxTrace(ctx, "batch video cooldown %s before shot=%d", betweenShotCooldown, shotNum)
			select {
			case <-ctx.Done():
				return outcome, fmt.Errorf("任务已取消（已完成 %d/%d 镜）", len(outcome.Clips), len(ordered))
			case <-time.After(betweenShotCooldown):
			}
		}

		cont := ""
		if i > 0 && continuityURL != "" {
			cont = continuityURL
		}

		clip, err := generateShotClipWithRetry(ctx, db, v, outputDir, projectID, episodeID, shotNum, cont)
		if err != nil {
			logger.CtxError(ctx, err, "batch video failed shot=%d", shotNum)
			outcome.Failed = append(outcome.Failed, BatchVideoFailure{
				ShotNumber: shotNum,
				Error:      UserMessage(err),
			})
			continuityURL = ""
			continue
		}
		outcome.Clips = append(outcome.Clips, clip)

		local, ok := publicURLToLocal(outputDir, clip.FileURL)
		if !ok {
			continuityURL = ""
			continue
		}
		nextURL, pubErr := ContinuityFrameFromClip(ctx, v, outputDir, local, workDir, shotNum)
		if pubErr != nil {
			logger.CtxTrace(ctx, "continuity frame failed shot=%d: %v", shotNum, pubErr)
			continuityURL = ""
			continue
		}
		continuityURL = nextURL
	}

	if len(outcome.Clips) == 0 {
		return outcome, fmt.Errorf("批量视频全部失败，请稍后分批重试（建议每次 3～4 镜）")
	}
	if len(outcome.Failed) > 0 {
		return outcome, fmt.Errorf("部分分镜失败（成功 %d，失败 %d）", len(outcome.Clips), len(outcome.Failed))
	}
	return outcome, nil
}
