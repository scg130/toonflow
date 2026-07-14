package media

import (
	"strings"

	"toonflow/service/storyboard"
	"toonflow/task"
)

// VideoMode selects how existing Agnes keyframe I2V is driven (method only — same vendor/models).
// Inspired by industrial short-drama pipelines: lock stills first, then pick frame strategy.
type VideoMode string

const (
	// VideoModeFrames2: first + last keyframe interpolation (dialogue / setup / emotion hold).
	VideoModeFrames2 VideoMode = "frames2"
	// VideoModeMultiframe: 2–3 keyframes one continuous take (conflict / twist / action).
	VideoModeMultiframe VideoMode = "multiframe"
)

// ClassifyShotVideoMode picks frames2 vs multiframe from shot content (no external models).
func ClassifyShotVideoMode(shot *storyboard.ShotMeta) VideoMode {
	if shot == nil {
		return VideoModeFrames2
	}
	n := len(shot.Beats)
	if n >= 3 {
		return VideoModeMultiframe
	}
	blob := strings.ToLower(shot.Description + " " + shot.Camera + " " + shot.ActionContinue)
	for _, b := range shot.Beats {
		blob += " " + strings.ToLower(b.Action)
	}
	conflictHints := videoI2VLines("conflict_hints", []string{
		"冲突", "打脸", "反转", "撕", "砸", "跪下", "冲", "打", "爆", "围攻",
		"对峙", "追", "杀", "怒吼", "一拳", "战斗", "打斗", "高潮",
		"push-in", "handheld", "dolly zoom", "slow-motion", "慢放", "手持", "急速",
	})
	for _, h := range conflictHints {
		if strings.Contains(blob, strings.ToLower(h)) {
			return VideoModeMultiframe
		}
	}
	if n <= 2 {
		return VideoModeFrames2
	}
	return VideoModeMultiframe
}

// SelectKeyframesForMode reduces CDN keyframe URLs for the chosen Agnes strategy.
func SelectKeyframesForMode(urls []string, mode VideoMode) []string {
	if len(urls) == 0 {
		return nil
	}
	if len(urls) == 1 {
		return []string{urls[0], urls[0]}
	}
	switch mode {
	case VideoModeFrames2:
		// First + last only — lock start/end composition like frames2video.
		return []string{urls[0], urls[len(urls)-1]}
	default:
		return SelectEvenKeyframeURLs(urls, 3)
	}
}

// BeatActionsForMode returns beat actions aligned with selected keyframe slots.
func BeatActionsForMode(beats []task.ShotBeat, mode VideoMode) []task.ShotBeat {
	if len(beats) == 0 {
		return nil
	}
	if mode == VideoModeFrames2 && len(beats) >= 2 {
		return []task.ShotBeat{beats[0], beats[len(beats)-1]}
	}
	if len(beats) > 3 {
		return SelectEvenBeats(beats, 3)
	}
	return beats
}

// SelectEvenBeats picks up to n beats evenly by index.
func SelectEvenBeats(beats []task.ShotBeat, n int) []task.ShotBeat {
	if n <= 0 || len(beats) == 0 {
		return nil
	}
	if len(beats) <= n {
		return beats
	}
	out := make([]task.ShotBeat, 0, n)
	for i := 0; i < n; i++ {
		idx := int(float64(i)*float64(len(beats)-1)/float64(n-1) + 0.5)
		out = append(out, beats[idx])
	}
	return out
}
