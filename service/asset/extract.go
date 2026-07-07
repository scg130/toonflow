package asset

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"toonflow/adapter"
	"toonflow/service/internal/jsonutil"
)

type extractAssetItem struct {
	Name            string
	Type            string
	Desc            string
	CharacterID     string
	FeatureKeywords []string
	TurnaroundViews []extractTurnaroundView
}

type extractTurnaroundView struct {
	View string
	Desc string
}

// ExtractAssetsFromEpisode extracts characters/scenes/props from episode script.
func ExtractAssetsFromEpisode(ctx context.Context, db *sql.DB, v adapter.Vendor, userID, projectID, episodeID string) (int, error) {
	var script string
	err := db.QueryRow("SELECT script_content FROM o_episode WHERE id = ?", episodeID).Scan(&script)
	if err != nil || script == "" {
		var work string
		_ = db.QueryRow("SELECT content FROM o_agent_work WHERE project_id = ? AND episode_id = ? AND work_type = 'script'", projectID, episodeID).Scan(&work)
		script = work
	}
	if script == "" {
		return 0, fmt.Errorf("请先生成剧本")
	}

	var videoRatio, artStyle string
	_ = db.QueryRow("SELECT video_ratio, art_style FROM o_project WHERE id = ?", projectID).Scan(&videoRatio, &artStyle)

	systemPrompt := buildAssetExtractSystemPrompt(videoRatio, artStyle)
	resp, err := v.TextRequest(ctx, adapter.DefaultTextModel, adapter.TextParams{
		Messages: []adapter.TextMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: script},
		},
		MaxTokens: 6000,
	})
	if err != nil {
		return 0, err
	}

	type turnaroundView struct {
		View string `json:"view"`
		Desc string `json:"desc"`
	}
	type assetItemJSON struct {
		Name            string           `json:"name"`
		Type            string           `json:"type"`
		Desc            string           `json:"desc"`
		CharacterID     string           `json:"character_id"`
		FeatureKeywords []string         `json:"feature_keywords"`
		TurnaroundViews []turnaroundView `json:"turnaround_views"`
	}
	var raw []assetItemJSON
	text := jsonutil.ExtractJSONArray(resp.Content)
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return 0, fmt.Errorf("parse assets: %w", err)
	}
	items := make([]extractAssetItem, 0, len(raw))
	for _, r := range raw {
		it := extractAssetItem{
			Name: r.Name, Type: r.Type, Desc: r.Desc,
			CharacterID: r.CharacterID, FeatureKeywords: r.FeatureKeywords,
		}
		for _, tv := range r.TurnaroundViews {
			it.TurnaroundViews = append(it.TurnaroundViews, extractTurnaroundView{View: tv.View, Desc: tv.Desc})
		}
		items = append(items, it)
	}

	count := 0
	for _, it := range items {
		if it.Name == "" {
			continue
		}
		if it.Type == "" {
			it.Type = "role"
		}
		desc := buildMainAssetDesc(it)
		parentID, inserted, err := upsertProjectAsset(db, projectID, userID, it.Name, desc, it.Type, 0, "")
		if err != nil {
			continue
		}
		if inserted {
			count++
		}
		if it.Type != "role" {
			continue
		}
		views := it.TurnaroundViews
		if len(views) == 0 {
			views = defaultTurnaroundViews(it)
		}
		for _, tv := range views {
			viewName := fmt.Sprintf("%s·%s", it.Name, tv.View)
			viewDesc := buildTurnaroundDesc(it, tv.View, tv.Desc)
			_, childInserted, err := upsertProjectAsset(db, projectID, userID, viewName, viewDesc, "role", int(parentID), tv.View)
			if err == nil && childInserted {
				count++
			}
		}
	}
	if userID != "" {
		_, _ = db.Exec(`UPDATE o_assets SET user_id = ? WHERE project_id = ? AND (user_id IS NULL OR user_id = '')`,
			userID, projectID)
	}
	if len(items) == 0 {
		return 0, fmt.Errorf("未能从剧本解析出资产")
	}
	return count, nil
}

func buildAssetExtractSystemPrompt(videoRatio, artStyle string) string {
	ratioNote := "16:9 widescreen"
	if videoRatio == "9:16" {
		ratioNote = "9:16 vertical"
	}
	return fmt.Sprintf(`你是短剧资产策划。从剧本提取角色/场景/道具，输出 JSON 数组（仅 JSON，无说明文字）。

每项字段：
- name (string) 资产名称
- type (string) role | scene | prop（无生命的物体如树桩、残躯、武器、面具等必须 type=prop，禁止标为 role）
- desc (string) 中文视觉描述（仅角色本体：发型、瞳色、服装、体型、气质；**禁止**手持道具、武器、面具；**禁止**具体背景/场景——背景请单独建 scene 资产，道具请建 prop 资产）
- character_id (string) 角色唯一 ID（仅 role，如 ShiHao）
- feature_keywords (string[]) 不可变特征关键词（发型、瞳色、服装、体型等，仅 role）
- turnaround_views (array, 仅主要 role) 多角度设定卡，每项含：
  - view: front | side | back | three_quarter
  - desc: 英文 turnaround sheet 描述（T-pose 参考、%s 构图、consistent lighting）

主要角色必须输出 4 个 turnaround_views。desc 中注明 character_id 与 style: consistent。
画风锚点: %s, Unreal Engine 5 render, Octane Render, high fidelity, consistent lighting, unified color palette`, ratioNote, artStyle)
}

func buildMainAssetDesc(it extractAssetItem) string {
	cid := it.CharacterID
	if cid == "" {
		cid = CharacterIDFromName(it.Name)
	}
	parts := []string{strings.TrimSpace(it.Desc)}
	parts = append(parts, fmt.Sprintf("character_id: %s", cid))
	if len(it.FeatureKeywords) > 0 {
		parts = append(parts, "feature_keywords: "+strings.Join(it.FeatureKeywords, ", "))
	}
	parts = append(parts, "style: consistent", "turnaround sheet reference required")
	return strings.Join(parts, " | ")
}

func defaultTurnaroundViews(it extractAssetItem) []extractTurnaroundView {
	cid := it.CharacterID
	if cid == "" {
		cid = CharacterIDFromName(it.Name)
	}
	kw := strings.Join(it.FeatureKeywords, ", ")
	base := strings.TrimSpace(it.Desc)
	return []extractTurnaroundView{
		{"front", fmt.Sprintf("%s turnaround sheet front view T-pose, character_id: %s, style: consistent, %s, consistent lighting", base, cid, kw)},
		{"side", fmt.Sprintf("%s turnaround sheet side profile, character_id: %s, style: consistent, %s", base, cid, kw)},
		{"back", fmt.Sprintf("%s turnaround sheet back view, character_id: %s, style: consistent, %s", base, cid, kw)},
		{"three_quarter", fmt.Sprintf("%s turnaround sheet three-quarter view, character_id: %s, style: consistent, %s", base, cid, kw)},
	}
}

func buildTurnaroundDesc(it extractAssetItem, view, desc string) string {
	if strings.TrimSpace(desc) != "" {
		return desc
	}
	cid := it.CharacterID
	if cid == "" {
		cid = CharacterIDFromName(it.Name)
	}
	return fmt.Sprintf("%s %s turnaround, character_id: %s, style: consistent, Unreal Engine 5 render, consistent lighting",
		strings.TrimSpace(it.Desc), view, cid)
}

func upsertProjectAsset(db *sql.DB, projectID, userID, name, desc, assetType string, parentID int, derive string) (int64, bool, error) {
	var id int64
	err := db.QueryRow(`SELECT id FROM o_assets WHERE project_id = ? AND name = ?`, projectID, name).Scan(&id)
	if err == sql.ErrNoRows {
		res, insErr := db.Exec(`
			INSERT INTO o_assets (project_id, user_id, name, desc, type, parent_id, derive)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			projectID, userID, name, desc, assetType, parentID, derive)
		if insErr != nil {
			return 0, false, insErr
		}
		id, _ = res.LastInsertId()
		return id, true, nil
	}
	if err != nil {
		return 0, false, err
	}
	_, err = db.Exec(`UPDATE o_assets SET desc = ?, type = ?, parent_id = ?, derive = ? WHERE id = ?`,
		desc, assetType, parentID, derive, id)
	return id, false, err
}
