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

type alwaysPolicyRewriteVendor struct {
	imageCalls int
	textCalls  int
}

func (v *alwaysPolicyRewriteVendor) VendorConfig() adapter.VendorConfig { return adapter.VendorConfig{} }
func (v *alwaysPolicyRewriteVendor) Configure(string, string)           {}
func (v *alwaysPolicyRewriteVendor) TextRequest(interface{}, string, adapter.TextParams) (*adapter.TextResponse, error) {
	v.textCalls++
	return &adapter.TextResponse{Content: "safe stylized still frame attempt"}, nil
}
func (v *alwaysPolicyRewriteVendor) VideoRequest(interface{}, string, adapter.VideoParams) (*adapter.VideoResponse, error) {
	return nil, errors.New("unused")
}
func (v *alwaysPolicyRewriteVendor) TTSRequest(interface{}, string, adapter.TTSParams) (*adapter.TTSResponse, error) {
	return nil, errors.New("unused")
}
func (v *alwaysPolicyRewriteVendor) ImageRequest(interface{}, string, adapter.ImageParams) (*adapter.ImageResponse, error) {
	v.imageCalls++
	return nil, errors.New(`agnes image error 400: Unable to generate this content. Please modify your prompt`)
}

func TestRequestShotImageWithRetry_givesUpOnPersistentPolicy(t *testing.T) {
	v := &alwaysPolicyRewriteVendor{}
	_, err := RequestShotImageWithRetry(context.Background(), v, "m", "16:9", "blood gore kill", "")
	if err == nil {
		t.Fatal("expected policy failure")
	}
	if !strings.Contains(err.Error(), "内容安全策略拦截") {
		t.Fatalf("expected clear policy failure, got %v", err)
	}
	if v.imageCalls != maxImagePolicyAttempts {
		t.Fatalf("imageCalls=%d want %d", v.imageCalls, maxImagePolicyAttempts)
	}
	// 3 rewrites between 4 image attempts
	if v.textCalls != maxImagePolicyAttempts-1 {
		t.Fatalf("textCalls=%d want %d", v.textCalls, maxImagePolicyAttempts-1)
	}
}

type rewriteFailVendor struct {
	imageCalls int
	textCalls  int
}

func (v *rewriteFailVendor) VendorConfig() adapter.VendorConfig { return adapter.VendorConfig{} }
func (v *rewriteFailVendor) Configure(string, string)           {}
func (v *rewriteFailVendor) TextRequest(interface{}, string, adapter.TextParams) (*adapter.TextResponse, error) {
	v.textCalls++
	return nil, errors.New("text api timeout")
}
func (v *rewriteFailVendor) VideoRequest(interface{}, string, adapter.VideoParams) (*adapter.VideoResponse, error) {
	return nil, errors.New("unused")
}
func (v *rewriteFailVendor) TTSRequest(interface{}, string, adapter.TTSParams) (*adapter.TTSResponse, error) {
	return nil, errors.New("unused")
}
func (v *rewriteFailVendor) ImageRequest(interface{}, string, adapter.ImageParams) (*adapter.ImageResponse, error) {
	v.imageCalls++
	return nil, errors.New(`agnes image error 400: Unable to generate this content. Please modify your prompt`)
}

func TestRequestShotImageWithRetry_rewriteFailureAborts(t *testing.T) {
	v := &rewriteFailVendor{}
	_, err := RequestShotImageWithRetry(context.Background(), v, "m", "16:9", "blood gore", "")
	if err == nil {
		t.Fatal("expected abort on rewrite failure")
	}
	if !strings.Contains(err.Error(), "重写合规 prompt 失败") {
		t.Fatalf("got %v", err)
	}
	if v.imageCalls != 1 {
		t.Fatalf("should not keep image-retrying after rewrite fail, calls=%d", v.imageCalls)
	}
	if v.textCalls != 1 {
		t.Fatalf("textCalls=%d", v.textCalls)
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
