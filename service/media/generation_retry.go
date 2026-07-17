package media

import (
	"context"
	"fmt"
	"strings"
	"time"

	"toonflow/adapter"
	"toonflow/logger"
	"toonflow/service/core"
	"toonflow/service/project"
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

// maxImagePolicyAttempts: 原 prompt + light + action-core + safe + ultra-minimal。
const maxImagePolicyAttempts = 5

// maxImageTransientAttempts caps timeout/5xx retries so one stuck shot cannot
// freeze the whole one-click episode run for hours.
const maxImageTransientAttempts = 12

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

// RewriteImagePromptForPolicy asks the text model to regenerate a story-faithful,
// policy-safe image prompt after the image API rejected the previous one.
func RewriteImagePromptForPolicy(ctx context.Context, v adapter.Vendor, blockedPrompt string, policyErr error, round int) (string, error) {
	if v == nil {
		return "", fmt.Errorf("vendor not configured")
	}
	blockedPrompt = strings.TrimSpace(blockedPrompt)
	if blockedPrompt == "" {
		return "", fmt.Errorf("empty blocked prompt")
	}
	if round < 1 {
		round = 1
	}

	// Feed only the visual action core — full asset lore often causes the text
	// model to refuse / return empty when asked to "comply with content policy".
	actionCore := project.ExtractVisualActionCore(blockedPrompt)
	if actionCore == "" {
		actionCore = blockedPrompt
	}
	if len([]rune(actionCore)) > 400 {
		actionCore = string([]rune(actionCore)[:400])
	}

	system := strings.TrimSpace(`你是短剧分镜「文生图提示词」改写专家。
上游图片模型因内容安全策略拒绝了当前 prompt。请输出一条可过审、仍贴合动作镜头的生图 prompt。

硬性要求：
1. 尽量保留镜头景别、角色外形、姿势动作、情绪与场景氛围；杀意、暴怒、对峙、打斗等剧情表达可以保留。
2. 仅弱化真正极限的直白描写：鲜血喷溅、肢解碎尸、内脏、裸露色情等；改成风格化/特效化表达即可。
3. 避免 gore / nude / blood / corpse / torture 等英文极限词及鲜血/碎尸/内脏/裸体等中文直白词。
4. 输出一条可直接喂给文生图 API 的英文为主 prompt，不要解释、不要 Markdown。`)

	reason := strings.TrimSpace(core.UserMessage(policyErr))
	if reason == "" {
		reason = "content policy"
	}
	user := fmt.Sprintf("第 %d 次改写。被拒原因：%s\n\n镜头动作（请改写为合规生图 prompt）：\n%s",
		round, reason, actionCore)

	resp, err := v.TextRequest(ctx, adapter.DefaultTextModel, adapter.TextParams{
		Messages: []adapter.TextMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0.45,
		MaxTokens:   400,
	})
	if err != nil {
		return "", err
	}
	out := cleanRewrittenImagePrompt(resp.Content)
	if out == "" {
		return "", fmt.Errorf("模型未返回有效合规 prompt")
	}
	return project.SanitizeImagePromptForPolicy(out, project.SanitizeLevelLight), nil
}

func cleanRewrittenImagePrompt(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "\n"); strings.HasPrefix(strings.ToLower(s), "prompt") && i > 0 {
		s = strings.TrimSpace(s[i+1:])
	}
	parts := strings.Split(s, "\n\n")
	s = strings.TrimSpace(parts[0])
	s = strings.Trim(s, "\"'`")
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > 500 {
		s = string(runes[:500])
	}
	return s
}

// nextPolicyPrompt picks the next prompt after a content-policy block.
// Ladder is monotonic (never regresses to a riskier full asset prompt):
//  1. model rewrite (once) or light sanitize
//  2. action-core only (strip asset lore) + light sanitize
//  3. safe fallback
//  4. ultra-minimal escape hatch
func nextPolicyPrompt(ctx context.Context, v adapter.Vendor, basePrompt, currentPrompt string, policyHits int, policyErr error) (string, string, error) {
	switch policyHits {
	case 1:
		rewritten, err := RewriteImagePromptForPolicy(ctx, v, basePrompt, policyErr, 1)
		if err == nil && strings.TrimSpace(rewritten) != "" && rewritten != currentPrompt {
			return rewritten, "model-rewrite", nil
		}
		if err != nil {
			logger.CtxTrace(ctx, "shot image prompt rewrite skipped: %v", err)
		}
		light := project.SanitizeImagePromptForPolicy(basePrompt, project.SanitizeLevelLight)
		if light == "" {
			light = project.BuildSafeImagePromptFallback(basePrompt)
		}
		return light, "light-sanitize", nil
	case 2:
		core := project.ExtractVisualActionCore(basePrompt)
		core = project.SanitizeImagePromptForPolicy(core, project.SanitizeLevelLight)
		if core == "" {
			core = project.BuildSafeImagePromptFallback(basePrompt)
		}
		return core, "action-core", nil
	case 3:
		return project.BuildSafeImagePromptFallback(basePrompt), "safe-fallback", nil
	default:
		return project.BuildUltraMinimalSafeImagePrompt(basePrompt), "ultra-minimal", nil
	}
}

// RetryUntilSuccess repeats fn until it returns nil or ctx is cancelled.
func RetryUntilSuccess(ctx context.Context, label string, fn func(attempt int) error) error {
	status := core.PipelineStatusFromContext(ctx)
	for attempt := 0; ; attempt++ {
		if err := core.WaitIfPaused(ctx); err != nil {
			return err
		}
		err := fn(attempt)
		if err == nil {
			if attempt > 0 {
				status.ClearRetry()
				logger.CtxTrace(ctx, "%s ok after %d retries", label, attempt)
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		delay := RetryBackoffDelay(attempt + 1)
		logger.CtxTrace(ctx, "%s attempt=%d failed, retry in %s: %v", label, attempt+1, delay, err)
		if status != nil {
			status.SetRetry(attempt+1, 0, core.UserMessage(err))
			if seq, total := status.ShotProgress(); total > 0 {
				localPct := float32(seq-1) / float32(total) * 100
				msg := fmt.Sprintf("第 %d 镜自动重试中（第 %d 次，%s 后）：%s",
					status.CurrentShot(), attempt+1, delay, core.UserMessage(err))
				core.ReportStepProgress(ctx, localPct, msg)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

// RequestShotImageWithRetry calls image API; on content-policy blocks it escalates
// through a monotonic sanitize ladder before giving up.
func RequestShotImageWithRetry(ctx context.Context, v adapter.Vendor, model, aspectRatio, basePrompt, refURL string) (*adapter.ImageResponse, error) {
	if v == nil {
		return nil, fmt.Errorf("image vendor not configured")
	}
	status := core.PipelineStatusFromContext(ctx)
	policyHits := 0
	transientHits := 0
	prompt := basePrompt
	useRef := refURL
	var lastErr error

	for attempt := 0; ; attempt++ {
		if err := core.WaitIfPaused(ctx); err != nil {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		logger.CtxTrace(ctx, "shot image attempt=%d policyHits=%d ref=%v promptLen=%d",
			attempt+1, policyHits, useRef != "", len(prompt))

		r, err := v.ImageRequest(ctx, model, adapter.ImageParams{
			Prompt:            prompt,
			Model:             model,
			AspectRatio:       aspectRatio,
			ReferenceImageURL: useRef,
		})
		if err == nil {
			if r == nil || (r.DataURL == "" && r.RemoteURL == "") {
				err = fmt.Errorf("empty image response")
			} else {
				if attempt > 0 {
					status.ClearRetry()
					logger.CtxTrace(ctx, "shot image ok after %d retries (policyHits=%d)", attempt, policyHits)
				}
				return r, nil
			}
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		policy := project.IsContentPolicyViolation(err)
		if !policy {
			transientHits++
			if transientHits >= maxImageTransientAttempts {
				logger.CtxTrace(ctx, "shot image giving up after %d transient failures: %v", transientHits, err)
				return nil, lastErr
			}
			delay := RetryBackoffDelay(attempt + 1)
			reportImageRetry(ctx, status, attempt+1, maxImageTransientAttempts, delay, err)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
			continue
		}

		policyHits++
		if policyHits >= maxImagePolicyAttempts {
			logger.CtxTrace(ctx, "shot image giving up after %d policy blocks: %v", policyHits, err)
			return nil, fmt.Errorf("内容安全策略拦截，已按剧情重写并本地净化兜底仍失败: %w", err)
		}

		// Drop reference after first policy block — ref image can also trip filters.
		if policyHits >= 1 {
			useRef = ""
		}

		next, stage, stageErr := nextPolicyPrompt(ctx, v, basePrompt, prompt, policyHits, err)
		if stageErr != nil {
			return nil, fmt.Errorf("内容安全策略拦截后准备下一轮 prompt 失败: %w（原错误: %v）", stageErr, err)
		}
		if status != nil {
			if seq, total := status.ShotProgress(); total > 0 {
				localPct := float32(seq-1) / float32(total) * 100
				core.ReportStepProgress(ctx, localPct, fmt.Sprintf(
					"第 %d 镜触发内容安全策略，正在降级生图 prompt（%s，第 %d/%d 次）…",
					status.CurrentShot(), stage, policyHits, maxImagePolicyAttempts-1))
			}
		}
		logger.CtxTrace(ctx, "shot image policy stage=%s round=%d len=%d", stage, policyHits, len(next))
		prompt = next

		delay := 400 * time.Millisecond
		reportImageRetry(ctx, status, attempt+1, maxImagePolicyAttempts, delay, err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
}

func reportImageRetry(ctx context.Context, status *core.PipelineStatus, attempt, maxAtt int, delay time.Duration, err error) {
	logger.CtxTrace(ctx, "shot image attempt=%d failed, retry in %s: %v", attempt, delay, err)
	if status == nil {
		return
	}
	status.SetRetry(attempt, maxAtt, core.UserMessage(err))
	if seq, total := status.ShotProgress(); total > 0 {
		localPct := float32(seq-1) / float32(total) * 100
		msg := fmt.Sprintf("第 %d 镜自动重试中（第 %d 次，%s 后）：%s",
			status.CurrentShot(), attempt, delay, core.UserMessage(err))
		core.ReportStepProgress(ctx, localPct, msg)
	}
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
