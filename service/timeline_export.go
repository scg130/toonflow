package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const defaultExportFPS = 24.0

// NormalizeTimelineEdit fills defaults for export settings and clip fields.
func NormalizeTimelineEdit(tl *TimelineEdit) {
	if tl == nil {
		return
	}
	if tl.ExportSettings == nil {
		tl.ExportSettings = DefaultExportSettings()
	} else {
		mergeExportSettingsDefaults(tl.ExportSettings)
	}
	for ti := range tl.Tracks {
		for ci := range tl.Tracks[ti].Clips {
			normalizeClipFields(&tl.Tracks[ti].Clips[ci])
		}
	}
}

func DefaultExportSettings() *TimelineExportSettings {
	return &TimelineExportSettings{
		DefaultTransition:  "fade",
		TransitionDuration: 0.15,
		TrimHeadFrames:     2,
		TrimTailFrames:     2,
		GlobalBrightness:   0,
		GlobalContrast:     1,
		GlobalSaturation:   1,
	}
}

func mergeExportSettingsDefaults(s *TimelineExportSettings) {
	d := DefaultExportSettings()
	if s.DefaultTransition == "" {
		s.DefaultTransition = d.DefaultTransition
	}
	if s.TransitionDuration <= 0 {
		s.TransitionDuration = d.TransitionDuration
	}
	if s.TrimHeadFrames <= 0 {
		s.TrimHeadFrames = d.TrimHeadFrames
	}
	if s.TrimTailFrames <= 0 {
		s.TrimTailFrames = d.TrimTailFrames
	}
	if s.GlobalContrast <= 0 {
		s.GlobalContrast = d.GlobalContrast
	}
	if s.GlobalSaturation <= 0 {
		s.GlobalSaturation = d.GlobalSaturation
	}
}

func normalizeClipFields(c *TimelineClip) {
	if c.Speed <= 0 {
		c.Speed = 1
	}
	if c.Volume <= 0 {
		c.Volume = 1
	}
	if c.Contrast <= 0 {
		c.Contrast = 1
	}
	if c.Saturation <= 0 {
		c.Saturation = 1
	}
}

func clipTrimRange(clip TimelineClip, settings *TimelineExportSettings) (start, end float64) {
	start = clip.Start
	end = clip.End
	if end <= 0 {
		end = clip.Duration
	}
	if settings != nil {
		if settings.TrimHeadFrames > 0 {
			start += float64(settings.TrimHeadFrames) / defaultExportFPS
		}
		if settings.TrimTailFrames > 0 {
			end -= float64(settings.TrimTailFrames) / defaultExportFPS
		}
	}
	if end <= start {
		end = start + 0.1
	}
	return start, end
}

func clipPlaySeconds(clip TimelineClip, settings *TimelineExportSettings) float64 {
	start, end := clipTrimRange(clip, settings)
	dur := end - start
	if clip.Speed > 0 && clip.Speed != 1 {
		dur /= clip.Speed
	}
	if dur < 0.05 {
		return 0.05
	}
	return dur
}

func effectiveTransitionAfter(clip TimelineClip, settings *TimelineExportSettings) string {
	if strings.EqualFold(clip.Transition, "none") {
		return "none"
	}
	if clip.Transition != "" {
		return clip.Transition
	}
	if settings != nil && settings.DefaultTransition != "" {
		return settings.DefaultTransition
	}
	return "fade"
}

func xfadeTransitionName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "dip", "fadeblack", "black":
		return "fadeblack"
	case "wipe", "wipeleft":
		return "wipeleft"
	case "wiperight":
		return "wiperight"
	case "slide", "slideleft":
		return "slideleft"
	case "fade", "":
		return "fade"
	default:
		return "fade"
	}
}

func buildClipVideoFilter(clip TimelineClip, playDur float64) string {
	var parts []string
	if clip.Speed > 0 && clip.Speed != 1 {
		parts = append(parts, fmt.Sprintf("setpts=PTS/%.4f", clip.Speed))
	}
	b := clip.Brightness
	c := clip.Contrast
	s := clip.Saturation
	if c <= 0 {
		c = 1
	}
	if s <= 0 {
		s = 1
	}
	if b != 0 || c != 1 || s != 1 {
		parts = append(parts, fmt.Sprintf("eq=brightness=%.3f:contrast=%.3f:saturation=%.3f", b, c, s))
	}
	if clip.FadeIn > 0 {
		parts = append(parts, fmt.Sprintf("fade=t=in:st=0:d=%.3f", clip.FadeIn))
	}
	if clip.FadeOut > 0 && playDur > clip.FadeOut {
		parts = append(parts, fmt.Sprintf("fade=t=out:st=%.3f:d=%.3f", playDur-clip.FadeOut, clip.FadeOut))
	}
	return strings.Join(parts, ",")
}

func buildGlobalVideoFilter(settings *TimelineExportSettings) string {
	if settings == nil {
		return ""
	}
	b := settings.GlobalBrightness
	c := settings.GlobalContrast
	s := settings.GlobalSaturation
	if c <= 0 {
		c = 1
	}
	if s <= 0 {
		s = 1
	}
	if b == 0 && c == 1 && s == 1 {
		return ""
	}
	return fmt.Sprintf("eq=brightness=%.3f:contrast=%.3f:saturation=%.3f", b, c, s)
}

func renderTimelinePart(local, dest string, clip TimelineClip, settings *TimelineExportSettings) (float64, error) {
	start, end := clipTrimRange(clip, settings)
	playDur := end - start
	if clip.Speed > 0 && clip.Speed != 1 {
		playDur /= clip.Speed
	}
	vf := buildClipVideoFilter(clip, playDur)
	args := []string{"-y", "-ss", fmt.Sprintf("%.3f", start), "-to", fmt.Sprintf("%.3f", end), "-i", local}
	if vf != "" {
		args = append(args, "-vf", vf)
	}
	args = append(args, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-an", dest)
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		return 0, fmt.Errorf("trim clip: %s", string(out))
	}
	dur, err := probeMediaDuration(dest)
	if err != nil {
		return playDur, nil
	}
	return dur, nil
}

func probeMediaDuration(path string) (float64, error) {
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

func concatTimelineParts(partFiles []string, partDurations []float64, transitions []string, transDur float64, dest string) error {
	if len(partFiles) == 0 {
		return fmt.Errorf("no parts")
	}
	if len(partFiles) == 1 {
		return copyFile(partFiles[0], dest)
	}
	useXfade := false
	for _, t := range transitions {
		if t != "" && !strings.EqualFold(t, "none") {
			useXfade = true
			break
		}
	}
	if !useXfade || transDur <= 0 {
		return concatMediaFiles(partFiles, dest)
	}
	return concatWithXfade(partFiles, partDurations, transitions, transDur, dest)
}

func concatMediaFiles(parts []string, dest string) error {
	listPath := dest + ".txt"
	f, err := os.Create(listPath)
	if err != nil {
		return err
	}
	for _, p := range parts {
		abs, _ := filepath.Abs(p)
		fmt.Fprintf(f, "file '%s'\n", strings.ReplaceAll(abs, "'", `'\''`))
	}
	f.Close()
	defer os.Remove(listPath)
	out, err := exec.Command("ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i", listPath,
		"-c:v", "libx264", "-pix_fmt", "yuv420p", dest).CombinedOutput()
	if err != nil {
		return fmt.Errorf("concat: %s", string(out))
	}
	return nil
}

func concatWithXfade(parts []string, durations []float64, transitions []string, fadeSec float64, dest string) error {
	if len(parts) != len(durations) {
		return fmt.Errorf("parts/durations mismatch")
	}
	args := []string{"-y"}
	for _, p := range parts {
		args = append(args, "-i", p)
	}
	var filters []string
	prev := "[0:v]"
	cumulative := durations[0]
	for i := 1; i < len(parts); i++ {
		trans := "fade"
		if i-1 < len(transitions) {
			trans = transitions[i-1]
		}
		if strings.EqualFold(trans, "none") {
			// fall back to hard concat for remaining — simplify: use fade with shorter dur
			trans = "fade"
		}
		offset := cumulative - fadeSec
		if offset < 0 {
			offset = 0
		}
		outLabel := fmt.Sprintf("[v%02d]", i)
		if i == len(parts)-1 {
			outLabel = "[vout]"
		}
		filters = append(filters, fmt.Sprintf("%s[%d:v]xfade=transition=%s:duration=%.3f:offset=%.3f%s",
			prev, i, xfadeTransitionName(trans), fadeSec, offset, outLabel))
		prev = outLabel
		cumulative += durations[i] - fadeSec
	}
	args = append(args, "-filter_complex", strings.Join(filters, ";"), "-map", "[vout]",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", dest)
	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("xfade: %s", string(out))
	}
	return nil
}

func applyGlobalGrade(input, dest string, settings *TimelineExportSettings) error {
	vf := buildGlobalVideoFilter(settings)
	if vf == "" {
		return copyFile(input, dest)
	}
	out, err := exec.Command("ffmpeg", "-y", "-i", input, "-vf", vf, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "copy", dest).CombinedOutput()
	if err != nil {
		return fmt.Errorf("global grade: %s", string(out))
	}
	return nil
}
