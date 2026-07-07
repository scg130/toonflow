package media

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"toonflow/service/internal/camera"
	"toonflow/service/storyboard"
)

// buildShotVideoPrompt returns motion-focused prompts for image-to-video (not image render tags).
func buildShotVideoPrompt(shot *storyboard.ShotMeta, artStyle, stylePrompt, styleAnchor string, humanSubject bool) (string, string) {
	parts := make([]string, 0, 12)

	if d := strings.TrimSpace(shot.Description); d != "" {
		parts = append(parts, d)
	}
	if ac := strings.TrimSpace(shot.ActionContinue); ac != "" {
		parts = append(parts, "action continuation: "+ac)
	}
	dialogue := resolveShotDialogue(shot)
	parts = appendDialogueVideoInstructions(parts, dialogue, humanSubject)
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

	// Temporal coherence encoding (toonflow.doc §4.2 / §4.3)
	parts = append(parts,
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
	}, ", ")
	if humanSubject && !dialogue.Ignorable && dialogue.PureText != "" {
		negative += ", closed mouth while speaking, static lips during dialogue, no lip sync, mute expression while talking, wrong speaker lip movement"
	}

	return strings.Join(parts, ", "), negative
}

func resolveShotDialogue(shot *storyboard.ShotMeta) ParsedDialogue {
	if shot == nil {
		return ParsedDialogue{Ignorable: true}
	}
	dialogue := strings.TrimSpace(shot.Dialogue)
	if dialogue == "" {
		dialogue = storyboard.ExtractDialogueFromDescription(shot.Description)
	}
	return ParseDialogueForTTS(dialogue)
}

func appendDialogueVideoInstructions(parts []string, dialogue ParsedDialogue, humanSubject bool) []string {
	if !humanSubject || dialogue.Ignorable || strings.TrimSpace(dialogue.PureText) == "" {
		return parts
	}
	speaker := strings.TrimSpace(dialogue.Speaker)
	if speaker == "" {
		speaker = "character"
	}
	line := truncateDialogueForVideoPrompt(dialogue.PureText, 80)
	parts = append(parts,
		fmt.Sprintf("dialogue performance: %s speaking \"%s\"", speaker, line),
		fmt.Sprintf("character %s performs matching body language and facial acting while speaking", speaker),
		fmt.Sprintf("visible lip sync and mouth movement for %s aligned with the spoken line", speaker),
		"natural jaw and lip motion synchronized with dialogue delivery",
		"expressive speaking gestures, eye contact and emotional acting tied to the line",
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
