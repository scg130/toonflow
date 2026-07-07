package project

import (
	"database/sql"
	"strings"
)

// LookupArtStylePrompt loads the DB prompt for an art style name.
func LookupArtStylePrompt(db *sql.DB, artStyle string) string {
	if artStyle == "" {
		return ""
	}
	var prompt string
	err := db.QueryRow(`SELECT prompt FROM o_artStyle WHERE name = ?`, artStyle).Scan(&prompt)
	if err != nil || strings.TrimSpace(prompt) == "" {
		return ""
	}
	return strings.TrimSpace(prompt)
}
