package adapter

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	edge_tts "github.com/bytectlgo/edge-tts/pkg/edge_tts"
)

// EdgeTTSSynthesize converts text to MP3 using Microsoft Edge online TTS (no API key).
func EdgeTTSSynthesize(ctx context.Context, text, voice string) ([]byte, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("empty tts text")
	}
	if voice == "" {
		voice = "zh-CN-YunyangNeural"
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
	}

	comm := edge_tts.NewCommunicate(text, voice,
		edge_tts.WithRate("+0%"),
		edge_tts.WithVolume("+0%"),
		edge_tts.WithPitch("+0Hz"),
	)

	ch, err := comm.Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("edge tts: %w", err)
	}

	var buf bytes.Buffer
	for chunk := range ch {
		switch chunk.Type {
		case "audio":
			buf.Write(chunk.Data)
		case "error":
			if len(chunk.Data) > 0 {
				return nil, fmt.Errorf("edge tts: %s", string(chunk.Data))
			}
			return nil, fmt.Errorf("edge tts stream error")
		}
	}
	if buf.Len() == 0 {
		return nil, fmt.Errorf("edge tts returned empty audio")
	}
	return buf.Bytes(), nil
}
