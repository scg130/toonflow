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
	if strings.Contains(msg, "401") || strings.Contains(msg, "无效的令牌") ||
		strings.Contains(lower, "invalid api key") || strings.Contains(lower, "incorrect api key") {
		return "API Key 无效或已过期，请在系统设置中检查供应商配置"
	}
	if strings.Contains(msg, "403") {
		return "无权限访问 AI 服务，请检查供应商配置"
	}
	if strings.Contains(msg, "429") {
		return "AI 服务请求过于频繁，请稍后重试"
	}
	if strings.Contains(msg, "500") || strings.Contains(msg, "502") || strings.Contains(msg, "503") {
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

func isTimeoutError(lower string) bool {
	return strings.Contains(lower, "context deadline exceeded") ||
		strings.Contains(lower, "client.timeout exceeded") ||
		strings.Contains(lower, "timeout exceeded while awaiting headers") ||
		strings.Contains(lower, "i/o timeout")
}