package adapter

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
)

const DefaultTTSModel = "microsoft-tts"

// SynthesizeSpeech returns MP3 bytes via vendor TTS, then Edge neural TTS.
func SynthesizeSpeech(ctx context.Context, v Vendor, text, voice string) ([]byte, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("empty tts text")
	}
	if voice == "" {
		voice = "zh-CN-YunyangNeural"
	}

	if v != nil {
		resp, err := v.TTSRequest(ctx, DefaultTTSModel, TTSParams{
			Text:    text,
			VoiceID: voice,
		})
		if err == nil {
			data, decErr := decodeTTSDataURL(resp.AudioURL)
			if decErr == nil && len(data) > 0 {
				return data, nil
			}
		}
	}

	return EdgeTTSSynthesize(ctx, text, voice)
}

func decodeTTSDataURL(audioURL string) ([]byte, error) {
	if strings.HasPrefix(audioURL, "data:") {
		idx := strings.Index(audioURL, "base64,")
		if idx < 0 {
			return nil, fmt.Errorf("invalid tts data url")
		}
		return base64.StdEncoding.DecodeString(audioURL[idx+7:])
	}
	if strings.HasPrefix(audioURL, "http://") || strings.HasPrefix(audioURL, "https://") {
		return nil, fmt.Errorf("remote tts url not supported: %s", audioURL)
	}
	return nil, fmt.Errorf("unsupported tts audio url")
}
