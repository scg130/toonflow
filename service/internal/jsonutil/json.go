package jsonutil

import "strings"

// ExtractJSONArray returns the outermost JSON array substring from text.
func ExtractJSONArray(text string) string {
	text = strings.TrimSpace(text)
	if start := strings.Index(text, "["); start >= 0 {
		if end := strings.LastIndex(text, "]"); end > start {
			return text[start : end+1]
		}
	}
	return text
}
