package adapter

import "sync"

// VendorConfig describes a vendor's capabilities and available models.
type VendorConfig struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Version   string        `json:"version"`
	Inputs    []VendorInput `json:"inputs"`
	Models    []Model       `json:"models"`
}

// VendorInput describes a configuration input field for a vendor.
type VendorInput struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"` // "string", "secret", "select", "number"
	Default  string   `json:"default,omitempty"`
	Options  []string `json:"options,omitempty"`
	Required bool     `json:"required"`
}

// Model describes a single AI model offered by a vendor.
type Model struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	// Supports lists which capabilities this model provides.
	Supports []string `json:"supports"` // "text", "image", "video", "tts"
}

// TextMessage represents a single message in a text conversation.
type TextMessage struct {
	Role    string `json:"role"`    // "system", "user", "assistant"
	Content string `json:"content"`
}

// TextParams holds parameters for text generation requests.
type TextParams struct {
	Messages       []TextMessage `json:"messages"`
	Model          string        `json:"model"`
	Temperature    float32       `json:"temperature,omitempty"`
	MaxTokens      int           `json:"max_tokens,omitempty"`
	ResponseFormat string        `json:"response_format,omitempty"`
}

// TextResponse is the result of a text generation request.
type TextResponse struct {
	Content string `json:"content"`
	Model   string `json:"model"`
}

// ImageParams holds parameters for image generation requests.
type ImageParams struct {
	Prompt         string `json:"prompt"`
	NegativePrompt string `json:"negative_prompt,omitempty"`
	Model          string `json:"model"`
	Width          int    `json:"width,omitempty"`
	Height         int    `json:"height,omitempty"`
	Seed           *int64 `json:"seed,omitempty"`
	AspectRatio    string `json:"aspect_ratio,omitempty"`
}

// ImageResponse is the result of an image generation request.
type ImageResponse struct {
	DataURL string `json:"data_url"` // base64-encoded PNG data URL
	Model   string `json:"model"`
}

// VideoParams holds parameters for video generation requests.
type VideoParams struct {
	Prompt   string  `json:"prompt"`
	ImageURL string  `json:"image_url,omitempty"`
	Model    string  `json:"model"`
	Duration float32 `json:"duration,omitempty"`
}

// VideoResponse is the result of a video generation request.
type VideoResponse struct {
	VideoURL string `json:"video_url"`
	Model    string `json:"model"`
}

// TTSParams holds parameters for text-to-speech requests.
type TTSParams struct {
	Text    string `json:"text"`
	VoiceID string `json:"voice_id,omitempty"`
	Model   string `json:"model"`
}

// TTSResponse is the result of a TTS request.
type TTSResponse struct {
	AudioURL string `json:"audio_url"`
	Model    string `json:"model"`
}

// Vendor is the interface all adapter implementations must satisfy.
//
// Each vendor (OpenAI, Kling, Vidu, MiniMax, etc.) implements this interface
// and registers itself via Register() in its init() function.
// To add a new vendor, create a new file in adapter/ with a struct that
// implements Vendor and calls adapter.Register() in init().
type Vendor interface {
	// VendorConfig returns the vendor's capability description.
	VendorConfig() VendorConfig

	// TextRequest sends a text generation request.
	TextRequest(ctx interface{}, model string, params TextParams) (*TextResponse, error)

	// ImageRequest sends an image generation request.
	ImageRequest(ctx interface{}, model string, params ImageParams) (*ImageResponse, error)

	// VideoRequest sends a video generation request.
	VideoRequest(ctx interface{}, model string, params VideoParams) (*VideoResponse, error)

	// TTSRequest sends a text-to-speech request.
	TTSRequest(ctx interface{}, model string, params TTSParams) (*TTSResponse, error)
}

// --- Registry ---

var (
	mu      sync.RWMutex
	vendors = make(map[string]Vendor)
)

// Register registers a vendor adapter by its ID. Typically called from init().
func Register(v Vendor) {
	cfg := v.VendorConfig()
	mu.Lock()
	defer mu.Unlock()
	vendors[cfg.ID] = v
}

// Get returns a vendor by ID.
func Get(id string) (Vendor, bool) {
	mu.RLock()
	defer mu.RUnlock()
	v, ok := vendors[id]
	return v, ok
}

// List returns all registered vendor IDs.
func List() []string {
	mu.RLock()
	defer mu.RUnlock()
	ids := make([]string, 0, len(vendors))
	for id := range vendors {
		ids = append(ids, id)
	}
	return ids
}

// All returns all registered vendors.
func All() []Vendor {
	mu.RLock()
	defer mu.RUnlock()
	result := make([]Vendor, 0, len(vendors))
	for _, v := range vendors {
		result = append(result, v)
	}
	return result
}
