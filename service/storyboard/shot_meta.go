package storyboard

import (
	"database/sql"
	"fmt"

	"toonflow/task"
)

// ShotMeta holds one storyboard shot's generation fields.
type ShotMeta struct {
	ShotNumber     int
	Scene          string
	Description    string
	Dialogue       *task.ShotDialogue
	Prompt         string
	Camera         string
	Duration       float64
	ShotSize       string
	Angle          string
	Composition    string
	Lighting       string
	ColorTone      string
	Motion         string
	ActionContinue string
	Transition     string
	SceneLink      string
	ImageURL       string
	ImageRemoteURL string
	Beats          []task.ShotBeat
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
			SyncSevenElements(&it)
			return &ShotMeta{
				ShotNumber:     it.ShotNumber,
				Scene:          it.Scene,
				Description:    it.Description,
				Dialogue:       it.Dialogue,
				Prompt:         it.Prompt,
				Camera:         it.Camera,
				Duration:       it.Duration,
				ShotSize:       it.ShotSize,
				Angle:          it.Angle,
				Composition:    it.Composition,
				Lighting:       it.Lighting,
				ColorTone:      it.ColorTone,
				Motion:         it.Motion,
				ActionContinue: it.ActionContinue,
				Transition:     it.Transition,
				SceneLink:      it.SceneLink,
				ImageURL:       it.ImageURL,
				ImageRemoteURL: it.ImageRemoteURL,
				Beats:          it.Beats,
			}, nil
		}
	}
	return nil, fmt.Errorf("未找到第 %d 镜", shotNumber)
}

// AsStoryboardItem converts ShotMeta back to a StoryboardItem for prompt helpers.
func (s *ShotMeta) AsStoryboardItem() task.StoryboardItem {
	if s == nil {
		return task.StoryboardItem{}
	}
	return task.StoryboardItem{
		ShotNumber:     s.ShotNumber,
		Scene:          s.Scene,
		Description:    s.Description,
		Dialogue:       s.Dialogue,
		Prompt:         s.Prompt,
		Camera:         s.Camera,
		Duration:       s.Duration,
		ShotSize:       s.ShotSize,
		Angle:          s.Angle,
		Composition:    s.Composition,
		Lighting:       s.Lighting,
		ColorTone:      s.ColorTone,
		Motion:         s.Motion,
		ActionContinue: s.ActionContinue,
		Transition:     s.Transition,
		SceneLink:      s.SceneLink,
		ImageURL:       s.ImageURL,
		ImageRemoteURL: s.ImageRemoteURL,
		Beats:          s.Beats,
	}
}
