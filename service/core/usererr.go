package core

import (
	"context"
	"regexp"
	"strings"

	"toonflow/logger"
)

var (
	urlPattern    = regexp.MustCompile(`https?://[^\s"']+`)
	postURLPrefix = regexp.MustCompile(`Post "https?://[^"]+":\s*`)
)

// UserMessage returns an error string safe to show in the UI (no URLs or low-level HTTP details).
func UserMessage(err error) string {
	if err == nil {
		return ""
	}
	return sanitizeUserError(err.Error())
}

// UserMessageText sanitizes a raw error string for display.
func UserMessageText(msg string) string {
	return sanitizeUserError(msg)
}

// UserMessageWithLogID returns a sanitized error string with log_id appended for user support.
func UserMessageWithLogID(err error, logID string) string {
	return AppendLogID(UserMessage(err), logID)
}

// UserMessageFromContext sanitizes err and appends log_id from context when present.
func UserMessageFromContext(ctx context.Context, err error) string {
	return UserMessageWithLogID(err, logger.IDFromContext(ctx))
}

// AppendLogID appends log_id to a user-facing message if not already present.
func AppendLogID(msg, logID string) string {
	if logID == "" || strings.Contains(msg, logID) {
		return msg
	}
	return msg + "（log_id: " + logID + "）"
}

func sanitizeUserError(msg string) string {
	if msg == "" {
		return "操作失败，请稍后重试"
	}

	lower := strings.ToLower(msg)
		if isTimeoutError(lower) {
			return "AI 服务响应超时，请稍后重试"
		}
		if isContentPolicyError(lower) {
			return "画面描述触发内容安全策略，按剧情重写合规 prompt 后仍失败，请编辑分镜描述后重试"
		}
		if strings.Contains(msg, "401") || strings.Contains(msg, "无效的令牌") ||
			strings.Contains(lower, "invalid api key") || strings.Contains(lower, "incorrect api key") {
			return "API Key 无效或已过期，请在系统设置中检查供应商配置"
		}
		if strings.Contains(msg, "403") {
			return "无权限访问 AI 服务，请检查供应商配置"
		}
		if strings.Contains(lower, "at most 3 images") || strings.Contains(msg, "最多 3 张") {
			return "关键帧视频单次最多 3 张图，请重新生成分镜后再试"
		}
	if strings.Contains(msg, "429") || strings.Contains(lower, "rate limit") || strings.Contains(lower, "rate_limit") {
		return "AI 服务请求过于频繁，请稍后重试"
	}
	if isUpstreamUnavailable(msg, lower) {
		return "AI 服务暂时不可用，请稍后重试"
	}

	msg = postURLPrefix.ReplaceAllString(msg, "")
	msg = urlPattern.ReplaceAllString(msg, "")

	for _, prefix := range []string{
		"text request: ",
		"chat api request failed: ",
		"image api request failed: ",
		"video api request failed: ",
		"tts api request failed: ",
		"request failed: ",
		"create chat request: ",
		"create image request: ",
		"create video request: ",
		"poll video result: ",
	} {
		msg = strings.ReplaceAll(msg, prefix, "")
	}

	msg = strings.TrimSpace(msg)
	msg = strings.Trim(msg, ": ")
	for strings.HasSuffix(msg, ": ") {
		msg = strings.TrimSuffix(msg, ": ")
	}

	if msg == "" {
		return "操作失败，请稍后重试"
	}
	return msg
}

func isUpstreamUnavailable(msg, lower string) bool {
	if strings.Contains(lower, "serviceunavailable") || strings.Contains(lower, "service busy") {
		return true
	}
	for _, code := range []string{"500", "502", "503", "504"} {
		if strings.Contains(lower, "agnes video error "+code) ||
			strings.Contains(lower, "agnes image error "+code) ||
			strings.Contains(lower, "agnes text error "+code) ||
			strings.Contains(lower, "error "+code+":") {
			return true
		}
	}
	return false
}

func isTimeoutError(lower string) bool {
	return strings.Contains(lower, "context deadline exceeded") ||
		strings.Contains(lower, "client.timeout exceeded") ||
		strings.Contains(lower, "timeout exceeded while awaiting headers") ||
		strings.Contains(lower, "i/o timeout")
}

	func isContentPolicyError(lower string) bool {
		return strings.Contains(lower, "content_policy_violation") ||
			strings.Contains(lower, "unable to generate this content") ||
			strings.Contains(lower, "please modify your prompt") ||
			strings.Contains(lower, "content policy") ||
			strings.Contains(lower, "内容安全策略") ||
			strings.Contains(lower, "内容审核") ||
			strings.Contains(lower, "违规")
	}

	// IsRetryableError reports whether an error is transient (upstream/network blip,
	// timeout, rate limit, 5xx) and the operation is worth retrying automatically.
	// Non-transient failures (auth, content policy, bad request, "please generate X
	// first") return false so the caller aborts instead of looping.
	func IsRetryableError(err error) bool {
		if err == nil {
			return false
		}
		lower := strings.ToLower(err.Error())
		// Never auto-retry auth or content-policy problems — image layer already
		// escalated prompt sanitize; whole-step replay cannot fix a blocked prompt.
		if strings.Contains(lower, "401") || strings.Contains(lower, "403") ||
			strings.Contains(lower, "invalid api key") || strings.Contains(lower, "incorrect api key") ||
			strings.Contains(lower, "无效的令牌") || isContentPolicyError(lower) ||
			strings.Contains(lower, "at most 3 images") {
			return false
		}
	if isTimeoutError(lower) {
		return true
	}
	if strings.Contains(lower, "超时") || strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "timed out") || strings.Contains(lower, "upstream_error") ||
		strings.Contains(lower, "connection reset") || strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "request failed") || strings.Contains(lower, "eof") ||
		strings.Contains(lower, "暂时不可用") || strings.Contains(lower, "过于频繁") {
		return true
	}
	return strings.Contains(lower, "error 408") || strings.Contains(lower, "error 425") ||
		strings.Contains(lower, "error 429") || strings.Contains(lower, " 429") ||
		strings.Contains(lower, "error 500") || strings.Contains(lower, "error 502") ||
		strings.Contains(lower, "error 503") || strings.Contains(lower, "error 504") ||
		strings.Contains(lower, " 500") || strings.Contains(lower, " 502") || strings.Contains(lower, " 503")
}