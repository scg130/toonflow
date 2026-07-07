package pipeline

import (
	"path/filepath"
	"testing"

	"toonflow/storage"
)

func TestEpisodeStepDone_batchStepsRequireStoryboard(t *testing.T) {
	db, err := storage.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	projectID := "proj_test"
	episodeID := "ep_test"
	_, err = db.Exec(`INSERT INTO o_project (id, name, art_style, video_ratio) VALUES (?, ?, ?, ?)`,
		projectID, "Test", "3D", "16:9")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO o_episode (id, project_id, title, episode_num) VALUES (?, ?, ?, ?)`,
		episodeID, projectID, "EP01", 1)
	if err != nil {
		t.Fatal(err)
	}

	for _, stepID := range []string{"batch_generate_shot_images", "batch_generate_shot_videos", "batch_compose_shots"} {
		done, err := episodeStepDone(db.DB, projectID, episodeID, stepID)
		if err != nil {
			t.Fatalf("%s: %v", stepID, err)
		}
		if done {
			t.Fatalf("%s should not be done without storyboard", stepID)
		}
	}

	steps, err := ListEpisodePipelineStatus(db.DB, projectID, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.ID == "batch_generate_shot_images" || step.ID == "batch_generate_shot_videos" || step.ID == "batch_compose_shots" {
			if step.Done {
				t.Fatalf("%s marked done in status list without storyboard", step.ID)
			}
		}
	}
}
