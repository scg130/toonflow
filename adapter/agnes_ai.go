package adapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AgnesAIVendor implements Vendor for Agnes-AI official API
type AgnesAIVendor struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func init() {
	Register(&AgnesAIVendor{})
}

const (
	DefaultAgnesBaseURL = "https://apihub.agnes-ai.com/v1"
	DefaultTextModel    = "agnes-2.0-flash"
	DefaultImageModel   = "agnes-image-2.0-flash"
	DefaultVideoModel   = "agnes-video-v2.0"
)

// NewAgnesAIVendor returns a configured Agnes-AI vendor instance.
func NewAgnesAIVendor(baseURL, apiKey string) *AgnesAIVendor {
	v := &AgnesAIVendor{}
	v.Configure(baseURL, apiKey)
	return v
}

// VendorConfig returns Agnes-AI vendor metadata
func (v *AgnesAIVendor) VendorConfig() VendorConfig {
	return VendorConfig{
		ID:      "agnes_ai",
		Name:    "Agnes-AI",
		Version: "1.0.0",
		Inputs: []VendorInput{
			{Key: "base_url", Label: "API Base URL", Type: "string", Default: DefaultAgnesBaseURL, Required: true},
			{Key: "api_key", Label: "API Key", Type: "secret", Required: true},
		},
		Models: []Model{
			{ID: DefaultTextModel, Name: "Agnes 2.0 Flash", Supports: []string{"text", "image"}},
			{ID: "agnes-1.5-flash", Name: "Agnes 1.5 Flash", Supports: []string{"text", "image"}},
			{ID: DefaultImageModel, Name: "Agnes Image 2.0 Flash", Supports: []string{"image"}},
			{ID: "agnes-image-2.1-flash", Name: "Agnes Image 2.1 Flash", Supports: []string{"image"}},
			{ID: DefaultVideoModel, Name: "Agnes Video V2.0", Supports: []string{"video"}},
			{ID: "microsoft-tts", Name: "Microsoft TTS", Supports: []string{"tts"}},
		},
	}
}

// Configure set base url & api key
func (v *AgnesAIVendor) Configure(baseURL, apiKey string) {
	v.baseURL = NormalizeAgnesBaseURL(baseURL)
	v.apiKey = SanitizeAPIKey(apiKey)
	v.client = &http.Client{Timeout: 180 * time.Second}
}

// NormalizeAgnesBaseURL fixes common misconfigured Agnes API base URLs.
func NormalizeAgnesBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, "/")
	if raw == "" {
		return DefaultAgnesBaseURL
	}
	raw = strings.ReplaceAll(raw, "api.agnesai.com", "apihub.agnes-ai.com")
	if !strings.Contains(raw, "apihub.agnes-ai.com") && strings.Contains(raw, "agnes-ai.com") {
		// allow user entering https://agnes-ai.com/... by falling back to official hub
		return DefaultAgnesBaseURL
	}
	if strings.HasSuffix(raw, "/v1") {
		return raw
	}
	if strings.Contains(raw, "apihub.agnes-ai.com") {
		return raw + "/v1"
	}
	return raw
}

func (v *AgnesAIVendor) apiRoot() string {
	root := strings.TrimSuffix(v.baseURL, "/v1")
	return strings.TrimRight(root, "/")
}

// TextRequest chat completion
func (v *AgnesAIVendor) TextRequest(ctx interface{}, model string, params TextParams) (*TextResponse, error) {
	if v.client == nil {
		return nil, fmt.Errorf("Agnes-AI vendor not configured: add vendor in settings or set AGNES_AI_API_KEY")
	}
	if v.apiKey == "" {
		return nil, fmt.Errorf("API key not configured")
	}
	if model == "" {
		model = DefaultTextModel
	}
	c, ok := ctx.(context.Context)
	if !ok {
		c = context.Background()
	}

	type reqBody struct {
		Model       string        `json:"model"`
		Messages    []TextMessage `json:"messages"`
		Temperature *float32      `json:"temperature,omitempty"`
		MaxTokens   int           `json:"max_tokens,omitempty"`
	}

	body := reqBody{
		Model:     model,
		Messages:  params.Messages,
		MaxTokens: params.MaxTokens,
	}
	if params.Temperature != 0 {
		t := params.Temperature
		body.Temperature = &t
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(c, "POST", v.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create chat request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+v.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chat api request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agnes chat error %d: %s", resp.StatusCode, string(raw))
	}

	type chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	var apiResp chatResp
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode chat resp: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("agnes chat empty choices")
	}

	return &TextResponse{
		Content: apiResp.Choices[0].Message.Content,
		Model:   model,
	}, nil
}

// ImageRequest image generation
func (v *AgnesAIVendor) ImageRequest(ctx interface{}, model string, params ImageParams) (*ImageResponse, error) {
	if v.client == nil {
		return nil, fmt.Errorf("Agnes-AI vendor not configured: add vendor in settings or set AGNES_AI_API_KEY")
	}
	if v.apiKey == "" {
		return nil, fmt.Errorf("API key not configured")
	}
	if model == "" {
		model = DefaultImageModel
	}
	c, ok := ctx.(context.Context)
	if !ok {
		c = context.Background()
	}

	// 尺寸映射逻辑和原OpenAI适配器保持一致
	size := "1024x1024"
	switch {
	case params.Width > 0 && params.Height > 0:
		size = fmt.Sprintf("%dx%d", params.Width, params.Height)
	case params.AspectRatio == "16:9":
		size = "1024x576"
	case params.AspectRatio == "9:16":
		size = "576x1024"
	}

	type reqBody struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
		Size   string `json:"size"`
		ExtraBody struct {
			ResponseFormat string `json:"response_format,omitempty"`
		} `json:"extra_body,omitempty"`
		ReturnBase64 bool `json:"return_base64,omitempty"`
	}
	body := reqBody{
		Model:        model,
		Prompt:       params.Prompt,
		Size:         size,
		ReturnBase64: true,
	}
	body.ExtraBody.ResponseFormat = "b64_json"

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal image request: %w", err)
	}

	req, err := http.NewRequestWithContext(c, "POST", v.baseURL+"/images/generations", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create image request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+v.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("image api request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agnes image error %d: %s", resp.StatusCode, string(raw))
	}

	type imgResp struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
			URL     string `json:"url"`
		} `json:"data"`
	}
	var apiResp imgResp
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode image resp: %w", err)
	}
	if len(apiResp.Data) == 0 {
		return nil, fmt.Errorf("agnes image empty data")
	}

	item := apiResp.Data[0]
	if item.URL != "" {
		return &ImageResponse{DataURL: item.URL, Model: model}, nil
	}
	if item.B64JSON == "" {
		return nil, fmt.Errorf("agnes image empty data")
	}

	// 校验base64合法性
	_, err = base64.StdEncoding.DecodeString(item.B64JSON)
	if err != nil {
		return nil, fmt.Errorf("image base64 decode failed: %w", err)
	}

	dataURL := "data:image/png;base64," + item.B64JSON
	return &ImageResponse{
		DataURL: dataURL,
		Model:   model,
	}, nil
}

// VideoRequest creates an async video task and polls until completion.
func (v *AgnesAIVendor) VideoRequest(ctx interface{}, model string, params VideoParams) (*VideoResponse, error) {
	if v.client == nil {
		return nil, fmt.Errorf("Agnes-AI vendor not configured: add vendor in settings or set AGNES_AI_API_KEY")
	}
	if v.apiKey == "" {
		return nil, fmt.Errorf("API key not configured")
	}
	if model == "" {
		model = DefaultVideoModel
	}
	c, ok := ctx.(context.Context)
	if !ok {
		c = context.Background()
	}

	numFrames := 49
	frameRate := 24
	if params.Duration > 0 {
		numFrames = int(params.Duration * float32(frameRate))
		if numFrames < 17 {
			numFrames = 17
		}
		if numFrames > 441 {
			numFrames = 441
		}
		// 8n+1 rule
		numFrames = ((numFrames-1)/8)*8 + 1
	}

	type reqBody struct {
		Model      string `json:"model"`
		Prompt     string `json:"prompt"`
		Height     int    `json:"height,omitempty"`
		Width      int    `json:"width,omitempty"`
		NumFrames  int    `json:"num_frames,omitempty"`
		FrameRate  int    `json:"frame_rate,omitempty"`
	}
	body := reqBody{
		Model:     model,
		Prompt:    params.Prompt,
		Height:    768,
		Width:     1152,
		NumFrames: numFrames,
		FrameRate: frameRate,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal video request: %w", err)
	}

	req, err := http.NewRequestWithContext(c, "POST", v.baseURL+"/videos", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create video request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+v.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("video api request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agnes video error %d: %s", resp.StatusCode, string(raw))
	}

	type createResp struct {
		VideoID string `json:"video_id"`
		TaskID  string `json:"task_id"`
		Status  string `json:"status"`
		Error   any    `json:"error"`
	}
	var created createResp
	if err := json.Unmarshal(raw, &created); err != nil {
		return nil, fmt.Errorf("decode video create resp: %w", err)
	}
	videoID := created.VideoID
	if videoID == "" {
		return nil, fmt.Errorf("agnes video missing video_id: %s", string(raw))
	}

	return v.pollVideoResult(c, videoID, model)
}

func (v *AgnesAIVendor) pollVideoResult(ctx context.Context, videoID, model string) (*VideoResponse, error) {
	pollURL := fmt.Sprintf("%s/agnesapi?video_id=%s&model_name=%s", v.apiRoot(), videoID, model)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(ctx, "GET", pollURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+v.apiKey)

		resp, err := v.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("poll video result: %w", err)
		}

		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("agnes video poll error %d: %s", resp.StatusCode, string(raw))
		}

		type pollResp struct {
			Status              string `json:"status"`
			RemixedFromVideoID  string `json:"remixed_from_video_id"`
			Error               any    `json:"error"`
		}
		var result pollResp
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("decode video poll resp: %w", err)
		}

		switch result.Status {
		case "completed":
			if result.RemixedFromVideoID == "" {
				return nil, fmt.Errorf("agnes video completed but no url returned")
			}
			return &VideoResponse{VideoURL: result.RemixedFromVideoID, Model: model}, nil
		case "failed":
			return nil, fmt.Errorf("agnes video generation failed: %v", result.Error)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// TTSRequest 对接 Agnes-AI 封装的微软TTS接口
func (v *AgnesAIVendor) TTSRequest(ctx interface{}, model string, params TTSParams) (*TTSResponse, error) {
	c, ok := ctx.(context.Context)
	if !ok {
		c = context.Background()
	}

	// 微软TTS标准入参，通过Agnes-AI中转
	type reqBody struct {
		Model  string  `json:"model"` // microsoft-tts
		Text   string  `json:"text"`
		Voice  string  `json:"voice"`           // 微软音色名称，如 zh-CN-YunyangNeural
		Rate   float32 `json:"rate,omitempty"`  // 语速，对应微软rate
		Pitch  float32 `json:"pitch,omitempty"` // 音调
		Format string  `json:"response_format"`
	}

	voice := params.VoiceID
	if voice == "" {
		voice = "zh-CN-YunyangNeural"
	}

	body := reqBody{
		Model:  model,
		Text:   params.Text,
		Voice:  voice,
		Format: "base64_json",
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal microsoft tts request: %w", err)
	}

	req, err := http.NewRequestWithContext(c, "POST", v.baseURL+"/audio/microsoft-speech", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create microsoft tts request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+v.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("microsoft tts api request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("microsoft tts error %d: %s", resp.StatusCode, string(raw))
	}

	// Agnes-AI 统一返回音频base64字段
	type ttsResp struct {
		AudioB64 string `json:"audio_b64"`
	}
	var apiResp ttsResp
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode microsoft tts resp: %w", err)
	}
	if apiResp.AudioB64 == "" {
		return nil, fmt.Errorf("microsoft tts empty audio data")
	}

	// 校验base64有效性
	_, err = base64.StdEncoding.DecodeString(apiResp.AudioB64)
	if err != nil {
		return nil, fmt.Errorf("microsoft tts base64 decode failed: %w", err)
	}

	dataURL := "data:audio/mpeg;base64," + apiResp.AudioB64
	return &TTSResponse{
		AudioURL: dataURL,
		Model:    model,
	}, nil
}
