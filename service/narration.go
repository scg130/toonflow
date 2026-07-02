package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"toonflow/adapter"
)

const DefaultNarrationVoice = "zh-CN-YunyangNeural"

// NarrationSegment is one timed narration line on the timeline.
type NarrationSegment struct {
	Start    float64 `json:"start"`
	End      float64 `json:"end"`
	Text     string  `json:"text"`
	ShotNum  int     `json:"shot_number,omitempty"`
	Label    string  `json:"label,omitempty"`
	AudioURL string  `json:"audio_url,omitempty"`
}

// NarrationPlan holds AI-generated narration aligned to video duration.
type NarrationPlan struct {
	ProjectID     string             `json:"project_id"`
	EpisodeID     string             `json:"episode_id"`
	TotalDuration float64            `json:"total_duration"`
	Voice         string             `json:"voice"`
	Segments      []NarrationSegment `json:"segments"`
	AudioURL      string             `json:"audio_url,omitempty"`
	Status        string             `json:"status"` // draft | synthesized
}

var reJSONArray = regexp.MustCompile(`\[[\s\S]*\]`)

// TimelineVideoDuration sums trimmed duration of all video clips on the timeline.
func TimelineVideoDuration(tl *TimelineEdit) float64 {
	if tl == nil {
		return 0
	}
	vt := findTrack(tl, "video")
	if vt == nil {
		return 0
	}
	var total float64
	for _, clip := range vt.Clips {
		total += clipPlayDuration(clip)
	}
	return total
}

func clipPlayDuration(clip TimelineClip) float64 {
	start := clip.Start
	end := clip.End
	if end <= 0 {
		end = clip.Duration
	}
	if end <= start {
		if clip.Duration > 0 {
			return clip.Duration
		}
		return 3
	}
	return end - start
}

// GenerateNarrationPlan uses AI to draft timed narration for the current timeline.
func GenerateNarrationPlan(ctx context.Context, db *sql.DB, v adapter.Vendor, projectID, episodeID string, tl *TimelineEdit) (*NarrationPlan, error) {
	if v == nil {
		return nil, fmt.Errorf("AI 供应商未配置")
	}
	if tl == nil {
		var err error
		tl, err = LoadTimeline(db, projectID, episodeID)
		if err != nil {
			return nil, err
		}
	}
	total := TimelineVideoDuration(tl)
	if total <= 0 {
		return nil, fmt.Errorf("时间线没有视频片段，请先载入分镜视频")
	}

	script := loadEpisodeScript(db, episodeID)
	clipLines := buildTimelineClipLines(tl)
	prompt := fmt.Sprintf(`你是短剧解说撰稿人。请根据下方「时间线片段」与剧本摘要，生成与成片总时长 %.1f 秒匹配的旁白方案。

要求：
- 只输出 JSON 数组，不要 markdown 或其它说明
- 每项字段：start (秒), end (秒), text (中文旁白), shot_number (可选镜号整数)
- **每个时间线视频片段必须对应一条旁白**，start/end 与该片段起止时间一致，不要跳过任何片段
- 旁白应连续覆盖 0~%.1f 秒，段与段之间不要留超过 0.3 秒的空白
- 每段 text 控制在 (end-start)×4 个汉字以内，宁可精简也不要写太长（避免念不完）
- 旁白覆盖关键剧情与情绪，不要念镜头号/分镜等技术词
- 各段按时间顺序排列

时间线片段（每条必须写旁白）：
%s

剧本摘要（前 3000 字）：
%s

示例：[{"start":0,"end":3.5,"text":"废墟之中，一切归于死寂。","shot_number":1}]`,
		total, total, clipLines, truncateRunes(script, 3000))

	resp, err := v.TextRequest(ctx, adapter.DefaultTextModel, adapter.TextParams{
		Messages: []adapter.TextMessage{
			{Role: "system", Content: "你只输出合法 JSON 数组。"},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.6,
		MaxTokens:   4000,
	})
	if err != nil {
		return nil, fmt.Errorf("生成旁白方案失败: %w", err)
	}

	segments, err := parseNarrationSegments(resp.Content)
	if err != nil {
		return nil, err
	}
	if len(segments) == 0 {
		return nil, fmt.Errorf("旁白方案为空，请重试")
	}
	NormalizeNarrationSegments(segments, total)
	segments = ensureNarrationCoverage(segments, tl, total)
	sortNarrationSegments(segments)
	NormalizeNarrationSegments(segments, total)

	voice := DefaultNarrationVoice
	if tl.Narration != nil && strings.TrimSpace(tl.Narration.Voice) != "" {
		voice = tl.Narration.Voice
	}

	plan := &NarrationPlan{
		ProjectID:     projectID,
		EpisodeID:     episodeID,
		TotalDuration: total,
		Voice:         voice,
		Segments:      segments,
		Status:        "draft",
	}
	return plan, nil
}

// SynthesizeNarrationPlan renders all segments with TTS and builds one narration audio track.
func SynthesizeNarrationPlan(ctx context.Context, v adapter.Vendor, outputDir string, plan *NarrationPlan) error {
	if plan == nil || len(plan.Segments) == 0 {
		return fmt.Errorf("旁白方案为空")
	}
	if plan.Voice == "" {
		plan.Voice = DefaultNarrationVoice
	}

	dir := filepath.Join(outputDir, "narration", plan.ProjectID, plan.EpisodeID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	workDir, err := os.MkdirTemp(dir, "work_")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	type timedPart struct {
		path  string
		start float64
	}
	var parts []timedPart
	for i, seg := range plan.Segments {
		text := strings.TrimSpace(seg.Text)
		if text == "" {
			continue
		}
		rawPath := filepath.Join(workDir, fmt.Sprintf("seg_%03d.mp3", i))
		if err := synthesizeNarrationTextToFile(ctx, v, text, plan.Voice, rawPath); err != nil {
			return fmt.Errorf("第 %d 段旁白合成失败: %w", i+1, err)
		}
		parts = append(parts, timedPart{path: rawPath, start: seg.Start})
	}
	if len(parts) == 0 {
		return fmt.Errorf("没有可合成的旁白文本")
	}

	paths := make([]string, len(parts))
	starts := make([]float64, len(parts))
	for i, p := range parts {
		paths[i] = p.path
		starts[i] = p.start
	}

	outName := fmt.Sprintf("narration_%d.mp3", time.Now().UnixNano())
	outPath := filepath.Join(dir, outName)
	if err := mixTimedNarrationAudio(paths, starts, plan.TotalDuration, outPath); err != nil {
		return err
	}

	plan.AudioURL = fmt.Sprintf("/output/narration/%s/%s/%s", plan.ProjectID, plan.EpisodeID, outName)
	plan.Status = "synthesized"
	return nil
}

// ApplyNarrationToTimeline stores the plan and adds narration audio to the audio track.
func ApplyNarrationToTimeline(tl *TimelineEdit, plan *NarrationPlan) error {
	if tl == nil || plan == nil {
		return fmt.Errorf("invalid timeline or plan")
	}
	if plan.AudioURL == "" {
		return fmt.Errorf("请先合成旁白配音")
	}
	tl.Narration = plan
	aTrack := findTrack(tl, "audio")
	if aTrack == nil {
		tl.Tracks = append(tl.Tracks, TimelineTrack{Type: "audio", Clips: []TimelineClip{}})
		aTrack = findTrack(tl, "audio")
	}
	// Replace previous narration clip, keep BGM clips.
	var kept []TimelineClip
	for _, c := range aTrack.Clips {
		if c.Label == "旁白配音" {
			continue
		}
		kept = append(kept, c)
	}
	narrationClip := TimelineClip{
		ID:       fmt.Sprintf("narr_%d", time.Now().UnixNano()),
		Label:    "旁白配音",
		FileURL:  plan.AudioURL,
		Start:    0,
		End:      plan.TotalDuration,
		Duration: plan.TotalDuration,
		Offset:   0,
	}
	aTrack.Clips = append([]TimelineClip{narrationClip}, kept...)
	return nil
}

func parseNarrationSegments(content string) ([]NarrationSegment, error) {
	raw := strings.TrimSpace(content)
	if raw == "" {
		return nil, fmt.Errorf("AI 未返回旁白内容")
	}
	if m := reJSONArray.FindString(raw); m != "" {
		raw = m
	}
	var segments []NarrationSegment
	if err := json.Unmarshal([]byte(raw), &segments); err != nil {
		return nil, fmt.Errorf("解析旁白 JSON 失败: %w", err)
	}
	return segments, nil
}

// NormalizeNarrationSegments clamps segment times to the video duration.
func NormalizeNarrationSegments(segments []NarrationSegment, total float64) {
	for i := range segments {
		segments[i].Text = strings.TrimSpace(segments[i].Text)
		if segments[i].End <= segments[i].Start {
			segments[i].End = segments[i].Start + 3
		}
		if segments[i].End > total {
			segments[i].End = total
		}
		if segments[i].Start < 0 {
			segments[i].Start = 0
		}
		if segments[i].End <= segments[i].Start {
			segments[i].End = segments[i].Start + 1
		}
	}
}

type clipWindow struct {
	Start      float64
	End        float64
	ShotNumber int
	Label      string
}

func timelineClipWindows(tl *TimelineEdit) []clipWindow {
	vt := findTrack(tl, "video")
	if vt == nil {
		return nil
	}
	var windows []clipWindow
	offset := 0.0
	for _, clip := range vt.Clips {
		dur := clipPlayDuration(clip)
		windows = append(windows, clipWindow{
			Start:      offset,
			End:        offset + dur,
			ShotNumber: clip.ShotNumber,
			Label:      clip.Label,
		})
		offset += dur
	}
	return windows
}

// ensureNarrationCoverage adds segments for timeline clips that have no narration.
func ensureNarrationCoverage(segments []NarrationSegment, tl *TimelineEdit, total float64) []NarrationSegment {
	windows := timelineClipWindows(tl)
	if len(windows) == 0 {
		return segments
	}
	for _, w := range windows {
		if segmentCoversWindow(segments, w) {
			continue
		}
		text := fmt.Sprintf("第 %d 镜。", w.ShotNumber)
		if w.ShotNumber <= 0 {
			text = "镜头继续。"
		}
		segments = append(segments, NarrationSegment{
			Start:   w.Start,
			End:     w.End,
			Text:    text,
			ShotNum: w.ShotNumber,
		})
	}
	return segments
}

func sortNarrationSegments(segments []NarrationSegment) {
	sort.Slice(segments, func(i, j int) bool {
		if segments[i].Start == segments[j].Start {
			return segments[i].End < segments[j].End
		}
		return segments[i].Start < segments[j].Start
	})
}

func segmentCoversWindow(segments []NarrationSegment, w clipWindow) bool {
	winDur := w.End - w.Start
	if winDur <= 0 {
		return true
	}
	for _, seg := range segments {
		if strings.TrimSpace(seg.Text) == "" {
			continue
		}
		overlap := min(seg.End, w.End) - max(seg.Start, w.Start)
		if overlap >= winDur*0.5 {
			return true
		}
	}
	return false
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func buildTimelineClipLines(tl *TimelineEdit) string {
	vt := findTrack(tl, "video")
	if vt == nil || len(vt.Clips) == 0 {
		return "（无片段）"
	}
	var b strings.Builder
	offset := 0.0
	for _, clip := range vt.Clips {
		dur := clipPlayDuration(clip)
		label := clip.Label
		if label == "" {
			label = fmt.Sprintf("片段")
		}
		fmt.Fprintf(&b, "- %.1f~%.1fs: %s\n", offset, offset+dur, label)
		offset += dur
	}
	return b.String()
}

func loadEpisodeScript(db *sql.DB, episodeID string) string {
	if db == nil || episodeID == "" {
		return ""
	}
	var script string
	_ = db.QueryRow(`SELECT script_content FROM o_episode WHERE id = ?`, episodeID).Scan(&script)
	if strings.TrimSpace(script) == "" {
		_ = db.QueryRow(`
			SELECT content FROM o_agent_work
			WHERE episode_id = ? AND work_type = 'script' ORDER BY updated_at DESC LIMIT 1`, episodeID).Scan(&script)
	}
	return script
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func synthesizeNarrationTextToFile(ctx context.Context, v adapter.Vendor, text, voice, dest string) error {
	chunks := splitNarrationText(text)
	if len(chunks) == 1 {
		return synthesizeTTSToFile(ctx, v, chunks[0], voice, dest)
	}
	dir := filepath.Dir(dest)
	var partFiles []string
	for i, chunk := range chunks {
		part := filepath.Join(dir, fmt.Sprintf("%s.part%d.mp3", filepath.Base(dest), i))
		if err := synthesizeTTSToFile(ctx, v, chunk, voice, part); err != nil {
			return err
		}
		partFiles = append(partFiles, part)
	}
	return concatAudioFiles(partFiles, dest)
}

// splitNarrationText breaks long copy into TTS-friendly chunks without losing content.
func splitNarrationText(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	const maxRunes = 180
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return []string{text}
	}
	var chunks []string
	start := 0
	for start < len(runes) {
		end := start + maxRunes
		if end >= len(runes) {
			chunks = append(chunks, strings.TrimSpace(string(runes[start:])))
			break
		}
		splitAt := end
		for j := end - 1; j > start+maxRunes/2; j-- {
			switch runes[j] {
			case '。', '！', '？', '；', '，', '\n':
				splitAt = j + 1
				goto foundSplit
			}
		}
	foundSplit:
		chunks = append(chunks, strings.TrimSpace(string(runes[start:splitAt])))
		start = splitAt
	}
	if len(chunks) == 0 {
		return []string{text}
	}
	return chunks
}

func synthesizeTTSToFile(ctx context.Context, v adapter.Vendor, text, voice, dest string) error {
	data, err := adapter.SynthesizeSpeech(ctx, v, text, voice)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0644)
}

func mixTimedNarrationAudio(parts []string, starts []float64, totalSec float64, output string) error {
	if len(parts) == 0 {
		return fmt.Errorf("no audio parts")
	}
	if totalSec <= 0 {
		totalSec = 30
	}
	if len(starts) != len(parts) {
		return fmt.Errorf("segment timing mismatch")
	}

	args := []string{"-y"}
	for _, p := range parts {
		args = append(args, "-i", p)
	}

	var filters []string
	var mixInputs strings.Builder
	for i := range parts {
		delayMs := int(starts[i] * 1000)
		if delayMs < 0 {
			delayMs = 0
		}
		label := fmt.Sprintf("n%d", i)
		filters = append(filters, fmt.Sprintf("[%d:a]adelay=%d|%d,volume=1.0[%s]", i, delayMs, delayMs, label))
		mixInputs.WriteString(fmt.Sprintf("[%s]", label))
	}
	filter := strings.Join(filters, ";") + ";" + mixInputs.String() +
		fmt.Sprintf("amix=inputs=%d:duration=longest:dropout_transition=2:normalize=0[aout]", len(parts))

	args = append(args, "-filter_complex", filter, "-map", "[aout]",
		"-c:a", "libmp3lame", "-q:a", "4", output)
	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg mix narration: %s", string(out))
	}
	return nil
}

func concatAudioFiles(parts []string, output string) error {
	if len(parts) == 1 {
		in, err := os.ReadFile(parts[0])
		if err != nil {
			return err
		}
		return os.WriteFile(output, in, 0644)
	}
	listPath := output + ".txt"
	f, err := os.Create(listPath)
	if err != nil {
		return err
	}
	for _, p := range parts {
		abs, _ := filepath.Abs(p)
		fmt.Fprintf(f, "file '%s'\n", strings.ReplaceAll(abs, "'", `'\''`))
	}
	f.Close()
	defer os.Remove(listPath)

	args := []string{"-y", "-f", "concat", "-safe", "0", "-i", listPath, "-c:a", "libmp3lame", "-q:a", "4", output}
	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg concat audio: %s", string(out))
	}
	return nil
}
