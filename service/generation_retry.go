package service

import (
	"context"
	"fmt"
	"time"

	"toonflow/adapter"
	"toonflow/logger"
)

// 阶梯退避：5s → 12s → 25s → 45s → 90s → 120s（封顶）
var retryBackoffSteps = []time.Duration{
	0,
	5 * time.Second,
	12 * time.Second,
	25 * time.Second,
	45 * time.Second,
	90 * time.Second,
	120 * time.Second,
}

// RetryBackoffDelay returns the wait before the next attempt (attempt is 0-based).
func RetryBackoffDelay(attempt int) time.Duration {
	if attempt < 0 {
		return 0
	}
	if attempt >= len(retryBackoffSteps) {
		return retryBackoffSteps[len(retryBackoffSteps)-1]
	}
	return retryBackoffSteps[attempt]
}

// PromptForImageAttempt applies阶梯 prompt 净化，不替换为兜底简图 prompt。
func PromptForImageAttempt(basePrompt string, attempt int) string {
	switch {
	case attempt < 3:
		return basePrompt
	case attempt < 7:
		return SanitizeImagePromptForPolicy(basePrompt, SanitizeLevelLight)
	default:
		return SanitizeImagePromptForPolicy(basePrompt, SanitizeLevelStrict)
	}
}

// RetryUntilSuccess repeats fn until it returns nil or ctx is cancelled.
func RetryUntilSuccess(ctx context.Context, label string, fn func(attempt int) error) error {
	for attempt := 0; ; attempt++ {
		if err := WaitIfPaused(ctx); err != nil {
			return err
		}
		err := fn(attempt)
		if err == nil {
			if attempt > 0 {
				logger.CtxTrace(ctx, "%s ok after %d retries", label, attempt)
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		delay := RetryBackoffDelay(attempt + 1)
		logger.CtxTrace(ctx, "%s attempt=%d failed, retry in %s: %v", label, attempt+1, delay, err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

// RequestShotImageWithRetry calls image API with阶梯 prompt 净化与固定参考图，直到成功。
func RequestShotImageWithRetry(ctx context.Context, v adapter.Vendor, model, aspectRatio, basePrompt, refURL string) (*adapter.ImageResponse, error) {
	if v == nil {
		return nil, fmt.Errorf("image vendor not configured")
	}
	var resp *adapter.ImageResponse
	err := RetryUntilSuccess(ctx, "shot image", func(attempt int) error {
		prompt := PromptForImageAttempt(basePrompt, attempt)
		r, err := v.ImageRequest(ctx, model, adapter.ImageParams{
			Prompt:            prompt,
			Model:             model,
			AspectRatio:       aspectRatio,
			ReferenceImageURL: refURL,
		})
		if err != nil {
			return err
		}
		if r == nil || (r.DataURL == "" && r.RemoteURL == "") {
			return fmt.Errorf("empty image response")
		}
		resp = r
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// RequestShotVideoWithRetry calls video API and downloads the file until success.
func RequestShotVideoWithRetry(ctx context.Context, v adapter.Vendor, model string, params adapter.VideoParams, destPath string) error {
	if v == nil {
		return fmt.Errorf("video vendor not configured")
	}
	return RetryUntilSuccess(ctx, fmt.Sprintf("shot video model=%s", model), func(attempt int) error {
		resp, err := v.VideoRequest(ctx, model, params)
		if err != nil {
			return err
		}
		if resp == nil || resp.VideoURL == "" {
			return fmt.Errorf("empty video response")
		}
		if err := adapter.DownloadHTTPURL(ctx, destPath, resp.VideoURL); err != nil {
			return err
		}
		return nil
	})
}
