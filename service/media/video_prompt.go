package media

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"toonflow/service/internal/camera"
	"toonflow/service/storyboard"
	"toonflow/task"
)

// buildShotVideoPrompt returns motion-focused prompts for image-to-video (not image render tags).
func buildShotVideoPrompt(shot *storyboard.ShotMeta, artStyle, stylePrompt, styleAnchor string, humanSubject bool) (string, string) {
	parts := make([]string, 0, 12)

	if d := strings.TrimSpace(shot.Description); d != "" {
		parts = append(parts, d)
	}
	if seq := formatBeatsForVideoPrompt(shot.Beats, shot.Duration); seq != "" {
		parts = append(parts, seq)
		parts = append(parts, "smooth keyframe interpolation through all timed beats, continuous seamless motion, no hard cuts within shot, uninterrupted fight choreography, fluid character interaction")
	}
	if ac := strings.TrimSpace(shot.ActionContinue); ac != "" {
		parts = append(parts, "action continuation: "+ac)
	}
	lines := storyboard.DialogueLinesForTTS(shot.Dialogue)
	parts = appendDialogueVideoInstructions(parts, lines, humanSubject)
	if cam := camera.MapCameraToVideoMotion(shot.Camera); cam != "" {
		parts = append(parts, cam)
	} else if humanSubject {
		parts = append(parts, "subtle cinematic camera movement with natural character motion")
	} else {
		parts = append(parts, "subtle cinematic camera movement, slow environmental motion, inanimate subject")
	}
	if len(parts) <= 2 {
		if trimmed := trimImagePromptForVideo(shot.Prompt); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	if lit := strings.TrimSpace(shot.Lighting); lit != "" {
		parts = append(parts, "lighting: "+lit)
	}
	if tr := strings.TrimSpace(shot.Transition); tr != "" {
		parts = append(parts, "transition hint: "+tr)
	}

	// Temporal coherence + silent video (dialogue audio comes from TTS compose, not I2V).
	parts = append(parts,
		"silent video no generated speech",
		"no dialogue audio in video",
		"Chinese story visuals only",
		"temporal encoding enabled",
		"keyframe interpolation smooth motion",
		"feature anchoring from first frame",
		"frame-to-frame continuity",
		"zero model mutation",
		"smooth fluid animation",
		"high quality cinematic motion",
	)

	if styleAnchor != "" {
		parts = append(parts, styleAnchor)
	} else if stylePrompt != "" {
		parts = append(parts, stylePrompt)
	} else if artStyle != "" {
		parts = append(parts, artStyle+" animation style")
	}
	if !humanSubject {
		parts = append(parts, "no human character motion, object and environment only")
	}

	negative := strings.Join([]string{
		"static image", "frozen frame", "slideshow", "still photo", "no motion",
		"morphing", "warping", "flickering", "jitter", "stuttering", "low fps",
		"blurry", "out of focus", "low quality", "low resolution",
		"distorted face", "deformed body", "bad anatomy", "extra limbs",
		"watermark", "text overlay", "logo",
		"random color shift", "style drift", "temporal discontinuity",
		"English speech", "English dialogue", "foreign language audio",
		"voiceover", "narration", "spoken words", "talking audio", "subtitle",
		"action freeze mid-motion", "discontinuous movement",
	}, ", ")
	if humanSubject && hasSpeakableLines(lines) {
		negative += ", closed mouth while speaking, static lips during dialogue, no lip sync, mute expression while talking, wrong speaker lip movement"
	}

	return strings.Join(parts, ", "), negative
}

// formatBeatsForVideoPrompt renders an intra-shot timed action plan as an explicit
// time-node instruction so a single generation animates the whole sequence in order,
// e.g. "timed sequence over 12.0s: [0.0s] ...; [4.0s] ...; [8.0s] ...".
func formatBeatsForVideoPrompt(beats []task.ShotBeat, dur float64) string {
	if len(beats) < 2 {
		return ""
	}
	nodes := make([]string, 0, len(beats))
	for _, b := range beats {
		action := strings.TrimSpace(b.Action)
		if action == "" {
			continue
		}
		nodes = append(nodes, fmt.Sprintf("[%.1fs] %s", b.Time, action))
	}
	if len(nodes) < 2 {
		return ""
	}
	header := "timed sequence"
	if dur > 0 {
		header = fmt.Sprintf("timed sequence over %.1fs", dur)
	}
	return header + ": " + strings.Join(nodes, "; ") + "; continuous seamless motion between beats, no cuts"
}

func resolveShotDialogue(shot *storyboard.ShotMeta) []storyboard.ParsedDialogue {
	if shot == nil {
		return nil
	}
	return storyboard.DialogueLinesForTTS(shot.Dialogue)
}

func hasSpeakableLines(lines []storyboard.ParsedDialogue) bool {
	for _, ln := range lines {
		if !ln.Ignorable && strings.TrimSpace(ln.PureText) != "" {
			return true
		}
	}
	return false
}

func appendDialogueVideoInstructions(parts []string, lines []storyboard.ParsedDialogue, humanSubject bool) []string {
	if !humanSubject || !hasSpeakableLines(lines) {
		return parts
	}
	for _, dialogue := range lines {
		if dialogue.Ignorable || strings.TrimSpace(dialogue.PureText) == "" {
			continue
		}
		speaker := strings.TrimSpace(dialogue.Speaker)
		if speaker == "" {
			speaker = "角色"
		}
		line := truncateDialogueForVideoPrompt(dialogue.PureText, 80)
		parts = append(parts,
			fmt.Sprintf("角色%s口型与表情表演，中文台词：%s", speaker, line),
			fmt.Sprintf("%s说话时自然唇形与下颌运动，动作连贯不中断", speaker),
		)
	}
	parts = append(parts,
		"仅口型与肢体表演，视频中禁止生成任何语音或旁白",
		"无声画面配合口型运动，不要英文对白音频",
	)
	return parts
}

func truncateDialogueForVideoPrompt(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" || maxRunes <= 0 {
		return text
	}
	if utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	return string([]rune(text)[:maxRunes]) + "…"
}

func trimImagePromptForVideo(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	if utf8.RuneCountInString(prompt) > 200 {
		prompt = string([]rune(prompt)[:200])
	}
	stripTerms := []string{
		"unreal engine", "octane render", "ambient occlusion", "subsurface scattering",
		"pbr", "8k", "global illumination", "volumetric", "bokeh", "character_id",
		"style: consistent", "high fidelity", "consistent lighting", "consistent character",
		"widescreen", "vertical", "unified color",
	}
	segments := strings.Split(prompt, ",")
	kept := make([]string, 0, 4)
	for _, seg := range segments {
		s := strings.TrimSpace(seg)
		if s == "" {
			continue
		}
		lower := strings.ToLower(s)
		skip := false
		for _, t := range stripTerms {
			if strings.Contains(lower, t) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		kept = append(kept, s)
		if len(kept) >= 3 {
			break
		}
	}
	return strings.Join(kept, ", ")
}
