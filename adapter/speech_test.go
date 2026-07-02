package adapter

import (
	"context"
	"testing"
)

func TestSynthesizeSpeechEdgeFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	ctx := context.Background()
	data, err := SynthesizeSpeech(ctx, nil, "测试旁白", "zh-CN-YunyangNeural")
	if err != nil {
		t.Fatalf("SynthesizeSpeech: %v", err)
	}
	if len(data) < 100 {
		t.Fatalf("audio too short: %d bytes", len(data))
	}
}
