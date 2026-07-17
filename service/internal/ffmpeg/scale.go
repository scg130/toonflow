package ffmpeg

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// MediaDimensions holds probed video/image frame size.
type MediaDimensions struct {
	Width  int
	Height int
}

// ProbeMediaDimensions returns the first video stream width/height via ffprobe.
func ProbeMediaDimensions(path string) (MediaDimensions, error) {
	out, err := exec.Command(
		"ffprobe", "-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=p=0:s=x",
		path,
	).Output()
	if err != nil {
		return MediaDimensions{}, err
	}
	var w, h int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%dx%d", &w, &h); err != nil {
		return MediaDimensions{}, fmt.Errorf("parse dimensions %q: %w", strings.TrimSpace(string(out)), err)
	}
	if w <= 0 || h <= 0 {
		return MediaDimensions{}, fmt.Errorf("invalid dimensions %dx%d", w, h)
	}
	return MediaDimensions{Width: w, Height: h}, nil
}

// NormalizeVideoDimensions scales/pads src to width×height with Lanczos + light unsharp.
// If already matching, returns nil without rewriting. Writes in-place via temp file.
func NormalizeVideoDimensions(src string, width, height int) (MediaDimensions, error) {
	if width <= 0 || height <= 0 {
		return MediaDimensions{}, fmt.Errorf("invalid target size %dx%d", width, height)
	}
	in, err := ProbeMediaDimensions(src)
	if err != nil {
		return MediaDimensions{}, err
	}
	if in.Width == width && in.Height == height {
		return in, nil
	}
	tmp := src + ".norm.mp4"
	vf := fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=decrease:flags=lanczos,"+
			"pad=%d:%d:(ow-iw)/2:(oh-ih)/2,"+
			"unsharp=3:3:0.4:3:3:0.0",
		width, height, width, height,
	)
	cmd := exec.Command(
		"ffmpeg", "-y", "-i", src,
		"-vf", vf,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "18",
		"-pix_fmt", "yuv420p",
		"-an",
		tmp,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(tmp)
		return in, fmt.Errorf("normalize video: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if err := os.Rename(tmp, src); err != nil {
		_ = os.Remove(tmp)
		return in, err
	}
	return in, nil
}
