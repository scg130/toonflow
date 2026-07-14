package agent

import (
	"fmt"
	"strings"

	"toonflow/service/project"
	"toonflow/skill"
)

// AIShortDramaScriptSystemPrompt is the system prompt for 5-minute 红果-style script extraction.
// Body lives in skills/prompts/script_extract.md.
func AIShortDramaScriptSystemPrompt() string {
	if s := strings.TrimSpace(skill.File("prompts/script_extract.md")); s != "" {
		return s
	}
	return "你是5分钟短剧编剧。写可见动作与短台词，禁止抽象情绪词。只输出 Markdown 正文。"
}

// AIShortDramaScriptUserPrompt builds the user prompt for script generation.
func AIShortDramaScriptUserPrompt(title string, params project.EpisodeParams, contextText, skeleton, strategy string) string {
	durationMin := params.TargetDurationMin
	if durationMin <= 0 {
		durationMin = 5
	}
	durationSec := durationMin * 60
	words := params.TargetWords
	if words <= 0 {
		words = 900
	}
	args := []any{title, durationMin, float64(durationSec), words, params.VideoRatio, params.ArtStyle,
		contextText, skeleton, strategy, title}
	if s := strings.TrimSpace(skill.Format("prompts/script_extract_user.md", args...)); s != "" {
		return s
	}
	return fmt.Sprintf(`为「%s」生成完整 5 分钟短剧剧本。目标时长 %.1f 分钟。

%s

骨架:
%s

策略:
%s

输出含：分场剧本、本集爽点、下集钩子。`, title, durationMin, contextText, skeleton, strategy)
}

func skeletonPrompt() string {
	if s := strings.TrimSpace(skill.File("prompts/script_skeleton.md")); s != "" {
		return s
	}
	return "请生成故事骨架，含六段节奏锚点与下集钩子。"
}

func strategyPrompt() string {
	if s := strings.TrimSpace(skill.File("prompts/script_strategy.md")); s != "" {
		return s
	}
	return "请生成改编策略，镜头配额 18–25。"
}
