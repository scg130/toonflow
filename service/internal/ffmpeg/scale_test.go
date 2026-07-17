package ffmpeg

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNormalizeVideoDimensions_upscales(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not available")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mp4")
	// Tiny 448x832-like vertical clip.
	cmd := exec.Command(
		"ffmpeg", "-y", "-f", "lavfi", "-i", "color=c=black:s=448x832:d=0.5",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", src,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create fixture: %v: %s", err, out)
	}
	in, err := NormalizeVideoDimensions(src, 720, 1280)
	if err != nil {
		t.Fatal(err)
	}
	if in.Width != 448 || in.Height != 832 {
		t.Fatalf("input dims %#v", in)
	}
	out, err := ProbeMediaDimensions(src)
	if err != nil {
		t.Fatal(err)
	}
	if out.Width != 720 || out.Height != 1280 {
		t.Fatalf("normalized %#v want 720x1280", out)
	}
	st, err := os.Stat(src)
	if err != nil || st.Size() == 0 {
		t.Fatal("normalized file missing")
	}
}
