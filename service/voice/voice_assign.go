package voice

import (
	"toonflow/service/asset"
	"toonflow/service/internal/jsonutil"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"toonflow/adapter"
	"toonflow/skill"
)

type voiceAssignItem struct {
	Name    string `json:"name"`
	VoiceID string `json:"voice_id"`
}

// AssignCharacterVoices uses LLM to pick Edge TTS voices for role assets.
func AssignCharacterVoices(ctx context.Context, db *sql.DB, v adapter.Vendor, skillMgr *skill.Manager, projectID string) (int, error) {
	if db == nil || projectID == "" {
		return 0, fmt.Errorf("project_id required")
	}
	if v == nil {
		return 0, fmt.Errorf("AI vendor not configured")
	}

	roles, err := loadMainRoleAssets(db, projectID)
	if err != nil {
		return 0, err
	}
	if len(roles) == 0 {
		return 0, fmt.Errorf("请先从剧本提取角色资产")
	}

	catalogJSON, _ := json.Marshal(EdgeVoiceCatalog())
	var roleLines []string
	for _, r := range roles {
		roleLines = append(roleLines, fmt.Sprintf("- %s: %s", r.Name, strings.TrimSpace(r.Desc)))
	}

	systemPrompt := voiceAssignSystemPrompt(skillMgr)
	userPrompt := fmt.Sprintf(`可选 voice_id 列表（JSON）:
%s

待分配角色:
%s

输出 JSON 数组，每项含 name、voice_id。`, string(catalogJSON), strings.Join(roleLines, "\n"))

	resp, err := v.TextRequest(ctx, adapter.DefaultTextModel, adapter.TextParams{
		Messages: []adapter.TextMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.3,
		MaxTokens:   2000,
	})
	if err != nil {
		return 0, err
	}

	var items []voiceAssignItem
	text := jsonutil.ExtractJSONArray(resp.Content)
	if err := json.Unmarshal([]byte(text), &items); err != nil {
		return 0, fmt.Errorf("parse voice assignment: %w", err)
	}

	byName := map[string]string{}
	for _, it := range items {
		name := strings.TrimSpace(it.Name)
		voice := strings.TrimSpace(it.VoiceID)
		if name == "" || voice == "" {
			continue
		}
		if !IsValidEdgeVoice(voice) {
			voice = DefaultVoiceForGender(inferGenderFromDesc(findRoleDesc(roles, name)))
		}
		byName[name] = voice
	}

	updated := 0
	for _, r := range roles {
		voice, ok := byName[r.Name]
		if !ok {
			voice = DefaultVoiceForGender(inferGenderFromDesc(r.Desc))
		}
		res, err := db.Exec(`
			UPDATE o_assets SET voice_id = ? WHERE project_id = ? AND name = ? AND type = 'role' AND COALESCE(parent_id, 0) = 0`,
			voice, projectID, r.Name)
		if err != nil {
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			updated++
		}
	}
	if updated == 0 {
		return 0, fmt.Errorf("未能更新角色音色")
	}
	return updated, nil
}

func voiceAssignSystemPrompt(skillMgr *skill.Manager) string {
	base := "你是短剧配音导演，为每个角色分配唯一、合适的 Edge TTS 神经语音。"
	if skillMgr != nil {
		if s := strings.TrimSpace(skillMgr.Get("production_execution")); s != "" {
			if idx := strings.Index(s, "voice_assigner"); idx >= 0 {
				base += "\n\n" + s
			}
		}
	}
	base += "\n\n仅输出 JSON 数组，禁止 Markdown 或说明文字。"
	return base
}

func loadMainRoleAssets(db *sql.DB, projectID string) ([]asset.ProjectAsset, error) {
	rows, err := db.Query(`
		SELECT id, name, COALESCE(desc, ''), type, COALESCE(file_url, ''), COALESCE(parent_id, 0), COALESCE(voice_id, '')
		FROM o_assets WHERE project_id = ? AND type = 'role' AND COALESCE(parent_id, 0) = 0
		ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []asset.ProjectAsset
	for rows.Next() {
		var a asset.ProjectAsset
		if err := rows.Scan(&a.ID, &a.Name, &a.Desc, &a.Type, &a.FileURL, &a.ParentID, &a.VoiceID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func findRoleDesc(roles []asset.ProjectAsset, name string) string {
	for _, r := range roles {
		if r.Name == name {
			return r.Desc
		}
	}
	return ""
}

func inferGenderFromDesc(desc string) string {
	d := strings.ToLower(desc)
	if strings.Contains(desc, "女") || strings.Contains(d, "female") || strings.Contains(desc, "少女") || strings.Contains(desc, "姑娘") {
		return "female"
	}
	if strings.Contains(desc, "童") || strings.Contains(desc, "孩") || strings.Contains(desc, "少年") {
		return "child"
	}
	return "male"
}

// LookupCharacterVoice finds voice_id for a speaker name in project assets.
func LookupCharacterVoice(db *sql.DB, projectID, speaker string) string {
	speaker = NormalizeSpeakerName(speaker)
	if speaker == "" || db == nil {
		return DefaultNarrationVoice
	}
	var voiceID sql.NullString
	err := db.QueryRow(`
		SELECT voice_id FROM o_assets
		WHERE project_id = ? AND type = 'role' AND COALESCE(parent_id, 0) = 0
		  AND (name = ? OR name LIKE ?)
		ORDER BY CASE WHEN name = ? THEN 0 ELSE 1 END
		LIMIT 1`,
		projectID, speaker, speaker+"%", speaker).Scan(&voiceID)
	if err == nil && voiceID.Valid && voiceID.String != "" {
		return voiceID.String
	}
	return DefaultVoiceForGender(inferGenderFromDesc(speaker))
}

func NormalizeSpeakerName(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, "「」\"'")
	if i := strings.IndexAny(s, "（("); i > 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

// RolesHaveVoices reports whether all main roles have voice_id assigned.
func RolesHaveVoices(db *sql.DB, projectID string) (bool, error) {
	roles, err := loadMainRoleAssets(db, projectID)
	if err != nil {
		return false, err
	}
	if len(roles) == 0 {
		return true, nil
	}
	for _, r := range roles {
		if strings.TrimSpace(r.VoiceID) == "" {
			return false, nil
		}
	}
	return true, nil
}
