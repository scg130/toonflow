package adapter

import (
	"database/sql"
	"encoding/json"
	"os"
)

// VendorSource describes where the active vendor credentials came from.
type VendorSource struct {
	BaseURL string `json:"base_url"`
	KeyHint string `json:"key_hint"`
	Source  string `json:"source"` // "db", "env", or "default"
}

// ResolveFromDB loads the first enabled vendor from the database, or falls back
// to Agnes-AI environment variables / default instance.
func ResolveFromDB(db *sql.DB, defaultVendorID string) Vendor {
	cfg := ResolveConfigFromDB(db)
	return NewAgnesAIVendor(cfg.BaseURL, cfg.APIKey)
}

// ResolveConfigFromDB returns merged vendor credentials (env overrides DB).
func ResolveConfigFromDB(db *sql.DB) struct {
	BaseURL string
	APIKey  string
	Info    VendorSource
} {
	out := struct {
		BaseURL string
		APIKey  string
		Info    VendorSource
	}{
		BaseURL: DefaultAgnesBaseURL,
		Info: VendorSource{
			BaseURL: DefaultAgnesBaseURL,
			Source:  "default",
		},
	}

	var inputValues string
	err := db.QueryRow(
		"SELECT input_values FROM o_vendorConfig WHERE enable = 1 ORDER BY created_at DESC LIMIT 1",
	).Scan(&inputValues)
	if err == nil {
		if url, key := parseVendorCredentials(inputValues); key != "" {
			out.BaseURL = NormalizeAgnesBaseURL(url)
			out.APIKey = key
			out.Info = VendorSource{
				BaseURL: out.BaseURL,
				KeyHint: MaskAPIKey(key),
				Source:  "db",
			}
		}
	}

	if envKey := SanitizeAPIKey(os.Getenv("AGNES_AI_API_KEY")); envKey != "" && !IsLikelyAPIURL(envKey) {
		out.APIKey = envKey
		out.Info.Source = "env"
		out.Info.KeyHint = MaskAPIKey(envKey)
	}
	if envURL := os.Getenv("AGNES_AI_BASE_URL"); envURL != "" {
		out.BaseURL = NormalizeAgnesBaseURL(envURL)
		out.Info.BaseURL = out.BaseURL
	}

	return out
}

func parseVendorCredentials(inputValues string) (url, key string) {
	var vals map[string]string
	if err := json.Unmarshal([]byte(inputValues), &vals); err != nil {
		return "", ""
	}
	url = vals["url"]
	if url == "" {
		url = vals["base_url"]
	}
	key = vals["key"]
	if key == "" {
		key = vals["api_key"]
	}
	key = SanitizeAPIKey(key)
	if key == "" || IsLikelyAPIURL(key) {
		return url, ""
	}
	return url, key
}

func vendorFromInputValues(inputValues string) Vendor {
	url, key := parseVendorCredentials(inputValues)
	if key == "" {
		return nil
	}
	return NewAgnesAIVendor(NormalizeAgnesBaseURL(url), key)
}
