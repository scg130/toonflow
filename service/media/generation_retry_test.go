package media

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"toonflow/adapter"
)

func TestRetryBackoffDelay(t *testing.T) {
	if RetryBackoffDelay(0) != 0 {
		t.Fatal("first attempt should not wait")
	}
	if RetryBackoffDelay(1) != 5*time.Second {
		t.Fatal("second backoff")
	}
	if RetryBackoffDelay(100) != 120*time.Second {
		t.Fatal("cap at 120s")
	}
}

func TestCleanRewrittenImagePrompt(t *testing.T) {
	got := cleanRewrittenImagePrompt("```\ncinematic anime still of a hero\n```")
	if !strings.Contains(got, "cinematic anime still") {
		t.Fatalf("got %q", got)
	}
	got = cleanRewrittenImagePrompt("Prompt:\nstylized confrontation, soft light")
	if got != "stylized confrontation, soft light" {
		t.Fatalf("got %q", got)
	}
}

type rewriteVendor struct {
	imageCalls int
	textCalls  int
	lastPrompt string
}

func (v *rewriteVendor) VendorConfig() adapter.VendorConfig { return adapter.VendorConfig{} }
func (v *rewriteVendor) Configure(string, string)           {}
func (v *rewriteVendor) TextRequest(interface{}, string, adapter.TextParams) (*adapter.TextResponse, error) {
	v.textCalls++
	return &adapter.TextResponse{Content: "stylized anime keyframe, hero in dramatic pose, energy light effects, no graphic violence"}, nil
}
func (v *rewriteVendor) VideoRequest(interface{}, string, adapter.VideoParams) (*adapter.VideoResponse, error) {
	return nil, errors.New("unused")
}
func (v *rewriteVendor) TTSRequest(interface{}, string, adapter.TTSParams) (*adapter.TTSResponse, error) {
	return nil, errors.New("unused")
}
func (v *rewriteVendor) ImageRequest(_ interface{}, _ string, params adapter.ImageParams) (*adapter.ImageResponse, error) {
	v.imageCalls++
	v.lastPrompt = params.Prompt
	if v.imageCalls == 1 {
		return nil, errors.New(`agnes image error 400: {"error":{"message":"Unable to generate this content. Please modify your prompt and try again.","type":"invalid_request_error"}}`)
	}
	return &adapter.ImageResponse{DataURL: "data:image/png;base64,aaa", RemoteURL: "https://cdn.example.com/ok.png"}, nil
}

func TestRequestShotImageWithRetry_rewritesViaModel(t *testing.T) {
	v := &rewriteVendor{}
	resp, err := RequestShotImageWithRetry(context.Background(), v, "m", "16:9", "hero with 鲜血 gore", "https://cdn.example.com/ref.png")
	if err != nil {
		t.Fatalf("expected recovery, got %v", err)
	}
	if resp == nil || resp.RemoteURL == "" {
		t.Fatal("expected image response")
	}
	if v.textCalls < 1 {
		t.Fatal("expected text model rewrite")
	}
	if v.imageCalls < 2 {
		t.Fatalf("expected second image attempt, calls=%d", v.imageCalls)
	}
	if !strings.Contains(v.lastPrompt, "stylized anime keyframe") {
		t.Fatalf("expected rewritten prompt, got %q", v.lastPrompt)
	}
}

type alwaysPolicyVendor struct {
	imageCalls int
	textCalls  int
	prompts    []string
}

func (v *alwaysPolicyVendor) VendorConfig() adapter.VendorConfig { return adapter.VendorConfig{} }
func (v *alwaysPolicyVendor) Configure(string, string)           {}
func (v *alwaysPolicyVendor) TextRequest(interface{}, string, adapter.TextParams) (*adapter.TextResponse, error) {
	v.textCalls++
	return &adapter.TextResponse{Content: ""}, nil // empty rewrite → local ladder
}
func (v *alwaysPolicyVendor) VideoRequest(interface{}, string, adapter.VideoParams) (*adapter.VideoResponse, error) {
	return nil, errors.New("unused")
}
func (v *alwaysPolicyVendor) TTSRequest(interface{}, string, adapter.TTSParams) (*adapter.TTSResponse, error) {
	return nil, errors.New("unused")
}
func (v *alwaysPolicyVendor) ImageRequest(_ interface{}, _ string, params adapter.ImageParams) (*adapter.ImageResponse, error) {
	v.imageCalls++
	v.prompts = append(v.prompts, params.Prompt)
	return nil, errors.New(`agnes image error 400: Unable to generate this content. Please modify your prompt`)
}

func TestRequestShotImageWithRetry_givesUpOnPersistentPolicy(t *testing.T) {
	v := &alwaysPolicyVendor{}
	base := "close-up shouting, blood-stained robes, asset consistency: 染血杀意角色, blood on sword"
	_, err := RequestShotImageWithRetry(context.Background(), v, "m", "16:9", base, "")
	if err == nil {
		t.Fatal("expected policy failure")
	}
	if !strings.Contains(err.Error(), "内容安全策略拦截") {
		t.Fatalf("expected clear policy failure, got %v", err)
	}
	if v.imageCalls != maxImagePolicyAttempts {
		t.Fatalf("imageCalls=%d want %d", v.imageCalls, maxImagePolicyAttempts)
	}
	last := v.prompts[len(v.prompts)-1]
	lower := strings.ToLower(last)
	if strings.Contains(lower, "blood") || strings.Contains(last, "染血") || strings.Contains(last, "杀意") {
		t.Fatalf("final prompt still risky: %q", last)
	}
	if !strings.Contains(lower, "family friendly") {
		t.Fatalf("expected ultra-minimal anchors, got %q", last)
	}
}

type rewriteFailThenOKVendor struct {
	imageCalls int
	textCalls  int
	lastPrompt string
}

func (v *rewriteFailThenOKVendor) VendorConfig() adapter.VendorConfig { return adapter.VendorConfig{} }
func (v *rewriteFailThenOKVendor) Configure(string, string)           {}
func (v *rewriteFailThenOKVendor) TextRequest(interface{}, string, adapter.TextParams) (*adapter.TextResponse, error) {
	v.textCalls++
	return nil, errors.New("text api timeout")
}
func (v *rewriteFailThenOKVendor) VideoRequest(interface{}, string, adapter.VideoParams) (*adapter.VideoResponse, error) {
	return nil, errors.New("unused")
}
func (v *rewriteFailThenOKVendor) TTSRequest(interface{}, string, adapter.TTSParams) (*adapter.TTSResponse, error) {
	return nil, errors.New("unused")
}
func (v *rewriteFailThenOKVendor) ImageRequest(_ interface{}, _ string, params adapter.ImageParams) (*adapter.ImageResponse, error) {
	v.imageCalls++
	v.lastPrompt = params.Prompt
	if v.imageCalls == 1 {
		return nil, errors.New(`agnes image error 400: Unable to generate this content. Please modify your prompt`)
	}
	return &adapter.ImageResponse{DataURL: "data:image/png;base64,aaa", RemoteURL: "https://cdn.example.com/ok.png"}, nil
}

func TestRequestShotImageWithRetry_rewriteFailureFallsBackToSanitize(t *testing.T) {
	v := &rewriteFailThenOKVendor{}
	resp, err := RequestShotImageWithRetry(context.Background(), v, "m", "16:9", "blood gore 鲜血 染血", "")
	if err != nil {
		t.Fatalf("expected sanitize fallback recovery, got %v", err)
	}
	if resp == nil || resp.RemoteURL == "" {
		t.Fatal("expected image response")
	}
	if v.textCalls != 1 {
		t.Fatalf("textCalls=%d", v.textCalls)
	}
	if v.imageCalls != 2 {
		t.Fatalf("imageCalls=%d", v.imageCalls)
	}
	if strings.Contains(v.lastPrompt, "鲜血") || strings.Contains(v.lastPrompt, "染血") {
		t.Fatalf("fallback should scrub risky words, got %q", v.lastPrompt)
	}
}

func TestRewriteImagePromptForPolicy(t *testing.T) {
	v := &rewriteVendor{}
	out, err := RewriteImagePromptForPolicy(context.Background(), v, "hero with 鲜血", errors.New("unable to generate this content"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "stylized") {
		t.Fatalf("got %q", out)
	}
}
