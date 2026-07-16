package core

import (
	"fmt"
	"strings"
	"testing"
)

func TestUserMessage_keyframesLimit(t *testing.T) {
	raw := `agnes video error 400: mode=keyframes supports at most 3 images`
	got := UserMessage(fmt.Errorf("%s", raw))
	want := "关键帧视频单次最多 3 张图，请重新生成分镜后再试"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUserMessage_503NotFromNested400(t *testing.T) {
	raw := `agnes video error 400: {"error":{"code":"400","message":"bad keyframes"}}`
	got := UserMessage(fmt.Errorf("%s", raw))
	if got == "AI 服务暂时不可用，请稍后重试" {
		t.Fatalf("400 should not map to 503, got: %s", got)
	}
}

func TestUserMessage_timeoutHidesURL(t *testing.T) {
	raw := `text request: chat api request failed: Post "https://apihub.agnes-ai.com/v1/chat/completions": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`
	got := UserMessage(fmt.Errorf("%s", raw))
	want := "AI 服务响应超时，请稍后重试"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUserMessage_stripsURLKeepsDetail(t *testing.T) {
	raw := `chat api request failed: Post "https://api.example.com/v1/chat/completions": connection refused`
	got := UserMessage(fmt.Errorf("%s", raw))
	if strings.Contains(got, "http") {
		t.Fatalf("URL leaked: %q", got)
	}
	if got == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestUserMessage_passesChineseThrough(t *testing.T) {
	got := UserMessage(fmt.Errorf("请先选择一集"))
	if got != "请先选择一集" {
		t.Fatalf("got %q", got)
	}
}

func TestUserMessage_401(t *testing.T) {
	got := UserMessage(fmt.Errorf("agnes chat error 401: invalid token"))
	if got != "API Key 无效或已过期，请在系统设置中检查供应商配置" {
		t.Fatalf("got %q", got)
	}
}

func TestUserMessageWithLogID(t *testing.T) {
	raw := `text request: chat api request failed: Post "https://apihub.agnes-ai.com/v1/chat/completions": context deadline exceeded`
	got := UserMessageWithLogID(fmt.Errorf("%s", raw), "wf_123")
	if !strings.Contains(got, "wf_123") {
		t.Fatalf("missing log_id: %q", got)
	}
	if strings.Contains(got, "http") {
		t.Fatalf("URL leaked: %q", got)
	}
}

func TestAppendLogID_skipsDuplicate(t *testing.T) {
	got := AppendLogID("失败（log_id: wf_1）", "wf_1")
	if got != "失败（log_id: wf_1）" {
		t.Fatalf("got %q", got)
	}
}

func TestIsRetryableError(t *testing.T) {
	retryable := []string{
		"shot 11: image api request failed: context deadline exceeded",
		"批量生图超时",
		"agnes chat error 404: {\"type\":\"upstream_error\",\"code\":\"404\"}",
		"agnes video error 503: service unavailable",
		"AI 服务请求过于频繁，请稍后重试",
		"connection reset by peer",
	}
	for _, m := range retryable {
		if !IsRetryableError(fmt.Errorf("%s", m)) {
			t.Errorf("expected retryable: %q", m)
		}
	}
	notRetryable := []string{
		"agnes chat error 401: invalid api key",
		"agnes image error 403: forbidden",
		"内容审核未通过：违规",
		"请先生成分镜",
		"unknown step: foo",
		`agnes image error 400: {"error":{"message":"Unable to generate this content. Please modify your prompt and try again.","type":"invalid_request_error"}}`,
		"第 2 镜被内容安全策略拦截，请编辑分镜描述",
	}
	for _, m := range notRetryable {
		if IsRetryableError(fmt.Errorf("%s", m)) {
			t.Errorf("expected NOT retryable: %q", m)
		}
	}
	if IsRetryableError(nil) {
		t.Error("nil should not be retryable")
	}
}

func TestUserMessage_contentPolicy(t *testing.T) {
	raw := `agnes image error 400: {"error":{"message":"Unable to generate this content. Please modify your prompt and try again."}}`
	got := UserMessage(fmt.Errorf("%s", raw))
	if !strings.Contains(got, "内容安全策略") {
		t.Fatalf("got %q", got)
	}
}