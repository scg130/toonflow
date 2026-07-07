package project

import (
	"database/sql"
	"fmt"
	"strings"

	"toonflow/service/asset"
)

// BuildStyleAnchor composes a project-level style embedding string from global config.
// All downstream image/video prompts must inherit this anchor (toonflow.doc §4.3).
func BuildStyleAnchor(artStyle, videoRatio, stylePrompt string) string {
	parts := asset.StylePromptAnchors(videoRatio, artStyle)
	if s := strings.TrimSpace(stylePrompt); s != "" {
		parts = append(parts, s)
	}
	parts = append(parts,
		"global style embedding locked",
		"zero model mutation across shots",
		"no random color shift",
		"unified cinematic color grade",
	)
	return strings.Join(parts, ", ")
}

// EnsureProjectStyleAnchor generates and persists style anchor when missing or stale.
func EnsureProjectStyleAnchor(db *sql.DB, projectID string) (string, error) {
	if db == nil || projectID == "" {
		return "", fmt.Errorf("project_id required")
	}
	var artStyle, videoRatio, anchor string
	err := db.QueryRow(
		`SELECT COALESCE(art_style,''), COALESCE(video_ratio,'16:9'), COALESCE(style_anchor,'')
		 FROM o_project WHERE id = ?`, projectID,
	).Scan(&artStyle, &videoRatio, &anchor)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(anchor) != "" {
		return anchor, nil
	}
	stylePrompt := LookupArtStylePrompt(db, artStyle)
	anchor = BuildStyleAnchor(artStyle, videoRatio, stylePrompt)
	_, _ = db.Exec(`UPDATE o_project SET style_anchor = ?, update_time = CURRENT_TIMESTAMP WHERE id = ?`,
		anchor, projectID)
	return anchor, nil
}

// RefreshProjectStyleAnchor recomputes anchor after project style/ratio changes.
func RefreshProjectStyleAnchor(db *sql.DB, projectID string) (string, error) {
	if db == nil || projectID == "" {
		return "", fmt.Errorf("project_id required")
	}
	var artStyle, videoRatio string
	err := db.QueryRow(
		`SELECT COALESCE(art_style,''), COALESCE(video_ratio,'16:9') FROM o_project WHERE id = ?`, projectID,
	).Scan(&artStyle, &videoRatio)
	if err != nil {
		return "", err
	}
	stylePrompt := LookupArtStylePrompt(db, artStyle)
	anchor := BuildStyleAnchor(artStyle, videoRatio, stylePrompt)
	_, err = db.Exec(`UPDATE o_project SET style_anchor = ?, update_time = CURRENT_TIMESTAMP WHERE id = ?`,
		anchor, projectID)
	return anchor, err
}

// LoadProjectStyleAnchor returns the locked style anchor for a project.
func LoadProjectStyleAnchor(db *sql.DB, projectID string) string {
	anchor, err := EnsureProjectStyleAnchor(db, projectID)
	if err != nil {
		return ""
	}
	return anchor
}
