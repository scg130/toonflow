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

// buildShotVideoPrompt builds an I2V prompt using industrial short-drama method:
// lock still keyframes first, then describe ONLY the motion between frames (not literary essays).
// Models remain the project's existing Agnes text/image/video stack.
func buildShotVideoPrompt(shot *storyboard.ShotMeta, artStyle, stylePrompt, styleAnchor string, humanSubject bool) (string, string) {
	mode := ClassifyShotVideoMode(shot)
	return buildShotVideoPromptWithMode(shot, mode, artStyle, stylePrompt, styleAnchor, humanSubject)
}

func buildShotVideoPromptWithMode(shot *storyboard.ShotMeta, mode VideoMode, artStyle, stylePrompt, styleAnchor string, humanSubject bool) (string, string) {
	parts := make([]string, 0, 24)
	parts = append(parts, videoI2VLines("positive_locks", []string{
		"image1 is first frame lock, imageN is last frame target",
		"generate only continuous motion between locked frames",
		"preserve subject identity, face structure, outfit, hairstyle, and scene layout",
	})...)

	beats := BeatActionsForMode(shot.Beats, mode)
	if motion := formatInterKeyframeMotion(beats, shot.Duration, mode); motion != "" {
		parts = append(parts, motion)
	} else if m := compressDescriptionForVideo(shot.Description); m != "" {
		parts = append(parts, "this clip only physical action: "+m)
	}

	parts = append(parts, videoI2VLines("motion_tail", []string{
		"one physical action path, no hard cuts inside the clip",
		"end pose must land on the last keyframe",
	})...)

	if ac := compressDescriptionForVideo(shot.ActionContinue); ac != "" && utf8.RuneCountInString(ac) <= 60 {
		if !isPlaceholderContinuity(ac) {
			parts = append(parts, "handoff from previous ending: "+ac)
		}
	}

	lines := storyboard.DialogueLinesForTTS(shot.Dialogue)
	parts = appendDialogueVideoInstructions(parts, lines, humanSubject)

	if cam := camera.MapCameraToVideoMotion(shot.Camera); cam != "" {
		parts = append(parts, cam)
	} else if humanSubject {
		parts = append(parts, videoI2VOneLine("camera_default_human", "one slow vertical short-drama push-in on face"))
	} else {
		parts = append(parts, videoI2VOneLine("camera_default_prop", "locked or one motivated vertical short-drama camera move"))
	}

	if len(parts) < 5 {
		if trimmed := trimImagePromptForVideo(shot.Prompt); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}

	if lit := compressLightingForVideo(shot.Lighting); lit != "" {
		parts = append(parts, "lighting continuity: "+lit)
	}

	parts = append(parts, hongguoVideoStyleTags(artStyle, stylePrompt)...)
	parts = append(parts, videoI2VLines("clip_tail", []string{
		"silent video no generated speech",
		"Chinese drama visuals only",
		"smooth temporal interpolation",
		"frame-to-frame continuity",
	})...)
	if mode == VideoModeFrames2 {
		parts = append(parts, videoI2VOneLine("mode_frames2", "FLF2V two-frame morph first-to-last only"))
	} else {
		parts = append(parts, videoI2VOneLine("mode_multiframe", "multi-keyframe continuous action take"))
	}
	if !humanSubject {
		parts = append(parts, videoI2VOneLine("non_human_tail", "no human character motion, object and environment only"))
	}

	negative := videoI2VCSV("negative", "static image, frozen frame, morphing, flicker, identity drift, cinematic, emotional")
	if humanSubject && hasSpeakableLines(lines) {
		if lip := videoI2VCSV("negative_lip_sync", "closed mouth while speaking, no lip sync"); lip != "" {
			negative += ", " + lip
		}
	}
	_ = styleAnchor

	return strings.Join(parts, ", "), negative
}

// formatInterKeyframeMotion describes physical change BETWEEN locked stills (not a prose summary).
func formatInterKeyframeMotion(beats []task.ShotBeat, dur float64, mode VideoMode) string {
	if len(beats) == 0 {
		return ""
	}
	type node struct {
		t float64
		a string
	}
	nodes := make([]node, 0, len(beats))
	for _, b := range beats {
		a := compressBeatActionForVideo(b.Action)
		if a == "" {
			continue
		}
		nodes = append(nodes, node{t: b.Time, a: a})
	}
	if len(nodes) == 0 {
		return ""
	}
	durHint := ""
	if dur > 0 {
		durHint = fmt.Sprintf(" over %.1fs", dur)
	}
	if mode == VideoModeFrames2 || len(nodes) == 1 {
		if len(nodes) == 1 {
			return "image-to-video motion" + durHint + ": animate from locked start frame — " + nodes[0].a
		}
		return "FLF2V motion" + durHint + ": start[" + nodes[0].a + "] → end[" + nodes[len(nodes)-1].a + "]; only the physical transition between these two locked frames"
	}
	parts := make([]string, 0, len(nodes))
	for _, n := range nodes {
		parts = append(parts, fmt.Sprintf("[%.1fs] %s", n.t, n.a))
	}
	return "multiframe motion" + durHint + ": " + strings.Join(parts, " → ") + "; morph through keyframes in order, hold pose continuity"
}

// hongguoVideoStyleTags returns vertical short-drama look tags for I2V (not still-image render jargon).
func hongguoVideoStyleTags(artStyle, stylePrompt string) []string {
	tags := append([]string{}, videoI2VLines("style_tags", []string{
		"Chinese vertical short drama style",
		"Hongguo Douyin short-series look",
		"9:16 vertical framing",
	})...)
	if s := strings.TrimSpace(artStyle); s != "" {
		tags = append(tags, s+" motion style")
	}
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
	moodOnly := videoI2VLines("literary_mood_only", []string{
		"悲愤欲绝", "几近破碎", "情绪崩溃", "滔天怒火", "杀意沸腾", "心境崩塌",
		"威压", "神念", "气势逼人", "无风起浪", "氛围压抑", "沉重气氛",
	})
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
	concrete := videoI2VLines("concrete_verbs", []string{"抬", "跪", "推", "抓", "碎", "裂", "站", "冲", "握", "泪", "瞳", "发", "光", "灰", "拳", "吼"})
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
	for _, w := range videoI2VLines("anti_slop", []string{"电影感", "氛围感", "cinematic", "epic", "dramatic", "beautiful"}) {
		action = strings.ReplaceAll(action, w, "")
	}
	action = strings.TrimSpace(action)
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
	added := 0
	for _, dialogue := range lines {
		if dialogue.Ignorable || strings.TrimSpace(dialogue.PureText) == "" {
			continue
		}
		if !isSpeakableVideoLine(dialogue.PureText) {
			continue
		}
		speaker := strings.TrimSpace(dialogue.Speaker)
		if speaker == "" {
			speaker = "角色"
		}
		line := truncateDialogueForVideoPrompt(dialogue.PureText, 24)
		tpls := videoI2VLines("dialogue_line", nil)
		if len(tpls) >= 2 && strings.Count(tpls[0], "%s") == 2 && strings.Count(tpls[1], "%s") == 1 {
			parts = append(parts, fmt.Sprintf(tpls[0], speaker, line), fmt.Sprintf(tpls[1], speaker))
		} else {
			parts = append(parts,
				fmt.Sprintf("%s近景张嘴说短句，下颌开合清晰：%s", speaker, line),
				fmt.Sprintf("%s唇形随字咬合开合，眉头与下颌同步位移", speaker),
			)
		}
		added++
	}
	if added == 0 {
		return parts
	}
	parts = append(parts, videoI2VLines("dialogue_tail", []string{
		"仅口型与肢体表演，视频禁止生成任何语音",
		"无声画面，不要英文对白音频",
	})...)
	return parts
}

// isSpeakableVideoLine rejects junk edits (digits-only, latin spam) that poison I2V lip-sync.
func isSpeakableVideoLine(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	han := 0
	digit := 0
	letter := 0
	for _, r := range text {
		switch {
		case r >= '0' && r <= '9':
			digit++
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			letter++
		case r >= 0x4e00 && r <= 0x9fff:
			han++
		}
	}
	if han == 0 {
		return false
	}
	if digit > han {
		return false
	}
	if letter > 2*han {
		return false
	}
	return true
}

func isPlaceholderContinuity(s string) bool {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "无")
	s = strings.Trim(s, "（）() ：:")
	s = strings.TrimSpace(s)
	switch s {
	case "", "开场", "无", "无（开场）", "开场。":
		return true
	}
	return strings.HasPrefix(s, "开场") && utf8.RuneCountInString(s) <= 6
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
