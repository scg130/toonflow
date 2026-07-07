package ffmpeg

import (
	"fmt"
	"math"
	"os/exec"
	"strings"
)

// BuildAtempoChain returns comma-separated atempo filters whose product equals ratio.
// ratio > 1 speeds up; ratio < 1 slows down.
func BuildAtempoChain(ratio float64) string {
	const eps = 0.01
	var parts []string
	f := ratio
	for f > 2.0+eps {
		parts = append(parts, "atempo=2.0")
		f /= 2.0
	}
	for f < 0.5-eps {
		parts = append(parts, "atempo=0.5")
		f /= 0.5
	}
	if math.Abs(f-1.0) > eps {
		parts = append(parts, fmt.Sprintf("atempo=%.6f", f))
	}
	return strings.Join(parts, ",")
}

// StretchAudioToDuration adjusts audio length to targetSec without truncating speech.
func StretchAudioToDuration(inputPath, outputPath string, targetSec float64) error {
	if targetSec <= 0 {
		return fmt.Errorf("invalid target duration")
	}
	srcDur, err := ProbeMediaDuration(inputPath)
	if err != nil || srcDur <= 0 {
		return fmt.Errorf("probe audio: %w", err)
	}
	const eps = 0.05
	if math.Abs(srcDur-targetSec) < eps {
		args := []string{"-y", "-i", inputPath, "-c:a", "libmp3lame", "-q:a", "4", outputPath}
		out, err := exec.Command("ffmpeg", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("ffmpeg copy audio: %s", string(out))
		}
		return nil
	}
	durStr := fmt.Sprintf("%.3f", targetSec)
	var filter string
	if srcDur > targetSec {
		ratio := srcDur / targetSec
		chain := BuildAtempoChain(ratio)
		if chain == "" {
			filter = fmt.Sprintf("[0:a]apad=whole_dur=%s[aout]", durStr)
		} else {
			filter = fmt.Sprintf("[0:a]%s,asetpts=PTS-STARTPTS,apad=whole_dur=%s[aout]", chain, durStr)
		}
	} else {
		filter = fmt.Sprintf("[0:a]asetpts=PTS-STARTPTS,apad=whole_dur=%s[aout]", durStr)
	}
	args := []string{"-y", "-i", inputPath, "-filter_complex", filter, "-map", "[aout]",
		"-c:a", "libmp3lame", "-q:a", "4", outputPath}
	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg stretch audio: %s", string(out))
	}
	return nil
}
