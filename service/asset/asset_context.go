package asset

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"toonflow/task"
)

var reCharacterIDTag = regexp.MustCompile(`(?i)character_id:\s*([^,]+)`)

var inanimateAssetKeywords = []string{
	"树桩", "stump", "残躯", "尸骸", " corpse", "枯木", "树干", "石碑", "雕像", "statue",
	"残骸", "器物", "灵根", "宝石", "gem", "artifact", "weapon", "武器", "剑", "刀",
	"面具", "mask", "道具", "prop", "inanimate",
}

// IsInanimateAsset reports props and misclassified role entries that must not use character_id.
func IsInanimateAsset(a ProjectAsset) bool {
	if a.Type == "prop" {
		return true
	}
	if a.Type == "scene" {
		return false
	}
	blob := strings.ToLower(strings.TrimSpace(a.Name + " " + a.Desc))
	for _, kw := range inanimateAssetKeywords {
		if strings.Contains(blob, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

func assetUsesCharacterID(a ProjectAsset) bool {
	return a.Type == "role" && !IsInanimateAsset(a)
}

// FormatAssetsForStoryboardPrompt builds an asset catalog block for storyboard LLM prompts.
func FormatAssetsForStoryboardPrompt(assets []ProjectAsset) string {
	if len(assets) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n【项目资产清单 — 分镜必须引用】\n")
	for _, a := range assets {
		if a.ParentID > 0 {
			continue
		}
		switch a.Type {
		case "role":
			if IsInanimateAsset(a) {
				fmt.Fprintf(&b, "- id=%d type=prop name=%s (inanimate, no character_id)", a.ID, a.Name)
			} else {
				fmt.Fprintf(&b, "- id=%d type=role name=%s character_id=%s", a.ID, a.Name, CharacterIDFromName(a.Name))
			}
		case "prop":
			fmt.Fprintf(&b, "- id=%d type=prop name=%s", a.ID, a.Name)
		case "scene":
			fmt.Fprintf(&b, "- id=%d type=scene name=%s", a.ID, a.Name)
		default:
			fmt.Fprintf(&b, "- id=%d type=%s name=%s", a.ID, a.Type, a.Name)
		}
		if d := strings.TrimSpace(a.Desc); d != "" {
			fmt.Fprintf(&b, " desc=%s", d)
		}
		b.WriteByte('\n')
	}
	b.WriteString("每镜 asset_ids 须从上述 id 选取。仅 type=role 可写 character_id；道具/场景镜用英文物体或环境描述，禁止把道具/场景名写入 character_id。\n")
	return b.String()
}

// CharacterIDFromName derives a stable character_id slug from an asset name.
func CharacterIDFromName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	if idx := strings.Index(name, "·"); idx > 0 {
		name = name[:idx]
	}
	return strings.ReplaceAll(name, " ", "_")
}

// LinkStoryboardAssets fills asset_ids and injects character consistency tags into prompts.
func LinkStoryboardAssets(items []task.StoryboardItem, assets []ProjectAsset) []task.StoryboardItem {
	if len(assets) == 0 {
		return items
	}
	for i := range items {
		if len(items[i].AssetIDs) == 0 {
			for _, a := range MatchShotAssets(items[i], assets) {
				items[i].AssetIDs = append(items[i].AssetIDs, a.ID)
			}
		}
		items[i] = injectCharacterConsistencyPrompt(items[i], assets)
		items[i] = SanitizeStoryboardItemPrompt(items[i], assets)
	}
	return items
}

// SanitizeFinalImagePrompt cleans the assembled image API prompt before generation.
func SanitizeFinalImagePrompt(prompt string, shot task.StoryboardItem, assets []ProjectAsset) string {
	prompt = StripNonRoleCharacterIDs(prompt, assets)
	if !ShotHasHumanRole(shot, assets) {
		prompt = stripHumanRenderTags(prompt)
	}
	return collapsePromptCommas(prompt)
}

func stripHumanRenderTags(prompt string) string {
	repl := strings.NewReplacer(
		"consistent character design", "consistent visual design",
		"subsurface scattering skin translucency", "surface material detail",
		"natural character motion", "slow environmental motion",
	)
	out := repl.Replace(prompt)
	var kept []string
	for _, seg := range strings.Split(out, ",") {
		s := strings.TrimSpace(seg)
		if s == "" {
			continue
		}
		lower := strings.ToLower(s)
		if strings.Contains(lower, "character_id") || strings.Contains(lower, "style: consistent") {
			continue
		}
		kept = append(kept, s)
	}
	if len(kept) == 0 {
		return "environment and props only, no human character, inanimate object focus"
	}
	return strings.Join(kept, ", ")
}

// ShotHasHumanRole reports whether the shot should use human character rendering tags.
func ShotHasHumanRole(shot task.StoryboardItem, assets []ProjectAsset) bool {
	return shotHasRoleAsset(shot, assets)
}

func SanitizeStoryboardItemPrompt(shot task.StoryboardItem, assets []ProjectAsset) task.StoryboardItem {
	shot.Prompt = StripNonRoleCharacterIDs(shot.Prompt, assets)
	if !shotHasRoleAsset(shot, assets) {
		shot.Prompt = stripAllCharacterIDs(shot.Prompt)
		if hint := inanimateShotHint(shot, assets); hint != "" {
			lower := strings.ToLower(shot.Prompt)
			if !strings.Contains(lower, "no human") && !strings.Contains(lower, "inanimate") {
				shot.Prompt = hint + ", " + strings.TrimSpace(shot.Prompt)
			}
		}
	}
	return shot
}

// SanitizeStoryboardItemForImage loads project assets and sanitizes shot prompt before image gen.
func SanitizeStoryboardItemForImage(db *sql.DB, projectID string, shot task.StoryboardItem) task.StoryboardItem {
	assets, err := LoadProjectAssets(db, projectID)
	if err != nil || len(assets) == 0 {
		return shot
	}
	return SanitizeStoryboardItemPrompt(shot, assets)
}

// StripNonRoleCharacterIDs deletes character_id values that refer to prop/scene assets.
func StripNonRoleCharacterIDs(prompt string, assets []ProjectAsset) string {
	nonRole := nonRoleCharacterIDKeys(assets)
	if strings.TrimSpace(prompt) == "" || len(nonRole) == 0 {
		return prompt
	}
	out := reCharacterIDTag.ReplaceAllStringFunc(prompt, func(match string) string {
		sub := reCharacterIDTag.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		val := strings.TrimSpace(sub[1])
		if isNonRoleCharacterID(val, nonRole) {
			return ""
		}
		return match
	})
	return collapsePromptCommas(out)
}

func nonRoleCharacterIDKeys(assets []ProjectAsset) map[string]bool {
	out := map[string]bool{}
	for _, a := range assets {
		if a.ParentID > 0 || !usesCharacterIDKey(a) {
			continue
		}
		name := strings.TrimSpace(a.Name)
		out[strings.ToLower(name)] = true
		out[strings.ToLower(CharacterIDFromName(name))] = true
	}
	return out
}

func usesCharacterIDKey(a ProjectAsset) bool {
	if a.Type == "scene" || a.Type == "prop" {
		return true
	}
	return IsInanimateAsset(a)
}

func isNonRoleCharacterID(val string, nonRole map[string]bool) bool {
	val = strings.TrimSpace(val)
	if val == "" {
		return false
	}
	if nonRole[strings.ToLower(val)] {
		return true
	}
	for key := range nonRole {
		if key != "" && strings.Contains(strings.ToLower(val), key) {
			return true
		}
	}
	return false
}

func stripAllCharacterIDs(prompt string) string {
	out := reCharacterIDTag.ReplaceAllString(prompt, "")
	return collapsePromptCommas(out)
}

func collapsePromptCommas(s string) string {
	s = strings.TrimSpace(s)
	for strings.Contains(s, ", ,") || strings.Contains(s, ",,") {
		s = strings.ReplaceAll(s, ", ,", ", ")
		s = strings.ReplaceAll(s, ",,", ",")
	}
	s = strings.TrimPrefix(s, ", ")
	s = strings.TrimSuffix(s, ", ")
	return strings.TrimSpace(s)
}

func shotHasRoleAsset(shot task.StoryboardItem, assets []ProjectAsset) bool {
	byID := make(map[int]ProjectAsset, len(assets))
	for _, a := range assets {
		byID[a.ID] = a
	}
	for _, id := range shot.AssetIDs {
		if a, ok := byID[id]; ok && assetUsesCharacterID(a) {
			return true
		}
	}
	for _, a := range MatchShotAssets(shot, assets) {
		if assetUsesCharacterID(a) {
			return true
		}
	}
	return false
}

func inanimateShotHint(shot task.StoryboardItem, assets []ProjectAsset) string {
	matched := MatchShotAssets(shot, assets)
	var parts []string
	for _, a := range matched {
		if IsInanimateAsset(a) || a.Type == "prop" || a.Type == "scene" {
			name := strings.TrimSpace(a.Name)
			if name == "" {
				continue
			}
			kind := a.Type
			if IsInanimateAsset(a) && a.Type == "role" {
				kind = "prop"
			}
			parts = append(parts, name+" inanimate "+kind+", no human character")
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "; ")
	}
	text := strings.TrimSpace(shot.Description)
	if text != "" {
		return "environment and props only, no human character, " + text
	}
	return "environment and props only, no human character"
}

func injectCharacterConsistencyPrompt(shot task.StoryboardItem, assets []ProjectAsset) task.StoryboardItem {
	byID := make(map[int]ProjectAsset, len(assets))
	for _, a := range assets {
		byID[a.ID] = a
	}
	var roleTags []string
	seen := map[string]bool{}
	for _, id := range shot.AssetIDs {
		a, ok := byID[id]
		if !ok || !assetUsesCharacterID(a) {
			continue
		}
		cid := CharacterIDFromName(a.Name)
		if seen[cid] {
			continue
		}
		seen[cid] = true
		roleTags = append(roleTags, fmt.Sprintf("character_id: %s, style: consistent", cid))
	}
	if len(roleTags) == 0 {
		for _, a := range MatchShotAssets(shot, assets) {
			if !assetUsesCharacterID(a) {
				continue
			}
			cid := CharacterIDFromName(a.Name)
			if seen[cid] {
				continue
			}
			seen[cid] = true
			roleTags = append(roleTags, fmt.Sprintf("character_id: %s, style: consistent", cid))
		}
	}
	if len(roleTags) == 0 {
		return shot
	}
	block := strings.Join(roleTags, "; ")
	lower := strings.ToLower(shot.Prompt)
	if !strings.Contains(lower, "character_id") {
		shot.Prompt = block + ", " + shot.Prompt
	}
	return shot
}

// StylePromptAnchors returns render-engine and color-consistency tags for prompts.
func StylePromptAnchors(videoRatio, artStyle string) []string {
	tags := []string{
		"Unreal Engine 5 render",
		"Octane Render high fidelity",
		"consistent lighting",
		"consistent character design",
		"unified color palette controlled saturation",
	}
	switch strings.TrimSpace(videoRatio) {
	case "9:16":
		tags = append(tags, "vertical 9:16 composition", "portrait framing teal-orange cinematic grade")
	default:
		tags = append(tags, "widescreen 16:9 composition", "cinematic color grade restrained tone range")
	}
	if s := strings.TrimSpace(artStyle); s != "" {
		tags = append(tags, s+" art style anchor")
	}
	return tags
}

// ApplyStoryboardStyleAnchors ensures each shot prompt includes render-engine style anchors.
func ApplyStoryboardStyleAnchors(items []task.StoryboardItem, videoRatio, artStyle string) []task.StoryboardItem {
	anchors := strings.Join(StylePromptAnchors(videoRatio, artStyle), ", ")
	for i := range items {
		lower := strings.ToLower(items[i].Prompt)
		if !strings.Contains(lower, "unreal engine") && !strings.Contains(lower, "octane render") {
			items[i].Prompt = anchors + ", " + items[i].Prompt
		}
	}
	return items
}
