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
