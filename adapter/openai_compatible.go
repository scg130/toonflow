package adapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAIVendor implements Vendor for any OpenAI-compatible API endpoint
// (OpenAI, Azure OpenAI, local Ollama, vLLM, etc.).
type OpenAIVendor struct {
	baseURL  string
	apiKey   string
	client   *http.Client
}

func init() {
	Register(&OpenAIVendor{})
}

// VendorConfig returns the OpenAI-compatible vendor metadata.
func (v *OpenAIVendor) VendorConfig() VendorConfig {
	return VendorConfig{
		ID:      "openai_compatible",
		Name:    "OpenAI Compatible",
		Version: "1.0.0",
		Inputs: []VendorInput{
			{Key: "base_url", Label: "API Base URL", Type: "string", Default: "https://api.openai.com/v1", Required: true},
			{Key: "api_key", Label: "API Key", Type: "secret", Required: true},
		},
		Models: []Model{
			{ID: "gpt-4o", Name: "GPT-4o", Supports: []string{"text", "image"}},
			{ID: "gpt-4o-mini", Name: "GPT-4o Mini", Supports: []string{"text", "image"}},
			{ID: "dall-e-3", Name: "DALL-E 3", Supports: []string{"image"}},
			{ID: "dall-e-2", Name: "DALL-E 2", Supports: []string{"image"}},
		},
	}
}

// Configure sets the vendor's base URL and API key.
func (v *OpenAIVendor) Configure(baseURL, apiKey string) {
	v.baseURL = baseURL
	v.apiKey = apiKey
	v.client = &http.Client{Timeout: 120 * time.Second}
}

// TextRequest sends a text completion request to the OpenAI-compatible API.
func (v *OpenAIVendor) TextRequest(ctx interface{}, model string, params TextParams) (*TextResponse, error) {
	c, ok := ctx.(context.Context)
	if !ok {
		c = context.Background()
	}

	type request struct {
		Model    string         `json:"model"`
		Messages []TextMessage  `json:"messages"`
		Temperature *float32    `json:"temperature,omitempty"`
		MaxTokens     int       `json:"max_tokens,omitempty"`
	}

	reqBody := request{
		Model:     model,
		Messages:  params.Messages,
		Temperature: func() *float32 { t := params.Temperature; return &t }(),
		MaxTokens: params.MaxTokens,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(c, "POST", v.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+v.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	return &TextResponse{
		Content: apiResp.Choices[0].Message.Content,
		Model:   model,
	}, nil
}

// ImageRequest sends an image generation request to the OpenAI-compatible API.
func (v *OpenAIVendor) ImageRequest(ctx interface{}, model string, params ImageParams) (*ImageResponse, error) {
	c, ok := ctx.(context.Context)
	if !ok {
		c = context.Background()
	}

	type request struct {
		Model   string `json:"model"`
		Prompt  string `json:"prompt"`
		Size    string `json:"size,omitempty"`
		Format  string `json:"response_format,omitempty"`
		N       int    `json:"n,omitempty"`
	}

	size := "1024x1024"
	switch {
	case params.Width > 0 && params.Height > 0:
		size = fmt.Sprintf("%dx%d", params.Width, params.Height)
	case params.AspectRatio == "16:9":
		size = "1024x576"
	case params.AspectRatio == "9:16":
		size = "576x1024"
	}

	reqBody := request{
		Model:  model,
		Prompt: params.Prompt,
		Size:   size,
		Format: "base64_json",
		N:      1,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(c, "POST", v.baseURL+"/images/generations", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+v.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Data []struct {
			Base64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(apiResp.Data) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	decoded, err := base64.StdEncoding.DecodeString(apiResp.Data[0].Base64JSON)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}

	_ = decoded // decoded but not used; we just validate the base64

	dataURL := "data:image/png;base64," + apiResp.Data[0].Base64JSON

	return &ImageResponse{
		DataURL: dataURL,
		Model:   model,
	}, nil
}

// VideoRequest is not supported by OpenAI-compatible API.
func (v *OpenAIVendor) VideoRequest(ctx interface{}, model string, params VideoParams) (*VideoResponse, error) {
	return nil, fmt.Errorf("video generation not supported by openai_compatible vendor")
}

// TTSRequest is not supported by OpenAI-compatible API.
func (v *OpenAIVendor) TTSRequest(ctx interface{}, model string, params TTSParams) (*TTSResponse, error) {
	return nil, fmt.Errorf("TTS not supported by openai_compatible vendor")
}
