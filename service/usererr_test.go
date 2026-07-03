package service

import (
	"fmt"
	"strings"
	"testing"
)

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