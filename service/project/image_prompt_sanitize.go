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

var reChinese = regexp.MustCompile(`[\p{Han}]+`)

// IsContentPolicyViolation reports Agnes/OpenAI-style content policy errors.
func IsContentPolicyViolation(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "content_policy_violation") ||
		strings.Contains(s, "unable to generate this content") ||
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
	if level >= SanitizeLevelStrict {
		s = strictPolicyReplacer.Replace(s)
		s = reChinese.ReplaceAllString(s, " ")
	}
	s = collapseSpaces(s)
	return s
}

// BuildFallbackSafeShotPrompt is deprecated; image generation uses tiered prompt sanitize retries instead.
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
	"鲜血", "红色光效",
	"流血", "激烈光影",
	"血迹", "场景氛围",
	"血腥", "激烈",
	"杀戮", "对决",
	"杀死", "击败",
	"击毙", "击倒",
	"尸体", "倒地身影",
	"裸", "",
	"裸体", "",
	"色情", "",
	"狰狞", "严肃",
	"残忍", "激烈",
	"血肉", "能量特效",
	"斩首", "挥剑",
	"刺杀", "对峙",
	"屠", "战",
	"blood", "crimson light effect",
	"bloody", "dramatic",
	"gore", "energy effects",
	"gory", "stylized",
	"murder", "confrontation",
	"killing", "dramatic action",
	"kill", "defeat",
	"corpse", "fallen figure",
	"nude", "fully clothed",
	"naked", "fully clothed",
	"nsfw", "",
	"brutal", "intense",
	"massacre", "epic confrontation",
	"disembowel", "clash",
	"decapitat", "sword swing",
	"torture", "tension",
	"rape", "",
	"sexual", "",
)

var strictPolicyReplacer = strings.NewReplacer(
	"赤红", "golden",
	"血红", "warm",
	"双目", "eyes",
	"rage", "determination",
	"scream", "expression",
	"weapon blood", "gleaming weapon",
	"tear/blood", "emotional particles",
	"blood particles", "light particles",
	"fight", "dynamic pose",
	"combat", "action pose",
	"battle", "dramatic scene",
	"war", "conflict scene",
	"death", "dramatic moment",
	"dead", "still",
	"die", "fall",
	"stab", "thrust",
	"slash", "sweep",
	"gun", "prop",
	"knife", "blade",
)

func collapseSpaces(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}
