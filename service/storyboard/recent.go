package storyboard

import (
	"database/sql"

	"toonflow/task"
)

// StoryboardFromRecentChat parses the best storyboard from recent assistant messages.
func StoryboardFromRecentChat(db *sql.DB, projectID, episodeID string, limit int) []task.StoryboardItem {
	rows, err := db.Query(`
		SELECT content FROM o_chat_message
		WHERE project_id = ? AND (episode_id = ? OR episode_id = '') AND role = 'assistant'
		ORDER BY created_at DESC LIMIT ?`, projectID, episodeID, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var best []task.StoryboardItem
	for rows.Next() {
		var content string
		if rows.Scan(&content) != nil {
			continue
		}
		parsed, _ := ParseStoryboardResponse(content)
		if StoryboardScore(parsed) > StoryboardScore(best) {
			best = parsed
		}
	}
	return best
}
