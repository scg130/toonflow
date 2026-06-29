package adapter

import "strings"

// SanitizeAPIKey trims whitespace and removes common paste mistakes.
func SanitizeAPIKey(key string) string {
	key = strings.TrimSpace(key)
	key = strings.Trim(key, `"'`)
	for strings.HasPrefix(strings.ToLower(key), "bearer ") {
		key = strings.TrimSpace(key[7:])
	}
	return key
}

// IsLikelyAPIURL reports whether s looks like a URL mistakenly used as an API key.
func IsLikelyAPIURL(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// MaskAPIKey returns a safe preview of an API key for debugging.
func MaskAPIKey(key string) string {
	key = SanitizeAPIKey(key)
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return key[:1] + "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}
