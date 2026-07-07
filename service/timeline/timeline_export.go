package timeline

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"toonflow/service/internal/ffmpeg"
	"toonflow/service/internal/fsutil"
	"toonflow/task"
)

const defaultExportFPS = 24.0

// continuousCutDur is the near-zero xfade used at a same-scene boundary so a
// mixed timeline still butt-joins continuous shots imperceptibly.
const continuousCutDur = 0.04

// timelineTransitionForShot maps an incoming shot's scene_link + storyboard
// transition style to a timeline boundary transition. Continuous shots produce
// no transition (seamless); scene changes get a visible effect.
func timelineTransitionForShot(sceneLink, transitionStyle string) string {
	if sceneLink == task.SceneLinkContinuous {
		return "none"
	}
	s := strings.ToLower(strings.TrimSpace(transitionStyle))
	switch {
	case strings.Contains(s, "black"), strings.Contains(s, "dip"),
		strings.Contains(s, "闪回"), strings.Contains(s, "淡出"), strings.Contains(s, "淡入淡出"):
		return "dip"
	case strings.Contains(s, "wipe"), strings.Contains(s, "擦"):
		return "wipe"
	case strings.Contains(s, "slide"), strings.Contains(s, "滑"):
		return "slide"
	default:
		// soft dissolve / match cut / unspecified scene change
		return "fade"
	}
}

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
		TransitionDuration: 0.4,
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

// TimelineExportDuration estimates final export length including trims, speed, and transitions.
func TimelineExportDuration(tl *TimelineEdit) float64 {
	if tl == nil {
		return 0
	}
	NormalizeTimelineEdit(tl)
	vt := findTrack(tl, "video")
	if vt == nil || len(vt.Clips) == 0 {
		return 0
	}
	settings := tl.ExportSettings
	var total float64
	for _, clip := range vt.Clips {
		total += clipPlaySeconds(clip, settings)
	}
	transCount := len(vt.Clips) - 1
	if settings != nil && settings.DefaultTransition != "none" && transCount > 0 {
		total -= float64(transCount) * settings.TransitionDuration
	}
	if total < 0 {
		return 0
	}
	return total
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
	dur, err := ffmpeg.ProbeMediaDuration(dest)
	if err != nil {
		return playDur, nil
	}
	return dur, nil
}

func concatTimelineParts(partFiles []string, partDurations []float64, transitions []string, transDur float64, dest string) error {
	if len(partFiles) == 0 {
		return fmt.Errorf("no parts")
	}
	if len(partFiles) == 1 {
		return fsutil.CopyFile(partFiles[0], dest)
	}

	// Group consecutive parts whose boundary is seamless ("none"/"") and hard-concat
	// each group (butt-join). Only real scene-change boundaries become xfade
	// crossfades between groups.
	//
	// Why not a single xfade chain over every part: a near-zero xfade at a continuous
	// cut corrupts the whole chain. ffmpeg's xfade drops its second input when the
	// offset sits within ~1 frame of the previous stream's end (which is exactly what
	// a ~0.04s continuous cut produces), so the first such boundary collapses the
	// accumulated stream and every later offset then exceeds it — the entire export
	// shrinks to the first clip's length. Hard-concatenating continuous runs and only
	// crossfading between multi-second groups keeps every xfade offset well inside its
	// first input.
	workDir := filepath.Dir(dest)
	var groupFiles []string
	var groupDurs []float64
	var groupTrans []string // scene-change transition entering group i (len == len(groups)-1)
	groupStart := 0
	groupIdx := 0

	flush := func(endExclusive int) error {
		members := partFiles[groupStart:endExclusive]
		var sum float64
		for _, d := range partDurations[groupStart:endExclusive] {
			sum += d
		}
		gf := members[0]
		if len(members) > 1 {
			gf = filepath.Join(workDir, fmt.Sprintf("group_%03d.mp4", groupIdx))
			if err := concatMediaFiles(members, gf); err != nil {
				return err
			}
		}
		if probed, err := ffmpeg.ProbeMediaDuration(gf); err == nil && probed > 0 {
			sum = probed
		}
		groupFiles = append(groupFiles, gf)
		groupDurs = append(groupDurs, sum)
		groupIdx++
		return nil
	}

	for i := 1; i < len(partFiles); i++ {
		trans := ""
		if i-1 < len(transitions) {
			trans = transitions[i-1]
		}
		if trans == "" || strings.EqualFold(trans, "none") {
			continue
		}
		if err := flush(i); err != nil {
			return err
		}
		groupTrans = append(groupTrans, trans)
		groupStart = i
	}
	if err := flush(len(partFiles)); err != nil {
		return err
	}

	if len(groupFiles) == 1 {
		return fsutil.CopyFile(groupFiles[0], dest)
	}
	if transDur <= 0 {
		return concatMediaFiles(groupFiles, dest)
	}
	return xfadeGroups(groupFiles, groupDurs, groupTrans, transDur, dest)
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

// xfadeGroups crossfades pre-concatenated, multi-second group clips together. Each
// group is already a seamless butt-join of continuous shots, so every xfade offset
// lands well inside its (long) first input and the chain stays stable.
func xfadeGroups(groups []string, durations []float64, transitions []string, fadeSec float64, dest string) error {
	if len(groups) != len(durations) {
		return fmt.Errorf("groups/durations mismatch")
	}
	args := []string{"-y"}
	for _, g := range groups {
		args = append(args, "-i", g)
	}
	var filters []string
	prev := "[0:v]"
	cumulative := durations[0]
	for i := 1; i < len(groups); i++ {
		name := "fade"
		if i-1 < len(transitions) {
			name = xfadeTransitionName(transitions[i-1])
		}
		// Clamp the crossfade so it never eats more than a group can spare; keeps the
		// offset a safe margin from the previous stream's end.
		effDur := fadeSec
		if maxFade := 0.5 * math.Min(durations[i-1], durations[i]); effDur > maxFade {
			effDur = maxFade
		}
		if effDur < continuousCutDur {
			effDur = continuousCutDur
		}
		offset := cumulative - effDur
		if offset < 0 {
			offset = 0
		}
		outLabel := fmt.Sprintf("[v%02d]", i)
		if i == len(groups)-1 {
			outLabel = "[vout]"
		}
		filters = append(filters, fmt.Sprintf("%s[%d:v]xfade=transition=%s:duration=%.3f:offset=%.3f%s",
			prev, i, name, effDur, offset, outLabel))
		prev = outLabel
		cumulative += durations[i] - effDur
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
		return fsutil.CopyFile(input, dest)
	}
	out, err := exec.Command("ffmpeg", "-y", "-i", input, "-vf", vf, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "copy", dest).CombinedOutput()
	if err != nil {
		return fmt.Errorf("global grade: %s", string(out))
	}
	return nil
}
