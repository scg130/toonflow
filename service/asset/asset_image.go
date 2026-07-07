package asset

import (
	"database/sql"
	"fmt"
	"strings"

	"toonflow/task"
)

// ProjectAsset is a project-scoped asset used during image generation.
type ProjectAsset struct {
	ID       int
	Name     string
	Desc     string
	Type     string
	FileURL  string
	ParentID int
	VoiceID  string
}

// LoadProjectAssets returns all assets for a project.
func LoadProjectAssets(db *sql.DB, projectID string) ([]ProjectAsset, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project_id required")
	}
	rows, err := db.Query(`
		SELECT id, name, COALESCE(desc, ''), type, COALESCE(file_url, ''), COALESCE(parent_id, 0), COALESCE(voice_id, '')
		FROM o_assets WHERE project_id = ? ORDER BY type, name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProjectAsset
	for rows.Next() {
		var a ProjectAsset
		if err := rows.Scan(&a.ID, &a.Name, &a.Desc, &a.Type, &a.FileURL, &a.ParentID, &a.VoiceID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CountProjectAssets returns how many assets exist for a project.
func CountProjectAssets(db *sql.DB, projectID string) (int, error) {
	if projectID == "" {
		return 0, fmt.Errorf("project_id required")
	}
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM o_assets WHERE project_id = ?`, projectID).Scan(&n)
	return n, err
}

// RequireProjectAssets returns an error when the project has no assets.
func RequireProjectAssets(db *sql.DB, projectID string) error {
	n, err := CountProjectAssets(db, projectID)
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("请先从剧本提取资产后再生成图片")
	}
	return nil
}

// MatchShotAssets finds assets linked to a storyboard shot by explicit IDs or name in shot text.
func MatchShotAssets(shot task.StoryboardItem, assets []ProjectAsset) []ProjectAsset {
	if len(assets) == 0 {
		return nil
	}
	if len(shot.AssetIDs) > 0 {
		byID := make(map[int]ProjectAsset, len(assets))
		for _, a := range assets {
			byID[a.ID] = a
		}
		var out []ProjectAsset
		for _, id := range shot.AssetIDs {
			if a, ok := byID[id]; ok {
				out = append(out, a)
			}
		}
		if len(out) > 0 {
			return out
		}
	}

	text := strings.ToLower(strings.Join([]string{shot.Scene, shot.Description, shot.Prompt}, " "))
	var matched []ProjectAsset
	for _, a := range assets {
		name := strings.TrimSpace(a.Name)
		if len([]rune(name)) < 2 {
			continue
		}
		if strings.Contains(text, strings.ToLower(name)) {
			matched = append(matched, a)
		}
	}
	return matched
}

// ShotImageParams resolves reference image URL and extra prompt constraints from shot assets.
// When no assets match, both return values are empty and generation uses the shot prompt only.
func ShotImageParams(db *sql.DB, projectID string, shot task.StoryboardItem) (refURL, extraPrompt string, assetIDs []int) {
	assets, err := LoadProjectAssets(db, projectID)
	if err != nil || len(assets) == 0 {
		return "", "", nil
	}
	matched := MatchShotAssets(shot, assets)
	if len(matched) == 0 {
		return "", "", nil
	}

	var descParts []string
	for _, a := range matched {
		assetIDs = append(assetIDs, a.ID)
		if refURL == "" && roleReferenceImageURL(a) {
			refURL = strings.TrimSpace(a.FileURL)
		}
		label := assetTypeLabel(a.Type)
		cid := CharacterIDFromName(a.Name)
		if a.Type == "role" {
			part := fmt.Sprintf("%s「%s」character_id: %s, style: consistent", label, a.Name, cid)
			if d := RoleAssetDescForShot(a.Desc, shot); d != "" {
				part += ": " + d
			}
			descParts = append(descParts, part)
		} else {
			part := fmt.Sprintf("%s「%s」", label, a.Name)
			if strings.TrimSpace(a.Desc) != "" {
				part += ": " + strings.TrimSpace(a.Desc)
			}
			descParts = append(descParts, part)
		}
	}
	return refURL, strings.Join(descParts, "；"), assetIDs
}

func assetTypeLabel(t string) string {
	switch t {
	case "role":
		return "角色"
	case "scene":
		return "场景"
	case "prop":
		return "道具"
	default:
		return "资产"
	}
}

func isHTTPURL(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}
