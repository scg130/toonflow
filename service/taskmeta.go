package service

import (
	"database/sql"
	"fmt"
	"strings"

	"toonflow/task"
)

// EnrichTaskMeta fills display fields (project/episode names, title) on a task.
func EnrichTaskMeta(db *sql.DB, t *task.Task) {
	if t == nil || db == nil {
		return
	}
	if t.ProjectID != "" {
		_ = db.QueryRow("SELECT name FROM o_project WHERE id = ?", t.ProjectID).Scan(&t.ProjectName)
	}
	if t.EpisodeID != "" {
		_ = db.QueryRow(
			"SELECT title, episode_num FROM o_episode WHERE id = ?",
			t.EpisodeID,
		).Scan(&t.EpisodeTitle, &t.EpisodeNum)
	}
	t.Title = BuildTaskTitle(t)
	if t.Step == "" || t.Step == "waiting" {
		t.Step = t.Title
	}
}

// BuildTaskTitle returns a human-readable task label for the task center.
func BuildTaskTitle(t *task.Task) string {
	mode := taskModeLabel(t.Mode)
	ep := episodeLabel(t.EpisodeNum, t.EpisodeTitle)
	shots := shotsLabel(t.GenerateShots)
	parts := make([]string, 0, 4)
	if mode != "" {
		parts = append(parts, mode)
	}
	if t.ProjectName != "" {
		parts = append(parts, t.ProjectName)
	}
	if ep != "" {
		parts = append(parts, ep)
	}
	if shots != "" {
		parts = append(parts, shots)
	}
	if len(parts) == 0 {
		return t.ID
	}
	return strings.Join(parts, " · ")
}

func taskModeLabel(mode string) string {
	switch mode {
	case "images":
		return "生成图片"
	case "video":
		return "生成视频"
	case "full":
		return "一键出片"
	case "parse":
		return "解析剧本"
	default:
		if mode != "" {
			return mode
		}
		return "生成任务"
	}
}

func episodeLabel(num int, title string) string {
	if num <= 0 && title == "" {
		return ""
	}
	if num > 0 && title != "" {
		return fmt.Sprintf("第%d集 %s", num, title)
	}
	if num > 0 {
		return fmt.Sprintf("第%d集", num)
	}
	return title
}

func shotsLabel(shots []int) string {
	if len(shots) == 0 {
		return "全部分镜"
	}
	if len(shots) == 1 {
		return fmt.Sprintf("第%d镜", shots[0])
	}
	parts := make([]string, len(shots))
	for i, n := range shots {
		parts[i] = fmt.Sprintf("第%d镜", n)
	}
	return strings.Join(parts, "、")
}
