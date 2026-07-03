package service

import (
	"regexp"
	"strings"

	"toonflow/task"
)

var (
	rolePropClausePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)[^,，;；\n]*(one hand )?hold(ing|s)?[^,，;；\n]*`),
		regexp.MustCompile(`[^,，;；\n]*(面具|mask|兽首)[^,，;；\n]*`),
		regexp.MustCompile(`[^,，;；\n]*[一]?手[^,，;；\n]*(轻)?[握持拿][^,，;；\n]*`),
	}
	roleSceneClausePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)[^,，;；\n]*(background|backdrop)[^,，;；\n]*`),
		regexp.MustCompile(`[^,，;；\n]*(身后|背景|远景)[^,，;；\n]*`),
		regexp.MustCompile(`(?i)[^,，;；\n]*red maple[^,，;；\n]*`),
		regexp.MustCompile(`[^,，;；\n]*红枫[^,，;；\n]*`),
	}
)

// RoleAssetDescForShot returns role description for image prompts.
// Handheld props and scene/backdrop clauses are omitted unless the shot text mentions them.
func RoleAssetDescForShot(desc string, shot task.StoryboardItem) string {
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return ""
	}
	out := desc
	if !shotMentionsProp(out, shot) {
		out = stripClauses(out, rolePropClausePatterns)
	}
	if !shotMentionsScene(out, shot) {
		out = stripClauses(out, roleSceneClausePatterns)
	}
	return cleanupDesc(out)
}

func stripClauses(desc string, patterns []*regexp.Regexp) string {
	out := desc
	for _, re := range patterns {
		out = re.ReplaceAllString(out, "")
	}
	return out
}

func shotMentionsProp(desc string, shot task.StoryboardItem) bool {
	text := shotText(shot)
	keywords := []string{"面具", "mask", "兽首", "道具", "手持", "握着", "holding"}
	for _, kw := range keywords {
		if strings.Contains(strings.ToLower(text), strings.ToLower(kw)) {
			return true
		}
	}
	if strings.Contains(desc, "面具") && strings.Contains(text, "石昊") {
		// allow explicit prop asset name match via MatchShotAssets elsewhere
	}
	return false
}

func shotMentionsScene(desc string, shot task.StoryboardItem) bool {
	text := strings.ToLower(shotText(shot))
	sceneKeys := []string{"背景", "background", "红枫", "maple", "身后", "backdrop", "云海"}
	for _, kw := range sceneKeys {
		if strings.Contains(text, strings.ToLower(kw)) {
			return true
		}
	}
	if s := strings.TrimSpace(shot.Scene); s != "" && strings.Contains(desc, s) {
		return true
	}
	return false
}

func shotText(shot task.StoryboardItem) string {
	return strings.Join([]string{shot.Scene, shot.Description, shot.Prompt}, " ")
}

func cleanupDesc(s string) string {
	s = strings.TrimSpace(s)
	for _, sep := range []string{",,", "，，", ", ,", "， ,"} {
		s = strings.ReplaceAll(s, sep, ",")
	}
	s = strings.Trim(s, ",，;；\n ")
	return strings.Join(strings.Fields(s), " ")
}

func roleReferenceImageURL(a ProjectAsset) bool {
	return a.Type == "role" && a.ParentID == 0 && isHTTPURL(a.FileURL)
}
