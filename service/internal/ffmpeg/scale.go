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

// NormalizeVideoDimensions scales/pads src to width×height and applies a light
// "HD restore" pass (denoise → Lanczos → unsharp → mild contrast), inspired by
// short-drama toolchains that turn on clarity + high-detail restoration after I2V.
// Always rewrites when size mismatches; when size already matches, still runs the
// clarity filter so soft Agnes output gains edge definition.
// Writes in-place via temp file.
func NormalizeVideoDimensions(src string, width, height int) (MediaDimensions, error) {
	if width <= 0 || height <= 0 {
		return MediaDimensions{}, fmt.Errorf("invalid target size %dx%d", width, height)
	}
	in, err := ProbeMediaDimensions(src)
	if err != nil {
		return MediaDimensions{}, err
	}

	tmp := src + ".norm.mp4"
	needScale := in.Width != width || in.Height != height
	vf := clarityFilter(width, height, needScale)

	cmd := exec.Command(
		"ffmpeg", "-y", "-i", src,
		"-vf", vf,
		"-c:v", "libx264", "-preset", "medium", "-crf", "17",
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

// clarityFilter builds the post-I2V restore chain:
// optional scale/pad → mild denoise → Lanczos (when scaling) → unsharp → mild eq.
func clarityFilter(width, height int, needScale bool) string {
	parts := make([]string, 0, 6)
	if needScale {
		parts = append(parts,
			fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease:flags=lanczos", width, height),
			fmt.Sprintf("pad=%d:%d:(ow-iw)/2:(oh-ih)/2", width, height),
		)
	}
	// Mild temporal/spatial denoise before sharpen — reduces mushy upscale halos.
	parts = append(parts, "hqdn3d=1.0:1.0:2.5:2.5")
	// Clarity ~0.8 equivalent: stronger luma unsharp, no chroma sharpen.
	parts = append(parts, "unsharp=5:5:0.75:5:5:0.0")
	// Punch local contrast / saturation slightly (cool short-drama look).
	parts = append(parts, "eq=contrast=1.05:saturation=1.06:brightness=0.01")
	return strings.Join(parts, ",")
}
