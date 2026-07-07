package adapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"toonflow/logger"
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
			{ID: "microsoft-tts", Name: "Microsoft TTS (Edge 回退)", Supports: []string{"tts"}},
		},
	}
}

// Configure set base url & api key
func (v *AgnesAIVendor) Configure(baseURL, apiKey string) {
	v.baseURL = NormalizeAgnesBaseURL(baseURL)
	v.apiKey = SanitizeAPIKey(apiKey)
	v.client = &http.Client{
		Timeout: 180 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:          20,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 120 * time.Second,
		},
	}
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

	stream := params.OnDelta != nil
	jsonMode := params.JSONMode && !stream

	once := func() (*TextResponse, error) {
		resp, err := v.doChatRequest(c, model, params, stream, jsonMode)
		if err != nil && jsonMode {
			// JSON mode is best-effort: if it failed for ANY reason (provider may
			// 404/400 on the response_format param without a descriptive message),
			// retry once without it so structured calls degrade to plain output +
			// fallback parsers.
			if resp2, err2 := v.doChatRequest(c, model, params, stream, false); err2 == nil {
				return resp2, nil
			}
		}
		return resp, err
	}

	// Retry transient upstream failures (429/5xx/upstream_error/network) for
	// non-streaming calls; streaming may have already emitted partial deltas.
	maxAttempts := 1
	if !stream {
		maxAttempts = 3
	}
	var resp *TextResponse
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err = once()
		if err == nil || !IsTransientTextError(err) || attempt == maxAttempts {
			break
		}
		logger.CtxTrace(c, "agnes text model=%s attempt=%d transient error, retrying: %v", model, attempt, err)
		select {
		case <-c.Done():
			return nil, c.Err()
		case <-time.After(time.Duration(attempt) * 2 * time.Second):
		}
	}
	return resp, err
}

func (v *AgnesAIVendor) doChatRequest(c context.Context, model string, params TextParams, stream, jsonMode bool) (*TextResponse, error) {
	type responseFormat struct {
		Type string `json:"type"`
	}
	type reqBody struct {
		Model          string          `json:"model"`
		Messages       []TextMessage   `json:"messages"`
		Temperature    *float32        `json:"temperature,omitempty"`
		MaxTokens      int             `json:"max_tokens,omitempty"`
		Stream         bool            `json:"stream,omitempty"`
		ResponseFormat *responseFormat `json:"response_format,omitempty"`
	}

	body := reqBody{
		Model:     model,
		Messages:  params.Messages,
		MaxTokens: params.MaxTokens,
		Stream:    stream,
	}
	if params.Temperature != 0 {
		t := params.Temperature
		body.Temperature = &t
	}
	if jsonMode {
		body.ResponseFormat = &responseFormat{Type: "json_object"}
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
	if body.Stream {
		req.Header.Set("Accept", "text/event-stream")
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chat api request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agnes chat error %d: %s", resp.StatusCode, string(raw))
	}

	if body.Stream {
		content, err := ConsumeChatSSE(resp.Body, params.OnDelta)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(content) == "" {
			return nil, fmt.Errorf("agnes chat empty stream content")
		}
		return &TextResponse{Content: content, Model: model}, nil
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
		Model     string `json:"model"`
		Prompt    string `json:"prompt"`
		Size      string `json:"size"`
		Image     string `json:"image,omitempty"`
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
	if params.ReferenceImageURL != "" {
		body.Image = params.ReferenceImageURL
	}
	body.ExtraBody.ResponseFormat = "url"

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal image request: %w", err)
	}
	logger.CtxTrace(c, "agnes image api request model=%s size=%s body=%s", model, size, string(payload))

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
		logger.CtxTrace(c, "agnes image api response status=%d body=%s", resp.StatusCode, sanitizeImageJSONForLog(raw))
		return nil, fmt.Errorf("agnes image error %d: %s", resp.StatusCode, string(raw))
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read image resp: %w", err)
	}
	logger.CtxTrace(c, "agnes image api response status=%d body=%s", resp.StatusCode, sanitizeImageJSONForLog(raw))

	var respData map[string]interface{}
	if err := json.Unmarshal(raw, &respData); err != nil {
		return nil, fmt.Errorf("decode image resp: %w", err)
	}

	imgURL, b64, err := extractImageURLOrB64(respData)
	if err != nil {
		return nil, err
	}
	if b64 != "" {
		if _, err = base64.StdEncoding.DecodeString(b64); err != nil {
			return nil, fmt.Errorf("image base64 decode failed: %w", err)
		}
		remote := ""
		if IsCDNImageURL(imgURL) {
			remote = imgURL
		}
		logger.CtxTrace(c, "agnes image parsed base64 len=%d remote_url=%s", len(b64), remote)
		return &ImageResponse{
			DataURL:   "data:image/png;base64," + b64,
			RemoteURL: remote,
			Model:     model,
		}, nil
	}
	if IsCDNImageURL(imgURL) {
		logger.CtxTrace(c, "agnes image parsed remote_url=%s (no inline base64)", imgURL)
		return &ImageResponse{DataURL: imgURL, RemoteURL: imgURL, Model: model}, nil
	}
	return nil, fmt.Errorf("agnes image empty data: %s", truncateForError(string(raw), 400))
}

// PublishImageForVideo uploads a local image and returns an Agnes CDN URL (~24h) for I2V.
func (v *AgnesAIVendor) PublishImageForVideo(ctx interface{}, localPath string) (string, error) {
	if v == nil || v.client == nil || v.apiKey == "" {
		return "", fmt.Errorf("Agnes-AI vendor not configured")
	}
	c, ok := ctx.(context.Context)
	if !ok {
		c = context.Background()
	}
	raw, err := os.ReadFile(localPath)
	if err != nil {
		return "", fmt.Errorf("read image: %w", err)
	}
	mime := "image/png"
	lower := strings.ToLower(localPath)
	if strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg") {
		mime = "image/jpeg"
	}
	type reqBody struct {
		Model        string `json:"model"`
		Prompt       string `json:"prompt"`
		Image        string `json:"image"`
		Size         string `json:"size"`
		ReturnBase64 bool   `json:"return_base64"`
		ExtraBody    struct {
			ResponseFormat string `json:"response_format,omitempty"`
		} `json:"extra_body,omitempty"`
	}
	body := reqBody{
		Model:        DefaultImageModel,
		Prompt:       "preserve exact same composition, subject, colors and lighting, high fidelity still frame",
		Image:        "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw),
		Size:         "1024x1024",
		ReturnBase64: false,
	}
	body.ExtraBody.ResponseFormat = "url"

	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	logger.CtxTrace(c, "agnes publish image for video local=%s", localPath)

	req, err := http.NewRequestWithContext(c, "POST", v.baseURL+"/images/generations", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+v.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("publish image request failed: %w", err)
	}
	defer resp.Body.Close()
	respRaw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("agnes publish image error %d: %s", resp.StatusCode, string(respRaw))
	}
	var respData map[string]interface{}
	if err := json.Unmarshal(respRaw, &respData); err != nil {
		return "", err
	}
	imgURL, _, err := extractImageURLOrB64(respData)
	if err != nil {
		return "", err
	}
	if !IsCDNImageURL(imgURL) {
		return "", fmt.Errorf("agnes publish image: no cdn url in response")
	}
	return imgURL, nil
}

// extractImageURLOrB64 parses Agnes image API responses (data[].url, output[], top-level url, etc.).
func extractImageURLOrB64(data map[string]interface{}) (imgURL, b64 string, err error) {
	if errVal, ok := data["error"]; ok && errVal != nil {
		switch e := errVal.(type) {
		case map[string]interface{}:
			msg := stringField(e, "message")
			if msg == "" {
				msg = stringField(e, "code")
			}
			if msg == "" {
				msg = fmt.Sprint(e)
			}
			return "", "", fmt.Errorf("文生图 API 错误: %s", msg)
		case string:
			if strings.TrimSpace(e) != "" {
				return "", "", fmt.Errorf("文生图 API 错误: %s", e)
			}
		}
	}

	if items, ok := data["data"].([]interface{}); ok && len(items) > 0 {
		switch first := items[0].(type) {
		case map[string]interface{}:
			var u, b string
			for _, key := range []string{"url", "image_url", "image"} {
				if found := stringField(first, key); isHTTPURL(found) {
					u = found
					break
				}
			}
			for _, key := range []string{"b64_json", "base64"} {
				if found := stringField(first, key); found != "" {
					b = found
					break
				}
			}
			if u != "" || b != "" {
				return u, b, nil
			}
			// 兜底：data[0] 内任意 https 字符串（如 platform-outputs.agnes-ai.space/...）
			for _, v := range first {
				if s, ok := v.(string); ok && isHTTPURL(s) {
					return s, b, nil
				}
			}
		case string:
			if isHTTPURL(first) {
				return first, "", nil
			}
			return "", first, nil
		}
	}

	if out, ok := data["output"].([]interface{}); ok && len(out) > 0 {
		if x, ok := out[0].(string); ok {
			if isHTTPURL(x) {
				return x, "", nil
			}
			return "", x, nil
		}
	}

	for _, key := range []string{"url", "image_url", "image"} {
		if u := stringField(data, key); isHTTPURL(u) {
			return u, "", nil
		}
	}

	return "", "", nil
}

func isHTTPURL(s string) bool {
	return IsCDNImageURL(s)
}

// IsCDNImageURL reports whether s is an http(s) URL for Agnes video/img2video (not base64/data).
func IsCDNImageURL(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "data:") {
		return false
	}
	if strings.HasPrefix(s, "/output/") || strings.HasPrefix(s, "/") {
		return false
	}
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func stringField(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func truncateForError(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// sanitizeImageJSONForLog keeps full JSON structure but omits huge base64 payloads.
func sanitizeImageJSONForLog(raw []byte) string {
	var data interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		if len(raw) > 4096 {
			return string(raw[:4096]) + "...(truncated)"
		}
		return string(raw)
	}
	sanitizeB64Fields(data)
	out, err := json.Marshal(data)
	if err != nil {
		return string(raw)
	}
	s := string(out)
	if len(s) > 16384 {
		return s[:16384] + "...(truncated)"
	}
	return s
}

func sanitizeB64Fields(v interface{}) {
	switch x := v.(type) {
	case map[string]interface{}:
		for k, val := range x {
			if k == "b64_json" || k == "base64" {
				if s, ok := val.(string); ok && len(s) > 80 {
					x[k] = fmt.Sprintf("<base64 len=%d>", len(s))
					continue
				}
			}
			sanitizeB64Fields(val)
		}
	case []interface{}:
		for _, item := range x {
			sanitizeB64Fields(item)
		}
	}
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

	frameRate := 24
	numFrames := FramesForVideoDuration(params.Duration, frameRate)

	type reqBody struct {
		Model             string `json:"model"`
		Prompt            string `json:"prompt"`
		Image             string `json:"image,omitempty"`
		Height            int    `json:"height,omitempty"`
		Width             int    `json:"width,omitempty"`
		NumFrames         int    `json:"num_frames,omitempty"`
		FrameRate         int    `json:"frame_rate,omitempty"`
		NegativePrompt    string `json:"negative_prompt,omitempty"`
		NumInferenceSteps int    `json:"num_inference_steps,omitempty"`
	}
	width := params.Width
	height := params.Height
	if width <= 0 {
		width = 1152
	}
	if height <= 0 {
		height = 768
	}
	body := reqBody{
		Model:             model,
		Prompt:            params.Prompt,
		Height:            height,
		Width:             width,
		NumFrames:         numFrames,
		FrameRate:         frameRate,
		NegativePrompt:    params.Negative,
		NumInferenceSteps: 45,
	}
	imageURL := strings.TrimSpace(params.ImageURL)
	if imageURL != "" {
		if !IsCDNImageURL(imageURL) {
			return nil, fmt.Errorf("图生视频须使用 Agnes CDN 图片 URL（https://），不能传 base64 或本地路径")
		}
		body.Image = imageURL
		logger.CtxTrace(c, "agnes video api image_url=%s", imageURL)
	}
	if body.NegativePrompt == "" {
		body.NegativePrompt = "static image, frozen frame, no motion, blurry, low quality, distorted, watermark"
	}
	logger.CtxTrace(c, "agnes video api request model=%s %dx%d frames=%d steps=%d has_image=%v",
		model, width, height, numFrames, body.NumInferenceSteps, imageURL != "")

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
	const pollInterval = 10 * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// 创建任务后先等待再查，降低触发 video status 限流的概率。
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(pollInterval):
	}

	backoff := pollInterval
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

		if resp.StatusCode == http.StatusTooManyRequests {
			logger.CtxTrace(ctx, "agnes video poll 429, backoff %s video_id=%s", backoff, videoID)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff += pollInterval
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("agnes video poll error %d: %s", resp.StatusCode, string(raw))
		}

		backoff = pollInterval

		type pollResp struct {
			Status             string `json:"status"`
			URL                string `json:"url"`
			RemixedFromVideoID string `json:"remixed_from_video_id"`
			Error              any    `json:"error"`
		}
		var result pollResp
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("decode video poll resp: %w", err)
		}
		switch result.Status {
		case "completed":
			videoURL := result.URL
			if videoURL == "" {
				videoURL = result.RemixedFromVideoID
			}
			if videoURL == "" {
				logger.CtxInfo(ctx, "agnes video completed but no url, video_id=%s raw=%s", videoID, string(raw))
				return nil, fmt.Errorf("agnes video completed but no url returned: %s", string(raw))
			}
			logger.CtxInfo(ctx, "agnes video completed video_id=%s url=%s", videoID, videoURL)
			return &VideoResponse{VideoURL: videoURL, Model: model}, nil
		case "failed":
			logger.CtxInfo(ctx, "agnes video failed, video_id=%s raw=%s", videoID, string(raw))
			return nil, fmt.Errorf("agnes video generation failed: %v", result.Error)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// TTSRequest 对接 OpenAI 兼容 TTS；Agnes 网关若未开放则 narration 会回退 Edge TTS。
func (v *AgnesAIVendor) TTSRequest(ctx interface{}, model string, params TTSParams) (*TTSResponse, error) {
	c, ok := ctx.(context.Context)
	if !ok {
		c = context.Background()
	}
	if v.client == nil {
		return nil, fmt.Errorf("Agnes-AI vendor not configured")
	}
	if v.apiKey == "" {
		return nil, fmt.Errorf("API key not configured")
	}
	if model == "" {
		model = DefaultTTSModel
	}
	voice := params.VoiceID
	if voice == "" {
		voice = "zh-CN-YunyangNeural"
	}

	// OpenAI-compatible speech endpoint (some gateways expose this).
	type openAISpeechReq struct {
		Model          string  `json:"model"`
		Input          string  `json:"input"`
		Voice          string  `json:"voice"`
		ResponseFormat string  `json:"response_format,omitempty"`
		Speed          float32 `json:"speed,omitempty"`
	}
	openBody, _ := json.Marshal(openAISpeechReq{
		Model:          model,
		Input:          params.Text,
		Voice:          voice,
		ResponseFormat: "mp3",
	})
	if resp, err := v.postTTS(c, v.baseURL+"/audio/speech", openBody, true); err == nil {
		return resp, nil
	}

	// Legacy Agnes microsoft-speech shape (if enabled on gateway).
	type legacyReq struct {
		Model  string  `json:"model"`
		Text   string  `json:"text"`
		Voice  string  `json:"voice"`
		Rate   float32 `json:"rate,omitempty"`
		Pitch  float32 `json:"pitch,omitempty"`
		Format string  `json:"response_format"`
	}
	legacyBody, _ := json.Marshal(legacyReq{
		Model: model, Text: params.Text, Voice: voice, Format: "base64_json",
	})
	return v.postTTS(c, v.baseURL+"/audio/microsoft-speech", legacyBody, false)
}

func (v *AgnesAIVendor) postTTS(c context.Context, url string, payload []byte, binaryOK bool) (*TTSResponse, error) {
	req, err := http.NewRequestWithContext(c, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+v.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tts api request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tts error %d: %s", resp.StatusCode, string(raw))
	}

	if binaryOK && len(raw) > 0 && raw[0] != '{' {
		b64 := base64.StdEncoding.EncodeToString(raw)
		return &TTSResponse{
			AudioURL: "data:audio/mpeg;base64," + b64,
			Model:    DefaultTTSModel,
		}, nil
	}

	type ttsResp struct {
		AudioB64 string `json:"audio_b64"`
		Audio    string `json:"audio"`
		Data     string `json:"data"`
	}
	var apiResp ttsResp
	if err := json.Unmarshal(raw, &apiResp); err != nil {
		return nil, fmt.Errorf("decode tts resp: %w", err)
	}
	b64 := apiResp.AudioB64
	if b64 == "" {
		b64 = apiResp.Audio
	}
	if b64 == "" {
		b64 = apiResp.Data
	}
	if b64 == "" {
		return nil, fmt.Errorf("tts empty audio data")
	}
	if _, err := base64.StdEncoding.DecodeString(b64); err != nil {
		return nil, fmt.Errorf("tts base64 decode failed: %w", err)
	}
	return &TTSResponse{
		AudioURL: "data:audio/mpeg;base64," + b64,
		Model:    DefaultTTSModel,
	}, nil
}
