package service

import (
	"testing"

	"toonflow/task"
)

func TestShotHasImage(t *testing.T) {
	if ShotHasImage(task.StoryboardItem{}) {
		t.Fatal("empty shot should not have image")
	}
	if !ShotHasImage(task.StoryboardItem{ImageURL: "/output/x/shot_001.png"}) {
		t.Fatal("image_url should count")
	}
	if !ShotHasImage(task.StoryboardItem{ImageRemoteURL: "https://cdn.example/a.png"}) {
		t.Fatal("remote url should count")
	}
}
