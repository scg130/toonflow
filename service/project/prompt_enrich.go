package project

import (
	"strings"

	"toonflow/service/asset"
	cameramap "toonflow/service/internal/camera"
	"toonflow/service/storyboard"
	"toonflow/task"
)

// BuildShotImagePrompt composes the final image-generation prompt for one storyboard shot.
func BuildShotImagePrompt(item task.StoryboardItem, style, videoRatio, assetPrompt, styleAnchor string) string {
	prompt := strings.TrimSpace(item.Prompt)
	if prompt == "" {
		prompt = strings.TrimSpace(item.Description)
	}
	if assetPrompt != "" {
		prompt += ". asset reference: " + SanitizeImagePromptForPolicy(assetPrompt, SanitizeLevelLight)
	}
	if style != "" {
		prompt += ", " + style + " art style"
	}
	if seven := storyboard.FormatSevenElementsPrompt(item); seven != "" {
		prompt += ", " + seven
	} else if cam := EnrichCameraForPrompt(item.Camera); cam != "" {
		prompt += ", " + cam
	}
	if ac := strings.TrimSpace(item.ActionContinue); ac != "" {
		prompt += ", action continuation: " + ac
	}
	if styleAnchor != "" {
		prompt += ", " + styleAnchor
	} else {
		prompt += ", " + strings.Join(asset.StylePromptAnchors(videoRatio, style), ", ")
	}
	prompt += ", " + strings.Join(imageRenderEnhancers(), ", ")
	prompt += ", frame-to-frame continuity, zero model mutation, no random color shift"
	if tags := motionBlurTags(item); len(tags) > 0 {
		prompt += ", " + strings.Join(tags, ", ")
	}
	return SanitizeImagePromptForPolicy(prompt, SanitizeLevelLight)
}

// EnrichCameraForPrompt maps storyboard camera notes to English prompt fragments.
func EnrichCameraForPrompt(camera string) string {
	if motion := cameramap.MapCameraToVideoMotion(camera); motion != "" {
		return motion
	}
	return ""
}

func imageRenderEnhancers() []string {
	// Detail/clarity intensity aligned with short-drama tool settings
	// (face/clothing/lighting high, HD restore on) — concrete tags, not empty "电影感".
	return []string{
		"3D anime cinematic quality",
		"PBR materials ambient occlusion contact shadows",
		"volumetric god rays atmospheric scattering",
		"subsurface scattering skin translucency",
		"metallic reflectivity specular highlights",
		"cinematic rim light global illumination",
		"high clarity facial detail",
		"crisp fabric texture and hair strands",
		"sharp edges no muddy blur",
		"high detail restoration",
	}
}

func motionBlurTags(item task.StoryboardItem) []string {
	text := strings.ToLower(strings.Join([]string{
		item.Description, item.Camera, item.Prompt, item.Scene, item.Motion, item.ShotSize,
	}, " "))
	var tags []string
	if containsAny(text,
		"战斗", "挥", "斩", "冲击", "疾驰", "奔跑", "爆炸", "combat", "fight", "strike", "burst", "sprint", "clash",
		"推镜", "手持", "跟移",
	) {
		tags = append(tags, "strong motion blur on fast movement", "high shutter speed action streaks")
	}
	if containsAny(text,
		"慢镜头", "升格", "slow motion", "slow-mo", "slowmo", "48fps", "60fps", "120fps",
	) {
		tags = append(tags, "slow motion high frame rate capture", "smooth temporal oversampling")
	}
	return tags
}

// ResolutionToVideoRatio maps pixel resolution to aspect ratio label.
func ResolutionToVideoRatio(resolution string) string {
	switch resolution {
	case "720x1280", "1080x1920":
		return "9:16"
	default:
		return "16:9"
	}
}

func containsAny(text string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(text, strings.ToLower(n)) {
			return true
		}
	}
	return false
}
