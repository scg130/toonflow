package media

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"toonflow/service/internal/camera"
	"toonflow/service/storyboard"
	"toonflow/task"
)

var (
	storyboardLabelRE = regexp.MustCompile(`【[^】]{1,12}】`)
	beatSectionRE     = regexp.MustCompile(`(?i)(画面|动作|反应)\s*[：:]`)
)

// buildShotVideoPrompt builds an I2V prompt optimized for 红果/抖音竖屏短剧观感：
// punchy close-ups, clear physical action, aggressive camera — not soft cinematic essays.
func buildShotVideoPrompt(shot *storyboard.ShotMeta, artStyle, stylePrompt, styleAnchor string, humanSubject bool) (string, string) {
	parts := make([]string, 0, 16)

	// 1) Motion plan first (AI video models follow action nodes, not literary prose).
	if seq := formatBeatsForVideoPrompt(shot.Beats, shot.Duration); seq != "" {
		parts = append(parts, seq)
		parts = append(parts,
			"one continuous take, clear beat-to-beat action progression",
			"strong character performance, readable body language",
		)
	} else if motion := compressDescriptionForVideo(shot.Description); motion != "" {
		parts = append(parts, motion)
	}

	// 2) Continuity hook — keep short.
	if ac := compressDescriptionForVideo(shot.ActionContinue); ac != "" && utf8.RuneCountInString(ac) <= 60 {
		parts = append(parts, "from previous: "+ac)
	}

	// 3) Dialogue = lip/face performance only (audio from TTS later).
	lines := storyboard.DialogueLinesForTTS(shot.Dialogue)
	parts = appendDialogueVideoInstructions(parts, lines, humanSubject)

	// 4) Punchy short-drama camera (never default to soft cinematic).
	if cam := camera.MapCameraToVideoMotion(shot.Camera); cam != "" {
		parts = append(parts, cam)
	} else if humanSubject {
		parts = append(parts, "aggressive vertical short-drama push-in on face, emotional close-up")
	} else {
		parts = append(parts, "dynamic vertical short-drama camera, motivated environmental motion")
	}

	// Fallback image prompt crumbs only if almost empty.
	if len(parts) < 2 {
		if trimmed := trimImagePromptForVideo(shot.Prompt); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}

	// 5) Lighting: keep concrete, drop abstract mood words.
	if lit := compressLightingForVideo(shot.Lighting); lit != "" {
		parts = append(parts, "lighting: "+lit)
	}

	// 6) 红果短剧视觉 DNA（图生视频专用，不要塞 UE5/Octane 静帧标签）.
	parts = append(parts, hongguoVideoStyleTags(artStyle, stylePrompt)...)

	// 7) Silent video + temporal coherence (short).
	parts = append(parts,
		"silent video no generated speech",
		"Chinese drama visuals only",
		"smooth keyframe interpolation",
		"frame-to-frame continuity",
		"high clarity facial performance",
	)

	if !humanSubject {
		parts = append(parts, "no human character motion, object and environment only")
	}

	negative := strings.Join([]string{
		"static image", "frozen frame", "slideshow", "still photo", "no motion", "boring slow motion",
		"soft dreamy cinematic essay", "empty atmosphere shot", "vague mood without action",
		"morphing", "warping", "flickering", "jitter", "stuttering", "low fps",
		"blurry", "out of focus", "low quality", "low resolution",
		"distorted face", "deformed body", "bad anatomy", "extra limbs",
		"watermark", "text overlay", "logo", "subtitle",
		"random color shift", "style drift", "temporal discontinuity",
		"English speech", "English dialogue", "foreign language audio",
		"voiceover", "narration", "spoken words", "talking audio",
		"action freeze mid-motion", "discontinuous movement",
		"overstacked VFX particles without story", "generic fantasy MV montage",
	}, ", ")
	if humanSubject && hasSpeakableLines(lines) {
		negative += ", closed mouth while speaking, static lips during dialogue, no lip sync, mute expression while talking, wrong speaker lip movement"
	}
	_ = styleAnchor // intentionally unused: UE5/style_anchor hurts I2V; use hongguoVideoStyleTags instead

	return strings.Join(parts, ", "), negative
}

// hongguoVideoStyleTags returns vertical short-drama look tags for I2V (not still-image render jargon).
func hongguoVideoStyleTags(artStyle, stylePrompt string) []string {
	tags := []string{
		"Chinese vertical short drama style",
		"Hongguo Douyin short-series look",
		"9:16 vertical framing",
		"tight emotional close-up priority",
		"high contrast punchy color",
		"dramatic rim light",
		"clear facial micro-expression",
		"fast emotional beats",
		"commercial short-drama production value",
	}
	if s := strings.TrimSpace(artStyle); s != "" {
		tags = append(tags, s+" motion style")
	}
	// Keep only a short non-jargon crumb from art style prompt if present.
	if crumb := trimImagePromptForVideo(stylePrompt); crumb != "" && utf8.RuneCountInString(crumb) <= 40 {
		tags = append(tags, crumb)
	}
	return tags
}

// compressDescriptionForVideo strips literary labels and keeps physical action sentences.
func compressDescriptionForVideo(desc string) string {
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return ""
	}
	desc = storyboardLabelRE.ReplaceAllString(desc, "")
	desc = strings.ReplaceAll(desc, "【", "")
	desc = strings.ReplaceAll(desc, "】", "")
	// Prefer the first concrete sentence; drop empty fragments.
	parts := strings.FieldsFunc(desc, func(r rune) bool {
		return r == '。' || r == '；' || r == '\n' || r == '!' || r == '！'
	})
	kept := make([]string, 0, 2)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if isLiteraryMoodOnly(p) {
			continue
		}
		kept = append(kept, p)
		if len(kept) >= 2 {
			break
		}
	}
	out := strings.Join(kept, "。")
	if out == "" {
		out = strings.TrimSpace(desc)
	}
	if utf8.RuneCountInString(out) > 120 {
		out = string([]rune(out)[:120]) + "…"
	}
	return out
}

func isLiteraryMoodOnly(s string) bool {
	moodOnly := []string{
		"悲愤欲绝", "几近破碎", "情绪崩溃", "滔天怒火", "杀意沸腾", "心境崩塌",
		"威压", "神念", "气势逼人", "无风起浪", "氛围压抑", "沉重气氛",
	}
	hasMood := false
	for _, m := range moodOnly {
		if strings.Contains(s, m) {
			hasMood = true
			break
		}
	}
	if !hasMood {
		return false
	}
	// If it also has a concrete verb/object change, keep it.
	concrete := []string{"抬", "跪", "推", "抓", "碎", "裂", "站", "冲", "握", "泪", "瞳", "发", "光", "灰", "拳", "吼"}
	for _, c := range concrete {
		if strings.Contains(s, c) {
			return false
		}
	}
	return true
}

func compressLightingForVideo(lit string) string {
	lit = strings.TrimSpace(lit)
	if lit == "" {
		return ""
	}
	lit = storyboardLabelRE.ReplaceAllString(lit, "")
	if utf8.RuneCountInString(lit) > 48 {
		lit = string([]rune(lit)[:48])
	}
	return lit
}

// formatBeatsForVideoPrompt renders an intra-shot timed action plan as an explicit
// time-node instruction so a single generation animates the whole sequence in order.
func formatBeatsForVideoPrompt(beats []task.ShotBeat, dur float64) string {
	if len(beats) < 2 {
		return ""
	}
	nodes := make([]string, 0, len(beats))
	for _, b := range beats {
		action := compressBeatActionForVideo(b.Action)
		if action == "" {
			continue
		}
		nodes = append(nodes, fmt.Sprintf("[%.1fs] %s", b.Time, action))
	}
	if len(nodes) < 2 {
		return ""
	}
	header := "timed action beats"
	if dur > 0 {
		header = fmt.Sprintf("timed action beats over %.1fs", dur)
	}
	return header + ": " + strings.Join(nodes, "; ") + "; continuous motion between beats, no cuts"
}

func compressBeatActionForVideo(action string) string {
	action = strings.TrimSpace(action)
	if action == "" {
		return ""
	}
	action = storyboardLabelRE.ReplaceAllString(action, "")
	// Normalize "画面：… 动作：… 反应：…" into a compact physical chain.
	if beatSectionRE.MatchString(action) {
		action = beatSectionRE.ReplaceAllStringFunc(action, func(m string) string {
			m = strings.TrimSpace(m)
			m = strings.TrimSuffix(m, "：")
			m = strings.TrimSuffix(m, ":")
			return " → " + m + ":"
		})
		action = strings.TrimPrefix(strings.TrimSpace(action), "→ ")
		action = strings.ReplaceAll(action, "  ", " ")
	}
	if utf8.RuneCountInString(action) > 90 {
		action = string([]rune(action)[:90]) + "…"
	}
	return action
}

func resolveShotDialogue(shot *storyboard.ShotMeta) []storyboard.ParsedDialogue {
	if shot == nil {
		return nil
	}
	return storyboard.DialogueLinesForTTS(shot.Dialogue)
}

func hasSpeakableLines(lines []storyboard.ParsedDialogue) bool {
	for _, ln := range lines {
		if !ln.Ignorable && strings.TrimSpace(ln.PureText) != "" {
			return true
		}
	}
	return false
}

func appendDialogueVideoInstructions(parts []string, lines []storyboard.ParsedDialogue, humanSubject bool) []string {
	if !humanSubject || !hasSpeakableLines(lines) {
		return parts
	}
	for _, dialogue := range lines {
		if dialogue.Ignorable || strings.TrimSpace(dialogue.PureText) == "" {
			continue
		}
		speaker := strings.TrimSpace(dialogue.Speaker)
		if speaker == "" {
			speaker = "角色"
		}
		line := truncateDialogueForVideoPrompt(dialogue.PureText, 24)
		parts = append(parts,
			fmt.Sprintf("%s近景口型表演，短句气声/怒吼：%s", speaker, line),
			fmt.Sprintf("%s说话时唇形与下颌清晰运动，表情夸张可读", speaker),
		)
	}
	parts = append(parts,
		"仅口型与肢体表演，视频禁止生成任何语音",
		"无声画面，不要英文对白音频",
	)
	return parts
}

func truncateDialogueForVideoPrompt(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" || maxRunes <= 0 {
		return text
	}
	if utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	return string([]rune(text)[:maxRunes]) + "…"
}

func trimImagePromptForVideo(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	if utf8.RuneCountInString(prompt) > 200 {
		prompt = string([]rune(prompt)[:200])
	}
	stripTerms := []string{
		"unreal engine", "octane render", "ambient occlusion", "subsurface scattering",
		"pbr", "8k", "global illumination", "volumetric", "bokeh", "character_id",
		"style: consistent", "high fidelity", "consistent lighting", "consistent character",
		"widescreen", "vertical", "unified color", "global style embedding",
		"zero model mutation", "cinematic color grade",
	}
	segments := strings.Split(prompt, ",")
	kept := make([]string, 0, 4)
	for _, seg := range segments {
		s := strings.TrimSpace(seg)
		if s == "" {
			continue
		}
		lower := strings.ToLower(s)
		skip := false
		for _, t := range stripTerms {
			if strings.Contains(lower, t) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		kept = append(kept, s)
		if len(kept) >= 3 {
			break
		}
	}
	return strings.Join(kept, ", ")
}
