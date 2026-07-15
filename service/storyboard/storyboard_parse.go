package storyboard

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"toonflow/task"
	"toonflow/service/internal/duration"
)

var (
	reShotHeader  = regexp.MustCompile(`(?i)^(?:#{1,3}\s*)?(?:\*\*)?(?:镜头|Shot|第\s*(\d+)\s*镜|镜\s*(\d+))`)
	reShotNum     = regexp.MustCompile(`(?i)(?:Shot|镜头)\s*(\d+)(?:-(\d+))?`)
	reSceneLine   = regexp.MustCompile(`(?i)^(?:#{1,3}\s*)?(?:\*\*)?(?:场景|Scene|场次)\s*\d*[:：\s]+(.+)`)
	reTableShotID = regexp.MustCompile(`(?i)^(?:VC|SC|S)?(\d+)$`)
	reActHeader   = regexp.MustCompile(`【([^】]+)】`)
	reVCInText    = regexp.MustCompile(`(?i)\bVC\d+\b`)
	reShotMarker  = regexp.MustCompile(`【\s*镜头\s*\d+`)
)

// NormalizeStoryboardItems fills defaults and fixes shot numbers.
func NormalizeStoryboardItems(items []task.StoryboardItem) []task.StoryboardItem {
	out := make([]task.StoryboardItem, 0, len(items))
	prevScene := ""
	for i, it := range items {
		if strings.TrimSpace(it.Description) == "" && strings.TrimSpace(it.Prompt) == "" && strings.TrimSpace(it.Scene) == "" {
			continue
		}
		if it.ShotNumber <= 0 {
			it.ShotNumber = i + 1
		}
		if it.Duration <= 0 {
			it.Duration = duration.DefaultShotDurationSec
		} else {
			it.Duration = duration.SnapPreferredShotDuration(it.Duration)
		}
		if it.Prompt == "" {
			it.Prompt = it.Description
		}
		if it.Description == "" {
			it.Description = it.Prompt
		}
		SyncSevenElements(&it)
		it.Dialogue = normalizeDialogue(it.Dialogue)
		it.SceneLink = resolveSceneLink(it.SceneLink, it.Scene, prevScene, len(out) == 0)
		it.Transition = resolveShotTransition(it.SceneLink, it.Transition)
		it.Beats = ensureShotBeats(it.Beats, it.Duration, it.Description)
		prevScene = strings.TrimSpace(it.Scene)
		out = append(out, it)
	}
	return out
}

// ensureShotBeats returns a cleaned intra-shot timed plan for Agnes keyframe video.
// Agnes accepts at most 3 keyframe images per generation; target 2–3 beats per shot.
func ensureShotBeats(beats []task.ShotBeat, dur float64, description string) []task.ShotBeat {
	if dur <= 0 {
		dur = duration.DefaultShotDurationSec
	}
	target := targetBeatCount(dur)
	cleaned := normalizeShotBeats(beats, dur)
	if len(cleaned) > duration.MaxBeatsPerShot {
		cleaned = downsampleBeats(cleaned, duration.MaxBeatsPerShot, dur)
	}
	if len(cleaned) >= target {
		return cleaned
	}
	return synthesizeShotBeats(cleaned, dur, description, target)
}

// CapShotBeats normalizes beat count for keyframe image/video generation (2–3 beats).
func CapShotBeats(beats []task.ShotBeat, dur float64, description string) []task.ShotBeat {
	return ensureShotBeats(beats, dur, description)
}

func targetBeatCount(dur float64) int {
	// 8–11s dialogue/setup → 2 keyframes; 12–15s conflict/twist → 3 (Agnes hard cap).
	if dur >= 12 {
		return duration.MaxBeatsPerShot
	}
	return duration.MinBeatsPerShot
}

func downsampleBeats(beats []task.ShotBeat, n int, dur float64) []task.ShotBeat {
	if n <= 0 || len(beats) == 0 {
		return nil
	}
	if len(beats) <= n {
		return beats
	}
	if n == 1 {
		return []task.ShotBeat{beats[0]}
	}
	out := make([]task.ShotBeat, 0, n)
	for i := 0; i < n; i++ {
		idx := int(float64(i)*float64(len(beats)-1)/float64(n-1) + 0.5)
		out = append(out, beats[idx])
	}
	out[0].Time = beats[0].Time
	out[len(out)-1].Time = beats[len(beats)-1].Time
	return normalizeShotBeats(out, dur)
}

func synthesizeShotBeats(seed []task.ShotBeat, dur float64, description string, target int) []task.ShotBeat {
	desc := strings.TrimSpace(description)
	if desc == "" {
		desc = "场景动作推进"
	}
	phases := []string{"开端铺垫", "动作推进", "情绪转折", "高潮爆发", "余韵收束", "结果落定", "画面定格"}
	out := make([]task.ShotBeat, 0, target)
	for i := 0; i < target; i++ {
		t := 0.0
		if target > 1 {
			t = dur * float64(i) / float64(target-1) * 0.92
		}
		action := desc + "，" + phases[i%len(phases)]
		// Map existing seed beats onto the denser grid by nearest time.
		if len(seed) > 0 {
			best := seed[0]
			bestDist := absFloat(seed[0].Time - t)
			for _, s := range seed[1:] {
				if d := absFloat(s.Time - t); d < bestDist {
					bestDist = d
					best = s
				}
			}
			// Prefer an explicit seed action when this slot is near a seed beat.
			span := dur / float64(target)
			if bestDist <= span*0.75 && strings.TrimSpace(best.Action) != "" {
				action = strings.TrimSpace(best.Action)
			}
		}
		out = append(out, task.ShotBeat{Time: t, Action: action})
	}
	return normalizeShotBeats(out, dur)
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func normalizeShotBeats(beats []task.ShotBeat, dur float64) []task.ShotBeat {
	if len(beats) == 0 {
		return nil
	}
	cleaned := make([]task.ShotBeat, 0, len(beats))
	for _, b := range beats {
		action := strings.TrimSpace(b.Action)
		if action == "" {
			continue
		}
		t := b.Time
		if t < 0 {
			t = 0
		}
		if t > dur {
			t = dur
		}
		cleaned = append(cleaned, task.ShotBeat{
			Time: t, Action: action, ImagePrompt: b.ImagePrompt,
			ImageURL: b.ImageURL, ImageRemoteURL: b.ImageRemoteURL,
		})
	}
	if len(cleaned) < 2 {
		return nil
	}
	sort.SliceStable(cleaned, func(i, j int) bool { return cleaned[i].Time < cleaned[j].Time })
	dedup := cleaned[:0]
	for i, b := range cleaned {
		if i > 0 && b.Time-dedup[len(dedup)-1].Time < 0.05 {
			b.Time = dedup[len(dedup)-1].Time + 0.05
		}
		dedup = append(dedup, b)
	}
	dedup[0].Time = 0
	return dedup
}

// NormalizeSceneLink maps a model-provided scene-link value to a canonical enum.
// Returns "" when the value is unrecognized so the caller can infer a default.
func NormalizeSceneLink(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	switch {
	case strings.Contains(s, "continu"), strings.Contains(s, "seamless"),
		strings.Contains(s, "same scene"), strings.Contains(s, "carry"),
		strings.Contains(s, "续接"), strings.Contains(s, "衔接"),
		strings.Contains(s, "同场景"), strings.Contains(s, "顺接"):
		return task.SceneLinkContinuous
	case strings.Contains(s, "transition"), strings.Contains(s, "cut"),
		strings.Contains(s, "new scene"), strings.Contains(s, "转场"),
		strings.Contains(s, "换场"), strings.Contains(s, "切换"),
		strings.Contains(s, "切镜"):
		return task.SceneLinkTransition
	}
	return ""
}

// resolveSceneLink returns the canonical scene link, inferring from scene changes
// when the model did not provide a usable value. The first shot is always a
// transition (no previous frame to continue from).
func resolveSceneLink(raw, scene, prevScene string, isFirst bool) string {
	if isFirst {
		return task.SceneLinkTransition
	}
	if v := NormalizeSceneLink(raw); v != "" {
		return v
	}
	if strings.TrimSpace(scene) != "" && strings.TrimSpace(scene) == strings.TrimSpace(prevScene) {
		return task.SceneLinkContinuous
	}
	return task.SceneLinkTransition
}

// resolveShotTransition fills default boundary effects for timeline export.
func resolveShotTransition(sceneLink, transition string) string {
	trans := strings.TrimSpace(transition)
	if sceneLink == task.SceneLinkContinuous {
		if trans == "" {
			return "soft dissolve"
		}
		return trans
	}
	if trans == "" {
		return "fade black"
	}
	return trans
}

// LoadStoryboardItems reads storyboard shots from DB for an episode.
func LoadStoryboardItems(db *sql.DB, projectID, episodeID string) ([]task.StoryboardItem, error) {
	if projectID == "" || episodeID == "" {
		return nil, fmt.Errorf("project_id and episode_id required")
	}
	sbID := fmt.Sprintf("sb_%s_%s", projectID, episodeID)
	var shotsJSON string
	err := db.QueryRow(`SELECT shots FROM o_storyboard WHERE id = ?`, sbID).Scan(&shotsJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var items []task.StoryboardItem
	if err := json.Unmarshal([]byte(shotsJSON), &items); err != nil {
		return nil, err
	}
	return NormalizeStoryboardItems(items), nil
}

// MergeStoryboardMedia keeps generated image fields when storyboard text is refreshed.
func MergeStoryboardMedia(existing, incoming []task.StoryboardItem) []task.StoryboardItem {
	if len(existing) == 0 || len(incoming) == 0 {
		return incoming
	}
	byShot := make(map[int]task.StoryboardItem, len(existing))
	for _, it := range existing {
		if it.ShotNumber > 0 {
			byShot[it.ShotNumber] = it
		}
	}
	out := make([]task.StoryboardItem, len(incoming))
	for i, it := range incoming {
		out[i] = it
		prev, ok := byShot[it.ShotNumber]
		if !ok {
			continue
		}
		if prev.ImageURL != "" {
			out[i].ImageURL = prev.ImageURL
		}
		if prev.ImageRemoteURL != "" {
			out[i].ImageRemoteURL = prev.ImageRemoteURL
		}
		if len(prev.AssetIDs) > 0 && len(out[i].AssetIDs) == 0 {
			out[i].AssetIDs = prev.AssetIDs
		}
	}
	return out
}

// ParseStoryboardResponse parses LLM or stored storyboard JSON/text into items.
func ParseStoryboardResponse(text string) ([]task.StoryboardItem, error) {
	text = strings.TrimSpace(text)

	// Preferred: provider JSON mode returns an object wrapper {"shots":[...]}.
	if wrapped := parseWrappedStoryboard(text); len(wrapped) > 0 {
		return NormalizeStoryboardItems(wrapped), nil
	}

	var items []task.StoryboardItem
	if err := json.Unmarshal([]byte(text), &items); err == nil && len(items) > 0 {
		return NormalizeStoryboardItems(items), nil
	}

	if start := strings.Index(text, "["); start != -1 {
		if end := strings.LastIndex(text, "]"); end > start {
			if err := json.Unmarshal([]byte(text[start:end+1]), &items); err == nil && len(items) > 0 {
				return NormalizeStoryboardItems(items), nil
			}
		}
	}

	items = parseTableStoryboard(text)
	if len(items) > 0 {
		return NormalizeStoryboardItems(items), nil
	}

	items = parseMarkdownShots(text)
	if len(items) > 0 {
		return NormalizeStoryboardItems(items), nil
	}

	items = fallbackParseShots(text)
	return NormalizeStoryboardItems(items), nil
}

// parseWrappedStoryboard extracts shots from an object wrapper the model returns
// under JSON mode, e.g. {"shots":[...]} (also tolerates "storyboard"/"items" keys).
func parseWrappedStoryboard(text string) []task.StoryboardItem {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil
	}
	var wrapper struct {
		Shots      []task.StoryboardItem `json:"shots"`
		Storyboard []task.StoryboardItem `json:"storyboard"`
		Items      []task.StoryboardItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &wrapper); err != nil {
		return nil
	}
	switch {
	case len(wrapper.Shots) > 0:
		return wrapper.Shots
	case len(wrapper.Storyboard) > 0:
		return wrapper.Storyboard
	case len(wrapper.Items) > 0:
		return wrapper.Items
	}
	return nil
}

func parseMarkdownShots(text string) []task.StoryboardItem {
	var items []task.StoryboardItem
	var current *task.StoryboardItem
	shotNum := 0
	currentScene := ""

	flush := func() {
		if current == nil {
			return
		}
		if current.Scene == "" {
			current.Scene = currentScene
		}
		items = append(items, *current)
		current = nil
	}

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "---") {
			continue
		}
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.Trim(line, "*")

		if m := reSceneLine.FindStringSubmatch(line); len(m) > 1 {
			currentScene = strings.TrimSpace(m[1])
			continue
		}

		if isShotHeader(line) {
			flush()
			shotNum++
			if n := extractShotNumber(line); n > 0 {
				shotNum = n
			}
			current = &task.StoryboardItem{
				ShotNumber: shotNum,
				Scene:      currentScene,
				Duration:   3,
			}
			if rest := shotHeaderRemainder(line); rest != "" {
				current.Description = rest
			}
			continue
		}

		if current == nil {
			if strings.HasPrefix(line, "#") || strings.Contains(strings.ToLower(line), "storyboard breakdown") {
				continue
			}
			continue
		}

		if idx := strings.IndexAny(line, ":："); idx != -1 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			switch strings.ToLower(key) {
			case "场景", "scene":
				current.Scene = val
			case "描述", "description", "画面", "画面描述", "visual":
				current.Description = val
			case "镜头", "camera", "运镜":
				current.Camera = val
			case "时长", "duration":
				fmtScanFloat(val, &current.Duration)
			case "对白", "dialogue", "台词", "audio", "音效", "音效/台词":
				current.Dialogue = ParseDialogueFlexible(val)
			case "prompt", "绘画", "绘画prompt", "ai prompt":
				current.Prompt = val
			default:
				current.Description = strings.TrimSpace(current.Description + " " + line)
			}
		} else {
			current.Description = strings.TrimSpace(current.Description + " " + line)
		}
	}
	flush()
	return items
}

func isShotHeader(line string) bool {
	if reShotHeader.MatchString(line) {
		return true
	}
	return reShotNum.MatchString(line)
}

func extractShotNumber(line string) int {
	if m := reShotNum.FindStringSubmatch(line); len(m) > 1 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	if m := reShotHeader.FindStringSubmatch(line); len(m) > 1 {
		for i := 1; i < len(m); i++ {
			if m[i] != "" {
				n, _ := strconv.Atoi(m[i])
				if n > 0 {
					return n
				}
			}
		}
	}
	return 0
}

func shotHeaderRemainder(line string) string {
	if idx := strings.IndexAny(line, ":："); idx != -1 {
		return strings.TrimSpace(line[idx+1:])
	}
	if m := reShotNum.FindStringIndex(line); m != nil {
		return strings.TrimSpace(line[m[1]:])
	}
	return ""
}

func fmtScanFloat(val string, out *float64) {
	f, err := strconv.ParseFloat(strings.TrimSuffix(strings.Fields(val)[0], "s"), 64)
	if err == nil {
		*out = f
	}
}

func fallbackParseShots(text string) []task.StoryboardItem {
	if md := parseMarkdownShots(text); len(md) > 0 {
		return md
	}

	var items []task.StoryboardItem
	lines := strings.Split(text, "\n")
	var current *task.StoryboardItem
	shotNum := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if isShotHeader(line) {
			if current != nil {
				items = append(items, *current)
			}
			shotNum++
			if n := extractShotNumber(line); n > 0 {
				shotNum = n
			}
			current = &task.StoryboardItem{ShotNumber: shotNum, Duration: 3.0}
			if rest := shotHeaderRemainder(line); rest != "" {
				current.Description = rest
			}
			continue
		}

		if current == nil {
			continue
		}

		if idx := strings.IndexAny(line, ":："); idx != -1 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			switch key {
			case "场景", "Scene":
				current.Scene = val
			case "描述", "Description":
				current.Description = val
			case "镜头", "Camera":
				current.Camera = val
			case "时长", "Duration":
				fmtScanFloat(val, &current.Duration)
			case "Prompt", "prompt":
				current.Prompt = val
			default:
				current.Description = strings.TrimSpace(current.Description + " " + line)
			}
		} else {
			current.Description = strings.TrimSpace(current.Description + " " + line)
		}
	}
	if current != nil {
		items = append(items, *current)
	}
	if len(items) == 0 {
		if reVCInText.MatchString(text) || strings.Contains(text, "镜头号") {
			return items
		}
		items = append(items, task.StoryboardItem{ShotNumber: 1, Description: text, Duration: 3.0, Prompt: text})
	}
	return items
}

// LooksLikeStoryboardTable detects markdown table storyboard scripts (VC01, etc.).
func LooksLikeStoryboardTable(text string) bool {
	return strings.Contains(text, "|") && (reVCInText.MatchString(text) || strings.Contains(text, "镜头号"))
}

// MinShotsForScript estimates shot count for a 5-minute episode (target 18–25).
// Prefer clear information-advancing shots over overcut one-liners.
func MinShotsForScript(script string) int {
	script = strings.TrimSpace(script)
	if script == "" {
		return 6
	}

	if markers := len(reShotMarker.FindAllStringIndex(script, -1)); markers >= 12 {
		if markers > duration.TargetShotsMax {
			return duration.TargetShotsMax
		}
		return markers
	}

	runeCount := len([]rune(script))
	byLength := runeCount / 160
	switch {
	case runeCount < 400:
		if byLength < 6 {
			byLength = 6
		}
	case runeCount < 1200:
		if byLength < 12 {
			byLength = 12
		}
	default:
		if byLength < duration.TargetShotsMin {
			byLength = duration.TargetShotsMin
		}
	}
	if byLength > duration.TargetShotsMax {
		byLength = duration.TargetShotsMax
	}

	sceneCount := 0
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if reSceneLine.MatchString(line) {
			sceneCount++
			continue
		}
		if strings.HasPrefix(line, "###") && (strings.Contains(line, "场") || strings.Contains(line, "钩子") || strings.Contains(line, "升级") || strings.Contains(line, "反转") || strings.Contains(line, "高潮")) {
			sceneCount++
			continue
		}
		if strings.HasPrefix(line, "【") && (strings.Contains(line, "场") || strings.Contains(line, "幕")) {
			sceneCount++
		}
	}
	// Six-act scripts often need ~3–4 shots per act → prefer higher of length vs scene*3.
	if sceneCount >= 4 {
		fromScenes := sceneCount * 3
		if fromScenes > byLength {
			byLength = fromScenes
		}
		if byLength > duration.TargetShotsMax {
			byLength = duration.TargetShotsMax
		}
		if byLength < duration.TargetShotsMin && runeCount >= 1200 {
			byLength = duration.TargetShotsMin
		}
	}
	return byLength
}

// IsAdequateStoryboard reports whether items meet the minimum shot count for a script.
func IsAdequateStoryboard(items []task.StoryboardItem, minShots int) bool {
	if minShots <= 0 {
		minShots = 3
	}
	if len(items) < minShots {
		return false
	}
	if len(items) == 1 {
		d := strings.ToLower(items[0].Description)
		if strings.Contains(d, "storyboard breakdown") || strings.Contains(d, "shot # | scene") {
			return false
		}
	}
	return true
}

// StoryboardScore ranks parse quality; higher is better.
// Prefer fewer dense long-shots (multi-beat coverage) over many thin short shots.
func StoryboardScore(items []task.StoryboardItem) int {
	if len(items) == 0 {
		return 0
	}
	if len(items) == 1 {
		d := strings.ToLower(items[0].Description)
		if strings.Contains(d, "storyboard breakdown") || strings.Contains(d, "shot # | scene") {
			return 1
		}
		return 2
	}
	score := 0
	totalBeats := 0
	for _, it := range items {
		score += 15
		if it.Duration >= duration.MinShotDurationSec {
			score += int(it.Duration)
		}
		n := len(it.Beats)
		totalBeats += n
		switch {
		case n == duration.MaxBeatsPerShot:
			score += n * 10
		case n == duration.MinBeatsPerShot:
			score += n * 6
		case n > duration.MaxBeatsPerShot:
			score -= (n - duration.MaxBeatsPerShot) * 15
		case n >= 2:
			score += n * 2
		case n == 0 && it.Duration >= duration.MinShotDurationSec:
			score -= 12
		}
	}
	if len(items) > 0 && totalBeats > 0 {
		score += (totalBeats * 4) / len(items)
	}
	// Strong penalty for over-fragmentation under the long-shot strategy.
	if len(items) > 8 {
		score -= (len(items) - 8) * 25
	}
	return score
}

// PickBestStoryboard chooses the highest-quality candidate.
func PickBestStoryboard(candidates ...[]task.StoryboardItem) []task.StoryboardItem {
	var best []task.StoryboardItem
	for _, c := range candidates {
		c = NormalizeStoryboardItems(c)
		if StoryboardScore(c) > StoryboardScore(best) {
			best = c
		}
	}
	return best
}

func parseTableStoryboard(text string) []task.StoryboardItem {
	if !strings.Contains(text, "|") {
		return nil
	}

	var items []task.StoryboardItem
	currentScene := ""
	colMap := tableColumnMap{}

	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		if m := reActHeader.FindStringSubmatch(line); len(m) > 1 && (strings.Contains(line, "幕") || strings.Contains(line, "SCENE") || strings.Contains(line, "Scene")) {
			currentScene = cleanMarkdown(m[1])
			continue
		}

		if !strings.HasPrefix(line, "|") {
			continue
		}
		if strings.Contains(line, ":---") || strings.Contains(line, "|:---") {
			continue
		}

		cells := splitTableCells(line)
		if len(cells) < 3 {
			continue
		}

		if isTableHeaderRow(cells) {
			colMap = detectTableColumns(cells)
			continue
		}

		shotID := cleanMarkdown(cells[0])
		if !isTableShotCell(shotID) {
			continue
		}

		shotNum := extractTableShotNumber(shotID)
		if shotNum <= 0 {
			shotNum = len(items) + 1
		}

		descIdx, camIdx, dlgIdx, durIdx := tableColumnIndexes(cells, colMap)
		desc := cleanMarkdown(cells[descIdx])
		if descIdx != 1 && len(cells) > 1 && colMap.desc < 0 {
			shotType := cleanMarkdown(cells[1])
			if shotType != "" && !strings.EqualFold(shotType, shotID) {
				desc = strings.TrimSpace(shotType + " — " + desc)
			}
		}

		camera := ""
		if camIdx >= 0 && camIdx < len(cells) {
			camera = cleanMarkdown(cells[camIdx])
		}
		duration := 3.0
		if durIdx >= 0 && durIdx < len(cells) {
			fmtScanFloat(cleanMarkdown(cells[durIdx]), &duration)
		}

		var dlg *task.ShotDialogue
		if dlgIdx >= 0 && dlgIdx < len(cells) {
			dlg = ParseDialogueFlexible(cleanMarkdown(cells[dlgIdx]))
		}

		items = append(items, task.StoryboardItem{
			ShotNumber:  shotNum,
			Scene:       currentScene,
			Description: desc,
			Camera:      camera,
			Dialogue:    dlg,
			Duration:    duration,
			Prompt:      desc,
		})
	}
	return items
}

type tableColumnMap struct {
	desc, cam, dialogue, dur int
}

func isTableHeaderRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	first := strings.ToLower(cleanMarkdown(cells[0]))
	return strings.Contains(first, "镜头") || strings.Contains(first, "shot")
}

func detectTableColumns(cells []string) tableColumnMap {
	m := tableColumnMap{desc: -1, cam: -1, dialogue: -1, dur: -1}
	for i, c := range cells {
		lower := strings.ToLower(cleanMarkdown(c))
		switch {
		case strings.Contains(lower, "画面") || strings.Contains(lower, "visual") || strings.Contains(lower, "描述"):
			m.desc = i
		case strings.Contains(lower, "运镜") || strings.Contains(lower, "camera"):
			m.cam = i
		case strings.Contains(lower, "对白") || strings.Contains(lower, "台词") || strings.Contains(lower, "audio") || strings.Contains(lower, "音效"):
			m.dialogue = i
		case strings.Contains(lower, "时长") || strings.Contains(lower, "duration"):
			m.dur = i
		}
	}
	return m
}

func splitTableCells(line string) []string {
	line = strings.Trim(line, "|")
	parts := strings.Split(line, "|")
	cells := make([]string, 0, len(parts))
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}

func isTableShotCell(cell string) bool {
	cell = strings.TrimSpace(cell)
	if cell == "" {
		return false
	}
	lower := strings.ToLower(cell)
	if strings.Contains(lower, "镜头号") || strings.Contains(lower, "shot #") || strings.Contains(lower, "shot number") {
		return false
	}
	if reVCInText.MatchString(cell) {
		return true
	}
	if reTableShotID.MatchString(strings.ToUpper(strings.Trim(cell, "*"))) {
		return true
	}
	if matched, _ := regexp.MatchString(`(?i)^(?:镜头|shot)\s*\d+`, cell); matched {
		return true
	}
	return false
}

func extractTableShotNumber(id string) int {
	id = strings.Trim(id, "*")
	if m := regexp.MustCompile(`(?i)(?:VC|SC|S)?(\d+)`).FindStringSubmatch(id); len(m) > 1 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

func tableColumnIndexes(cells []string, colMap tableColumnMap) (descIdx, camIdx, dlgIdx, durIdx int) {
	descIdx = colMap.desc
	camIdx = colMap.cam
	dlgIdx = colMap.dialogue
	durIdx = colMap.dur
	if descIdx < 0 {
		descIdx = 2
		if len(cells) <= 4 {
			descIdx = 1
		}
	}
	if camIdx < 0 {
		camIdx = 3
		if len(cells) <= 4 {
			camIdx = 2
		}
	}
	if dlgIdx < 0 && len(cells) >= 5 {
		dlgIdx = len(cells) - 2
	}
	if durIdx < 0 {
		durIdx = len(cells) - 1
	}
	return descIdx, camIdx, dlgIdx, durIdx
}

func cleanMarkdown(s string) string {
	s = strings.ReplaceAll(s, "<br>", " ")
	s = strings.ReplaceAll(s, "<br/>", " ")
	s = regexp.MustCompile(`\*+`).ReplaceAllString(s, "")
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}
