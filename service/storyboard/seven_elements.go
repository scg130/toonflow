package storyboard

import (
	"strings"

	"toonflow/task"
)

// SyncSevenElements fills missing 分镜七要素 and keeps camera as a readable composite
// of 景别+角度+动势 for legacy image/video prompt paths.
func SyncSevenElements(it *task.StoryboardItem) {
	if it == nil {
		return
	}
	it.ShotSize = strings.TrimSpace(it.ShotSize)
	it.Angle = strings.TrimSpace(it.Angle)
	it.Composition = strings.TrimSpace(it.Composition)
	it.Lighting = strings.TrimSpace(it.Lighting)
	it.ColorTone = strings.TrimSpace(it.ColorTone)
	it.Motion = strings.TrimSpace(it.Motion)
	it.Transition = strings.TrimSpace(it.Transition)
	it.Camera = strings.TrimSpace(it.Camera)

	// Defaults when model omitted structured fields.
	if it.ShotSize == "" {
		it.ShotSize = inferShotSize(it.Camera + " " + it.Description)
	}
	if it.Angle == "" {
		it.Angle = inferAngle(it.Camera)
	}
	if it.Composition == "" {
		it.Composition = "主体中心偏三分，竖直 9:16 安全区"
	}
	if it.Lighting == "" {
		it.Lighting = "侧光主光 + 淡辅光，面部有明暗"
	}
	if it.ColorTone == "" {
		it.ColorTone = "竖屏短剧高对比，主色纯净不浑浊"
	}
	if it.Motion == "" {
		it.Motion = inferMotion(it.Camera)
	}
	if it.Transition == "" {
		it.Transition = "soft dissolve"
	}

	// Always refresh camera composite from seven elements when any structured field set.
	if comp := FormatCameraComposite(*it); comp != "" {
		it.Camera = comp
	} else if it.Camera == "" {
		it.Camera = "中景平视定镜"
	}
}

// FormatCameraComposite builds "景别 + 角度 + 动势" for camera field / I2V.
func FormatCameraComposite(it task.StoryboardItem) string {
	parts := make([]string, 0, 3)
	for _, p := range []string{it.ShotSize, it.Angle, it.Motion} {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " ")
}

// FormatSevenElementsPrompt returns a compact visual-language block for image/video prompts.
func FormatSevenElementsPrompt(it task.StoryboardItem) string {
	pairs := []struct{ k, v string }{
		{"shot size", it.ShotSize},
		{"camera angle", it.Angle},
		{"composition", it.Composition},
		{"lighting", it.Lighting},
		{"color tone", it.ColorTone},
		{"motion", it.Motion},
	}
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		v := strings.TrimSpace(p.v)
		if v == "" {
			continue
		}
		parts = append(parts, p.k+": "+v)
	}
	return strings.Join(parts, "; ")
}

func inferShotSize(blob string) string {
	b := strings.ToLower(blob)
	switch {
	case strings.Contains(b, "极特写") || strings.Contains(b, "extreme close"):
		return "极特写"
	case strings.Contains(b, "特写") || strings.Contains(b, "close-up") || strings.Contains(b, "close up"):
		return "特写"
	case strings.Contains(b, "近景") || strings.Contains(b, "medium close"):
		return "近景"
	case strings.Contains(b, "全景") || strings.Contains(b, "wide"):
		return "全景"
	case strings.Contains(b, "远景") || strings.Contains(b, "long shot"):
		return "远景"
	default:
		return "中景"
	}
}

func inferAngle(camera string) string {
	c := strings.ToLower(camera)
	switch {
	case strings.Contains(c, "仰") || strings.Contains(c, "low angle"):
		return "仰拍"
	case strings.Contains(c, "俯") || strings.Contains(c, "high angle"):
		return "俯拍"
	case strings.Contains(c, "荷兰") || strings.Contains(c, "dutch"):
		return "荷兰角"
	case strings.Contains(c, "斜"):
		return "斜角"
	default:
		return "平视"
	}
}

func inferMotion(camera string) string {
	c := strings.ToLower(camera)
	switch {
	case strings.Contains(c, "推") || strings.Contains(c, "push") || strings.Contains(c, "dolly in"):
		return "缓慢推镜靠近主体"
	case strings.Contains(c, "拉") || strings.Contains(c, "pull") || strings.Contains(c, "dolly out"):
		return "拉镜Reveal空间"
	case strings.Contains(c, "摇") || strings.Contains(c, "pan"):
		return "短促横摇"
	case strings.Contains(c, "移") || strings.Contains(c, "track"):
		return "跟移，主体保持框中"
	case strings.Contains(c, "手持") || strings.Contains(c, "handheld"):
		return "手持微抖，主体前移时加剧"
	case strings.Contains(c, "环绕") || strings.Contains(c, "orbit"):
		return "轻度环绕，脸保持居中"
	case strings.Contains(c, "固定") || strings.Contains(c, "static") || strings.Contains(c, "定"):
		return "机位固定，仅主体肢体位移"
	default:
		return "机位稳定，主体有明确位移"
	}
}
