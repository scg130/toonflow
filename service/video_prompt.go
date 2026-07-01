package service

import (
	"strings"
	"unicode/utf8"
)

// buildShotVideoPrompt returns motion-focused prompts for image-to-video (not image render tags).
func buildShotVideoPrompt(shot *storyboardShot, artStyle, stylePrompt string) (string, string) {
	parts := make([]string, 0, 8)

	if d := strings.TrimSpace(shot.Description); d != "" {
		parts = append(parts, d)
	}
	if cam := mapCameraToVideoMotion(shot.Camera); cam != "" {
		parts = append(parts, cam)
	} else {
		parts = append(parts, "subtle cinematic camera movement with natural character motion")
	}
	if len(parts) == 1 {
		if trimmed := trimImagePromptForVideo(shot.Prompt); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	parts = append(parts, "smooth fluid animation", "temporal consistency", "high quality cinematic motion")

	if stylePrompt != "" {
		parts = append(parts, stylePrompt)
	} else if artStyle != "" {
		parts = append(parts, artStyle+" animation style")
	}

	negative := strings.Join([]string{
		"static image", "frozen frame", "slideshow", "still photo", "no motion",
		"morphing", "warping", "flickering", "jitter", "stuttering", "low fps",
		"blurry", "out of focus", "low quality", "low resolution",
		"distorted face", "deformed body", "bad anatomy", "extra limbs",
		"watermark", "text overlay", "logo",
	}, ", ")

	return strings.Join(parts, ", "), negative
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
