package timeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"toonflow/adapter"
	"toonflow/service/internal/ffmpeg"
	"toonflow/service/internal/fsutil"
	"toonflow/service/internal/jsonutil"
	"toonflow/service/storyboard"
	"toonflow/service/voice"
)


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

// TimelineVideoDuration returns estimated export duration (trims, speed, transitions).
func TimelineVideoDuration(tl *TimelineEdit) float64 {
	return TimelineExportDuration(tl)
}

// ResolveNarrationTargetDuration prefers ffprobe on exported video, then stored duration, then estimate.
func ResolveNarrationTargetDuration(outputDir string, tl *TimelineEdit) float64 {
	if tl == nil {
		return 0
	}
	if tl.ExportedVideoURL != "" && outputDir != "" {
		if local, ok := fsutil.PublicURLToLocal(outputDir, tl.ExportedVideoURL); ok {
			if d, err := ffmpeg.ProbeMediaDuration(local); err == nil && d > 0 {
				return d
			}
		}
	}
	if tl.ExportedDuration > 0 {
		return tl.ExportedDuration
	}
	return TimelineExportDuration(tl)
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

// GenerateNarrationPlan uses AI to draft timed story commentary aligned to exported video duration.
func GenerateNarrationPlan(ctx context.Context, db *sql.DB, v adapter.Vendor, outputDir, projectID, episodeID string, tl *TimelineEdit) (*NarrationPlan, error) {
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
	total := ResolveNarrationTargetDuration(outputDir, tl)
	if total <= 0 {
		return nil, fmt.Errorf("时间线没有视频片段，请先载入分镜视频并导出成片")
	}
	if tl.ExportedVideoURL == "" {
		return nil, fmt.Errorf("请先导出无旁白成片（对白已合成），再根据实际时长生成旁白解说")
	}

	script := loadEpisodeScript(db, episodeID)
	skeleton := loadAgentWork(db, episodeID, "skeleton")
	strategy := loadAgentWork(db, episodeID, "strategy")
	dialogueLines := buildShotDialogueReference(db, projectID, episodeID)
	clipLines := buildTimelineClipLines(tl)
	wordsBudget := int(total * 3.8)
	if wordsBudget < 20 {
		wordsBudget = 20
	}

	prompt := fmt.Sprintf(`你是抖音短剧「故事解说」撰稿人。请为下方已导出的成片（实测总时长 %.1f 秒）撰写**第三人称旁白解说**，铺满 0~%.1f 秒。

## 旁白 vs 角色对白（必须遵守）
- **旁白** = 故事解说/剧情概括，第三人称或 omniscient 叙述，帮观众理解剧情走向；
- **角色对白** = 已烧录在成片各镜中，由 TTS 配音，**旁白不得复述、引用或改写**下方列出的任何角色台词；
- 旁白与对白是两条音轨：对白在画面里，旁白是上层解说，内容必须互补而非重复。

## 输出与时长（重要）
- 只输出一个 JSON 对象 {"segments":[...]}，不要 markdown 或其它说明；
- segments 每项字段：text (中文旁白句)，shot_number 可选；**不需要你写 start/end 时间**，时间轴由系统按句子长度自动铺满 0~%.1f 秒；
- 把整集写成**一整条连续的长旁白**，按语义/句子切成有序的多段（segments），按剧情先后排列即可；
- 全片旁白总字数约 %d 字（按 %.1f 秒口播估算），必须写满全片、念得完，宁可精简也不要留半段空白；
- **必须概括本集完整故事线**（开场→发展→转折→高潮→收束），从头讲到尾，不要只讲前半段；
- 旁白风格：短剧解说、信息密度高、有悬念感，不要念镜头号/分镜/技术词。

## 时间线镜头（仅供了解剧情节奏，不必逐镜对应）
%s

## 各镜角色对白（勿复述，仅供了解剧情）
%s

## 本集剧本（完整参考）
%s

## 故事骨架
%s

## 改编策略
%s

示例：{"segments":[{"start":0,"end":4.0,"text":"界海边缘，石昊独自站在废墟之上，这一战，他已无退路。","shot_number":1}]}`,
		total, total, total, wordsBudget, total, clipLines, dialogueLines,
		truncateRunes(script, 8000), truncateRunes(skeleton, 2000), truncateRunes(strategy, 2000))

		resp, err := v.TextRequest(ctx, adapter.DefaultTextModel, adapter.TextParams{
			Messages: []adapter.TextMessage{
				{Role: "system", Content: "你只输出合法 JSON 对象 {\"segments\":[...]}。旁白是第三人称故事解说，禁止复述角色对白原文。"},
				{Role: "user", Content: prompt},
			},
		Temperature: 0.6,
		MaxTokens:   4000,
		JSONMode:    true,
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
	// One continuous narration: lay the model's story commentary back-to-back to
	// fill the whole video by sentence length (model no longer supplies timing).
	segments = redistributeNarrationTiming(segments, total)
	if len(segments) == 0 {
		return nil, fmt.Errorf("旁白方案为空，请重试")
	}

	voice := voice.DefaultNarrationVoice
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
		plan.Voice = voice.DefaultNarrationVoice
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
			rawPath := filepath.Join(workDir, fmt.Sprintf("seg_%03d_raw.mp3", i))
			if err := synthesizeNarrationTextToFile(ctx, v, text, plan.Voice, rawPath); err != nil {
				return fmt.Errorf("第 %d 段旁白合成失败: %w", i+1, err)
			}
			slot := seg.End - seg.Start
			if slot <= 0 {
				slot = 3
			}
			fittedPath := filepath.Join(workDir, fmt.Sprintf("seg_%03d.mp3", i))
			if err := ffmpeg.StretchAudioToDuration(rawPath, fittedPath, slot); err != nil {
				return fmt.Errorf("第 %d 段旁白时长对齐失败: %w", i+1, err)
			}
			parts = append(parts, timedPart{path: fittedPath, start: seg.Start})
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
			Volume:   0.9,
		}
	aTrack.Clips = append([]TimelineClip{narrationClip}, kept...)
	return nil
}

func parseNarrationSegments(content string) ([]NarrationSegment, error) {
	raw := strings.TrimSpace(content)
	if raw == "" {
		return nil, fmt.Errorf("AI 未返回旁白内容")
	}
	// Preferred: JSON-mode object wrapper {"segments":[...]}.
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end > start {
			var wrapper struct {
				Segments []NarrationSegment `json:"segments"`
			}
			if err := json.Unmarshal([]byte(raw[start:end+1]), &wrapper); err == nil && len(wrapper.Segments) > 0 {
				return wrapper.Segments, nil
			}
		}
	}
	// Fallback: bare JSON array.
	var segments []NarrationSegment
	if err := json.Unmarshal([]byte(jsonutil.ExtractJSONArray(raw)), &segments); err != nil {
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
	settings := tl.ExportSettings
	if settings == nil {
		settings = DefaultExportSettings()
	}
	var windows []clipWindow
	offset := 0.0
	transDur := 0.0
	if settings != nil && settings.DefaultTransition != "none" {
		transDur = settings.TransitionDuration
	}
	for i, clip := range vt.Clips {
		dur := clipPlaySeconds(clip, settings)
		windows = append(windows, clipWindow{
			Start:      offset,
			End:        offset + dur,
			ShotNumber: clip.ShotNumber,
			Label:      clip.Label,
		})
		offset += dur
		if i < len(vt.Clips)-1 {
			offset -= transDur
		}
	}
	return windows
}

// redistributeNarrationTiming lays the model's narration out as ONE continuous
// track filling 0~total: segments are placed back-to-back, each given a share of
// the total duration proportional to its text length. This turns the AI story
// commentary into a single long narration covering the whole video — no gaps and
// no generic filler. The model's own start/end are ignored on purpose.
func redistributeNarrationTiming(segments []NarrationSegment, total float64) []NarrationSegment {
	kept := make([]NarrationSegment, 0, len(segments))
	var totalWeight float64
	for _, s := range segments {
		txt := strings.TrimSpace(s.Text)
		if txt == "" {
			continue
		}
		s.Text = txt
		kept = append(kept, s)
		totalWeight += float64(len([]rune(txt)))
	}
	if len(kept) == 0 || total <= 0 {
		return kept
	}
	if totalWeight <= 0 {
		totalWeight = float64(len(kept))
	}
	offset := 0.0
	for i := range kept {
		w := float64(len([]rune(kept[i].Text)))
		if w <= 0 {
			w = 1
		}
		kept[i].Start = offset
		if i == len(kept)-1 {
			kept[i].End = total
		} else {
			kept[i].End = offset + total*w/totalWeight
		}
		offset = kept[i].End
	}
	return kept
}

func buildTimelineClipLines(tl *TimelineEdit) string {
	windows := timelineClipWindows(tl)
	if len(windows) == 0 {
		return "（无片段）"
	}
	var b strings.Builder
	for _, w := range windows {
		label := w.Label
		if label == "" {
			label = "片段"
		}
		fmt.Fprintf(&b, "- %.1f~%.1fs: %s\n", w.Start, w.End, label)
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

func loadAgentWork(db *sql.DB, episodeID, workType string) string {
	if db == nil || episodeID == "" {
		return ""
	}
	var content string
	_ = db.QueryRow(`
		SELECT content FROM o_agent_work
		WHERE episode_id = ? AND work_type = ? ORDER BY updated_at DESC LIMIT 1`,
		episodeID, workType).Scan(&content)
	return content
}

func buildShotDialogueReference(db *sql.DB, projectID, episodeID string) string {
	items, err := storyboard.LoadStoryboardItems(db, projectID, episodeID)
	if err != nil || len(items) == 0 {
		return "（无分镜对白）"
	}
	var b strings.Builder
	for _, it := range items {
		if it.Dialogue == nil || it.Dialogue.IsEmpty() {
			continue
		}
		fmt.Fprintf(&b, "- 第 %d 镜：%s\n", it.ShotNumber, storyboard.FormatDialogueDisplay(it.Dialogue))
	}
	if b.Len() == 0 {
		return "（本集无角色对白，旁白需完整讲述故事）"
	}
	return b.String()
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
