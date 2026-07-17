package project

import (
	"fmt"
	"regexp"
	"strings"

	"toonflow/service/asset"
	"toonflow/task"
)

const (
	SanitizeLevelLight  = 0
	SanitizeLevelStrict = 1
)

var (
	reChinese       = regexp.MustCompile(`[\p{Han}]+`)
	reMultiSpace    = regexp.MustCompile(`\s{2,}`)
	rePunctJunk     = regexp.MustCompile(`[\[\]「」|]+`)
	reEmptySeg      = regexp.MustCompile(`(,\s*){2,}`)
	reAssetMarker   = regexp.MustCompile(`(?i)(asset\s+consistency\s*:|asset\s+reference\s*:|角色「|character_id\s*:)`)
	reWordBlood    = regexp.MustCompile(`(?i)\bblood[-\s]?stained\b|\bblood\b|\bbloody\b|\bgore\b|\bgory\b`)
	reWordViolence = regexp.MustCompile(`(?i)\b(corpse|nude|naked|nsfw|torture|massacre|rape|disembowel(?:ed|ment)?|decapitat(?:e|ed|ion)?)\b`)
	reWordStrictEng = regexp.MustCompile(`(?i)\b(gore|bloody|blood|nude|naked|corpse|torture|massacre)\b`)
)

// IsContentPolicyViolation reports Agnes/OpenAI-style content policy errors.
func IsContentPolicyViolation(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "content_policy_violation") ||
		strings.Contains(s, "unable to generate this content") ||
		strings.Contains(s, "please modify your prompt") ||
		strings.Contains(s, "content policy")
}

// UserFacingImagePolicyMessage returns a concise hint for UI when policy blocks generation.
func UserFacingImagePolicyMessage(shotNumber int) string {
	return fmt.Sprintf(
		"第 %d 镜被内容安全策略拦截，请编辑分镜描述/prompt，避免血腥、裸露、残忍伤害等描写后重新生图",
		shotNumber,
	)
}

// SanitizeImagePromptForPolicy softens wording that commonly triggers image API policy filters.
func SanitizeImagePromptForPolicy(prompt string, level int) string {
	s := strings.TrimSpace(prompt)
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, "资产约束:", "asset reference:")
	s = lightPolicyReplacer.Replace(s)
	s = reWordBlood.ReplaceAllStringFunc(s, func(m string) string {
		lower := strings.ToLower(m)
		switch {
		case strings.Contains(lower, "stained"):
			return "weathered"
		case strings.Contains(lower, "bloody"):
			return "dramatic"
		case strings.Contains(lower, "gore"), strings.Contains(lower, "gory"):
			return "energy effects"
		default:
			return "crimson light"
		}
	})
	s = reWordViolence.ReplaceAllStringFunc(s, func(m string) string {
		lower := strings.ToLower(m)
		switch {
		case lower == "corpse":
			return "fallen figure"
		case lower == "nude", lower == "naked":
			return "fully clothed"
		case lower == "torture":
			return "tension"
		case lower == "massacre":
			return "epic confrontation"
		case strings.HasPrefix(lower, "disembowel"):
			return "clash"
		case strings.HasPrefix(lower, "decapitat"):
			return "sword swing"
		default:
			return ""
		}
	})
	if level >= SanitizeLevelStrict {
		s = reWordStrictEng.ReplaceAllStringFunc(s, softStrictEnglish)
		s = reChinese.ReplaceAllString(s, " ")
		s = rePunctJunk.ReplaceAllString(s, " ")
	}
	s = collapseSpaces(s)
	s = reEmptySeg.ReplaceAllString(s, ", ")
	return strings.Trim(s, ", ")
}

func softStrictEnglish(m string) string {
	switch strings.ToLower(m) {
	case "gore", "gory":
		return "energy effects"
	case "blood", "bloody":
		return "crimson light"
	case "nude", "naked":
		return "fully clothed"
	case "corpse":
		return "fallen figure"
	case "torture":
		return "tension"
	case "massacre":
		return "epic confrontation"
	default:
		return m
	}
}

// ExtractVisualActionCore keeps the camera/action head of a prompt and drops
// long asset-consistency blocks that often embed risky character lore.
func ExtractVisualActionCore(prompt string) string {
	s := strings.TrimSpace(prompt)
	if s == "" {
		return s
	}
	if loc := reAssetMarker.FindStringIndex(s); loc != nil && loc[0] > 0 {
		s = strings.TrimSpace(s[:loc[0]])
		s = strings.Trim(s, ",;| ")
	}
	return s
}

// BuildSafeImagePromptFallback builds a last-resort safe prompt from the original
// visual action core so one-click image gen can still produce a usable frame.
func BuildSafeImagePromptFallback(original string) string {
	core := ExtractVisualActionCore(original)
	core = SanitizeImagePromptForPolicy(core, SanitizeLevelStrict)
	if core == "" {
		core = "fantasy cinematic environment, stylized characters"
	}
	runes := []rune(core)
	if len(runes) > 180 {
		core = string(runes[:180])
	}
	return collapseSpaces(strings.Join([]string{
		"3D anime cinematic still frame",
		core,
		"young male hero white robe wild black hair",
		"twilight dusty battlefield atmosphere",
		"stylized dramatic pose",
		"no graphic violence",
		"family friendly",
		"soft cinematic lighting",
		"high fidelity composition",
	}, ", "))
}

// BuildUltraMinimalSafeImagePrompt is the final policy escape hatch: a short,
// story-adjacent prompt that avoids asset lore and risky verbs entirely.
func BuildUltraMinimalSafeImagePrompt(original string) string {
	core := ExtractVisualActionCore(original)
	core = SanitizeImagePromptForPolicy(core, SanitizeLevelStrict)
	// Keep only mild camera cues if present.
	var camera []string
	lower := strings.ToLower(core)
	for _, cue := range []string{"medium shot", "close-up", "wide shot", "back view", "profile"} {
		if strings.Contains(lower, cue) {
			camera = append(camera, cue)
		}
	}
	parts := []string{
		"3D anime cinematic still frame",
		"young male character in white robes",
		"wild black hair",
		"twilight dusty battlefield",
		"dramatic emotional pose",
		"stylized anime lighting",
		"family friendly",
		"no graphic violence",
		"soft cinematic lighting",
	}
	if len(camera) > 0 {
		parts = append([]string{parts[0], strings.Join(camera, ", ")}, parts[1:]...)
	}
	return collapseSpaces(strings.Join(parts, ", "))
}

// BuildFallbackSafeShotPrompt builds a minimal safe shot prompt from scene metadata.
func BuildFallbackSafeShotPrompt(item task.StoryboardItem, style, videoRatio string) string {
	scene := SanitizeImagePromptForPolicy(strings.TrimSpace(item.Scene), SanitizeLevelStrict)
	if scene == "" {
		scene = "fantasy cinematic environment"
	}
	parts := []string{
		"3D anime cinematic still",
		"character_id consistent",
		scene + " atmosphere",
		"stylized dramatic pose",
		"no graphic violence",
		"soft cinematic lighting",
	}
	if style != "" {
		parts = append(parts, style+" art style")
	}
	parts = append(parts, asset.StylePromptAnchors(videoRatio, style)...)
	return collapseSpaces(strings.Join(parts, ", "))
}

var lightPolicyReplacer = strings.NewReplacer(
	// 仅处理真正容易踩线的极限词：直白血腥 / 尸体肢解 / 裸露色情
	"鲜血", "红色光效",
	"流血", "激烈光影",
	"血迹", "场景氛围",
	"血腥", "激烈",
	"血沫", "水雾",
	"喷血", "能量爆发",
	"染血", "破损污渍",
	"血肉", "能量特效",
	"碎尸", "崩碎特效",
	"肢解", "击溃",
	"内脏", "能量碎片",
	"头颅", "头盔",
	"割喉", "对峙",
	"斩首", "挥剑",
	"虐杀", "激战",
	"尸体", "倒地身影",
	"尸骨", "残骸",
	"裸体", "",
	"色情", "",
)

func collapseSpaces(s string) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	return reMultiSpace.ReplaceAllString(s, " ")
}
