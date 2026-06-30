package service

import (
	"testing"

	"toonflow/task"
)

func TestBuildTaskTitle(t *testing.T) {
	title := BuildTaskTitle(&task.Task{
		Mode:          "images",
		ProjectName:   "大战三帝",
		EpisodeNum:    1,
		EpisodeTitle:  "柳神陨落",
		GenerateShots: []int{3},
	})
	if title != "生成图片 · 大战三帝 · 第1集 柳神陨落 · 第3镜" {
		t.Fatalf("unexpected title: %s", title)
	}
}
