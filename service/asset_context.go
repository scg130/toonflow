package service

import (
	"fmt"
	"strings"

	"toonflow/task"
)

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
		idSlug := CharacterIDFromName(a.Name)
		fmt.Fprintf(&b, "- id=%d type=%s name=%s character_id=%s", a.ID, a.Type, a.Name, idSlug)
		if d := strings.TrimSpace(a.Desc); d != "" {
			fmt.Fprintf(&b, " desc=%s", d)
		}
		b.WriteByte('\n')
	}
	b.WriteString("每镜 asset_ids 必须从上述 id 选取；prompt 须含 character_id 与 style: consistent。\n")
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
	}
	return items
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
		if !ok || a.Type != "role" {
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
			if a.Type != "role" {
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

// applyStoryboardStyleAnchors ensures each shot prompt includes render-engine style anchors.
func applyStoryboardStyleAnchors(items []task.StoryboardItem, videoRatio, artStyle string) []task.StoryboardItem {
	anchors := strings.Join(StylePromptAnchors(videoRatio, artStyle), ", ")
	for i := range items {
		lower := strings.ToLower(items[i].Prompt)
		if !strings.Contains(lower, "unreal engine") && !strings.Contains(lower, "octane render") {
			items[i].Prompt = anchors + ", " + items[i].Prompt
		}
	}
	return items
}
