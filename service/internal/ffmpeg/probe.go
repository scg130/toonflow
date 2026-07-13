package ffmpeg

import (
	"fmt"
	"os/exec"
	"strings"
)

// ProbeMediaDuration returns media duration in seconds via ffprobe.
func ProbeMediaDuration(path string) (float64, error) {
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path).Output()
	if err != nil {
		return 0, err
	}
	var d float64
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%f", &d); err != nil {
		return 0, err
	}
	return d, nil
}

// StripAudio remuxes video without an audio track (I2V often injects unwanted speech).
func StripAudio(src, dest string) error {
	cmd := exec.Command("ffmpeg", "-y", "-i", src, "-c:v", "copy", "-an", dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("strip audio: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
