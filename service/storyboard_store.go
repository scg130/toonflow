package service

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"toonflow/task"
)

// SaveStoryboardItems persists episode storyboard shots (including image URLs) to DB.
func SaveStoryboardItems(db *sql.DB, projectID, episodeID string, items []task.StoryboardItem) error {
	if db == nil || projectID == "" {
		return fmt.Errorf("invalid save storyboard args")
	}
	shotsJSON, err := json.Marshal(NormalizeStoryboardItems(items))
	if err != nil {
		return err
	}
	sbID := fmt.Sprintf("sb_%s", projectID)
	if episodeID != "" {
		sbID = fmt.Sprintf("sb_%s_%s", projectID, episodeID)
	}
	_, err = db.Exec(`
		INSERT INTO o_storyboard (id, project_id, scene_name, shots, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET shots = excluded.shots, updated_at = CURRENT_TIMESTAMP
	`, sbID, projectID, "episode", string(shotsJSON))
	return err
}

// UpdateStoryboardShotMedia updates one shot's image_url / image_remote_url in DB.
func UpdateStoryboardShotMedia(db *sql.DB, projectID, episodeID string, shotNumber int, imageURL, remoteURL string) error {
	items, err := LoadStoryboardItems(db, projectID, episodeID)
	if err != nil || len(items) == 0 {
		return err
	}
	updated := false
	for i := range items {
		if items[i].ShotNumber == shotNumber {
			if imageURL != "" {
				items[i].ImageURL = imageURL
			}
			if remoteURL != "" {
				items[i].ImageRemoteURL = remoteURL
			}
			updated = true
			break
		}
	}
	if !updated {
		return fmt.Errorf("shot %d not found", shotNumber)
	}
	return SaveStoryboardItems(db, projectID, episodeID, items)
}

// ShotHasImage reports whether a storyboard shot already has generated image media.
func ShotHasImage(it task.StoryboardItem) bool {
	return strings.TrimSpace(it.ImageURL) != "" || strings.TrimSpace(it.ImageRemoteURL) != ""
}

// MergeShotMediaFromStore copies image fields for one shot from DB into dst.
func MergeShotMediaFromStore(db *sql.DB, projectID, episodeID string, shotNumber int, dst *task.StoryboardItem) {
	if db == nil || dst == nil || shotNumber <= 0 {
		return
	}
	items, err := LoadStoryboardItems(db, projectID, episodeID)
	if err != nil {
		return
	}
	for _, it := range items {
		if it.ShotNumber != shotNumber {
			continue
		}
		if it.ImageURL != "" {
			dst.ImageURL = it.ImageURL
		}
		if it.ImageRemoteURL != "" {
			dst.ImageRemoteURL = it.ImageRemoteURL
		}
		if len(it.AssetIDs) > 0 && len(dst.AssetIDs) == 0 {
			dst.AssetIDs = it.AssetIDs
		}
		return
	}
}
