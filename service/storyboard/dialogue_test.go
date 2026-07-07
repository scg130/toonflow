package storyboard

import (
	"strings"
	"testing"
)

func TestExtractDialogueFromDescription(t *testing.T) {
	desc := "极低角度仰拍，石昊：这一战，我不会退。"
	got := ExtractDialogueFromDescription(desc)
	if got == "" || !strings.Contains(got, "石昊") {
		t.Fatalf("expected inline dialogue, got %q", got)
	}
}
