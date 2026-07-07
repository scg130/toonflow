package storyboard

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
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
)

// NormalizeStoryboardItems fills defaults and fixes shot numbers.
func NormalizeStoryboardItems(items []task.StoryboardItem) []task.StoryboardItem {
	out := make([]task.StoryboardItem, 0, len(items))
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
			it.Duration = duration.ResolveShotVideoDuration(it.Duration)
		}
		if it.Prompt == "" {
			it.Prompt = it.Description
		}
		if it.Description == "" {
			it.Description = it.Prompt
		}
		if it.Camera == "" {
			it.Camera = "固定镜头"
		}
		if strings.TrimSpace(it.Dialogue) == "" {
			if dlg := ExtractDialogueFromDescription(it.Description); dlg != "" {
				it.Dialogue = dlg
			}
		}
		out = append(out, it)
	}
	return out
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
				current.Dialogue = val
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

// MinShotsForScript estimates how many shots a script should yield.
func MinShotsForScript(script string) int {
	script = strings.TrimSpace(script)
	if script == "" {
		return 3
	}
	runeCount := len([]rune(script))
	byLength := runeCount / 180
	if byLength < 4 {
		byLength = 4
	}
	if byLength > 30 {
		byLength = 30
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
		if strings.HasPrefix(line, "【") && (strings.Contains(line, "场") || strings.Contains(line, "幕")) {
			sceneCount++
		}
	}
	if sceneCount >= byLength {
		return sceneCount
	}
	if sceneCount >= 3 {
		return sceneCount
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
	return len(items) * 10
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
		dialogue := ""
		if dlgIdx >= 0 && dlgIdx < len(cells) {
			dialogue = cleanMarkdown(cells[dlgIdx])
		}
		duration := 3.0
		if durIdx >= 0 && durIdx < len(cells) {
			fmtScanFloat(cleanMarkdown(cells[durIdx]), &duration)
		}

		items = append(items, task.StoryboardItem{
			ShotNumber:  shotNum,
			Scene:       currentScene,
			Description: desc,
			Camera:      camera,
			Dialogue:    dialogue,
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
