package storyboard

import (
	"database/sql"
	"fmt"
)

// ShotMeta holds one storyboard shot's generation fields.
type ShotMeta struct {
	ShotNumber     int
	Description    string
	Dialogue       string
	Prompt         string
	Camera         string
	Duration       float64
	Lighting       string
	ActionContinue string
	Transition     string
	SceneLink      string
	ImageURL       string
	ImageRemoteURL string
}

// LoadShot loads one shot's metadata from the episode storyboard.
func LoadShot(db *sql.DB, projectID, episodeID string, shotNumber int) (*ShotMeta, error) {
	sbID := fmt.Sprintf("sb_%s_%s", projectID, episodeID)
	var shotsJSON string
	err := db.QueryRow(`SELECT shots FROM o_storyboard WHERE id = ?`, sbID).Scan(&shotsJSON)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("分镜不存在")
	}
	if err != nil {
		return nil, err
	}
	items, err := ParseStoryboardResponse(shotsJSON)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if it.ShotNumber == shotNumber {
			return &ShotMeta{
				ShotNumber:     it.ShotNumber,
				Description:    it.Description,
				Dialogue:       it.Dialogue,
				Prompt:         it.Prompt,
				Camera:         it.Camera,
				Duration:       it.Duration,
				Lighting:       it.Lighting,
				ActionContinue: it.ActionContinue,
				Transition:     it.Transition,
				SceneLink:      it.SceneLink,
				ImageURL:       it.ImageURL,
				ImageRemoteURL: it.ImageRemoteURL,
			}, nil
		}
	}
	return nil, fmt.Errorf("未找到第 %d 镜", shotNumber)
}
