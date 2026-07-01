package service

import (
	"database/sql"
	"encoding/json"
	"fmt"

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
