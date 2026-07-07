package agent

import (
	"toonflow/service/asset"
	"toonflow/service/voice"
	"context"
	"database/sql"
	"fmt"

	"toonflow/adapter"
	"toonflow/skill"
	"toonflow/task"
)

// AgentExecutors groups specialized workflow executors (Huobao-style agent split).
type AgentExecutors struct {
	DB       *sql.DB
	Vendor   adapter.Vendor
	SkillMgr *skill.Manager
}

// ScriptRewriter generates skeleton / strategy / script with planning-focused prompts.
type ScriptRewriter struct{ *AgentExecutors }

// AssetExtractor extracts characters/scenes/props from episode script.
type AssetExtractor struct{ *AgentExecutors }

// StoryboardBreaker generates storyboard shots from script.
type StoryboardBreaker struct {
	*AgentExecutors
	Generate func(ctx context.Context, projectID, episodeID string) ([]task.StoryboardItem, error)
}

// VoiceAssigner assigns TTS voices to role assets.
type VoiceAssigner struct{ *AgentExecutors }

// NewAgentExecutors creates the executor bundle for AgentChat.
func NewAgentExecutors(db *sql.DB, v adapter.Vendor, skillMgr *skill.Manager) *AgentExecutors {
	return &AgentExecutors{DB: db, Vendor: v, SkillMgr: skillMgr}
}

// ExecutorForAction returns the specialized agent name for an action (for logging/UI).
func ExecutorForAction(actionType string) string {
	switch actionType {
	case "generate_skeleton", "generate_strategy", "generate_script":
		return "script_rewriter"
	case "extract_assets":
		return "extractor"
	case "generate_storyboard":
		return "storyboard_breaker"
	case "assign_character_voices":
		return "voice_assigner"
	case "compose_shot", "batch_compose_shots":
		return "compose_engine"
	default:
		return "general"
	}
}

// Extract runs asset extraction.
func (e *AssetExtractor) Extract(ctx context.Context, userID, projectID, episodeID string) (int, error) {
	return asset.ExtractAssetsFromEpisode(ctx, e.DB, e.Vendor, userID, projectID, episodeID)
}

// AssignVoices runs voice assignment for roles.
func (v *VoiceAssigner) AssignVoices(ctx context.Context, projectID string) (int, error) {
	return voice.AssignCharacterVoices(ctx, v.DB, v.Vendor, v.SkillMgr, projectID)
}

// BreakStoryboard runs storyboard generation when Generate is wired.
func (s *StoryboardBreaker) BreakStoryboard(ctx context.Context, projectID, episodeID string) ([]task.StoryboardItem, error) {
	if s.Generate == nil {
		return nil, fmt.Errorf("storyboard generator not configured")
	}
	return s.Generate(ctx, projectID, episodeID)
}
