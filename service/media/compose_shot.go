package media

import (
	"toonflow/service/storyboard"
	"toonflow/service/voice"
	"toonflow/service/internal/ffmpeg"
	"toonflow/service/internal/fsutil"
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"toonflow/adapter"
)

// ComposeShotResult describes one dialogue compose run.
type ComposeShotResult struct {
	ComposedURL string `json:"composed_url"`
	Mode        string `json:"mode"` // tts
	ShotNumber  int    `json:"shot_number"`
	Speaker     string `json:"speaker,omitempty"`
	Text        string `json:"text,omitempty"`
	VoiceID     string `json:"voice_id,omitempty"`
	Message     string `json:"message"`
}

// ComposeShotClip mixes TTS dialogue + burned subtitles into the selected video clip.
func ComposeShotClip(ctx context.Context, db *sql.DB, v adapter.Vendor, outputDir, projectID, episodeID string, shotNumber int) (*ComposeShotResult, error) {
	clip, err := SelectedClipForShot(db, projectID, episodeID, shotNumber)
	if err != nil {
		return nil, err
	}
	if clip.FileURL == "" {
		return nil, fmt.Errorf("第 %d 镜尚无视频，请先生成视频", shotNumber)
	}

	shot, err := storyboard.LoadShot(db, projectID, episodeID, shotNumber)
	if err != nil {
		return nil, err
	}
	lines := storyboard.DialogueLinesForTTS(shot.Dialogue)
	if len(lines) == 0 {
		parsed := storyboard.DialogueForTTS(shot.Dialogue)
		return nil, fmt.Errorf("%s", storyboard.ExplainComposeSkipReason(shotNumber, shot.Dialogue, parsed))
	}

	videoPath, ok := fsutil.PublicURLToLocal(outputDir, clip.FileURL)
	if !ok {
		return nil, fmt.Errorf("视频文件不存在: %s", clip.FileURL)
	}
	videoDur := clip.Duration
	if probed, err := ffmpeg.ProbeMediaDuration(videoPath); err == nil && probed > 0 {
		videoDur = probed
	} else if videoDur <= 0 {
		videoDur = shot.Duration
	}
	if videoDur <= 0 {
		videoDur = 5
	}

	composedDir := filepath.Join(outputDir, "composed", projectID, episodeID)
	if err := os.MkdirAll(composedDir, 0755); err != nil {
		return nil, err
	}
	outName := fmt.Sprintf("shot_%d_v%d_%d.mp4", shotNumber, clip.Version, time.Now().UnixNano())
	outPath := filepath.Join(composedDir, outName)

	workDir, err := os.MkdirTemp(composedDir, "compose_")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)

	audioPath, subtitlePath, voiceID, err := synthesizeDialogueTracks(ctx, db, v, workDir, projectID, lines, videoDur)
	if err != nil {
		return nil, err
	}

	if err := ffmpegComposeShot(videoPath, audioPath, subtitlePath, outPath); err != nil {
		return nil, err
	}

	publicURL := fmt.Sprintf("/output/composed/%s/%s/%s", projectID, episodeID, outName)
	_, err = db.Exec(`UPDATE o_shot_clip SET composed_file_url = ? WHERE id = ?`, publicURL, clip.ID)
	if err != nil {
		return nil, err
	}
	msg := formatComposeMessage(shotNumber, lines)
	displayText := strings.Join(func() []string {
		var t []string
		for _, ln := range lines {
			t = append(t, ln.PureText)
		}
		return t
	}(), " / ")
	return &ComposeShotResult{
		ComposedURL: publicURL,
		Mode:        "tts",
		ShotNumber:  shotNumber,
		Speaker:     lines[0].Speaker,
		Text:        displayText,
		VoiceID:     voiceID,
		Message:     msg,
	}, nil
}

// BatchComposeShots composes all shots that have dialogue and a selected video clip.
func BatchComposeShots(ctx context.Context, db *sql.DB, v adapter.Vendor, outputDir, projectID, episodeID string) (int, []string, error) {
	items, err := storyboard.LoadStoryboardItems(db, projectID, episodeID)
	if err != nil {
		return 0, nil, err
	}
	var composed int
	var urls []string
	for _, it := range items {
		if !storyboard.HasSpeakableDialogue(it.Dialogue) {
			continue
		}
		url, err := ComposeShotClip(ctx, db, v, outputDir, projectID, episodeID, it.ShotNumber)
		if err != nil {
			continue
		}
		composed++
		urls = append(urls, url.ComposedURL)
	}
	if composed == 0 {
		return 0, nil, fmt.Errorf("没有可合成的对白镜头（请确认分镜含对白且已生成视频）")
	}
	return composed, urls, nil
}

func SelectedClipForShot(db *sql.DB, projectID, episodeID string, shotNumber int) (*ShotClip, error) {
	clips, err := ListShotClips(db, projectID, episodeID)
	if err != nil {
		return nil, err
	}
	for _, c := range clips {
		if c.ShotNumber == shotNumber && c.IsSelected && c.Status == "ready" && c.FileURL != "" {
			return &c, nil
		}
	}
	for _, c := range clips {
		if c.ShotNumber == shotNumber && c.Status == "ready" && c.FileURL != "" {
			return &c, nil
		}
	}
	return nil, fmt.Errorf("第 %d 镜没有可用视频片段", shotNumber)
}

func ffmpegSupportsSubtitles() bool {
	out, err := exec.Command("ffmpeg", "-hide_banner", "-filters").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "subtitles")
}

func ffmpegComposeShot(videoPath, audioPath, subtitlePath, outPath string) error {
	videoDur, err := ffmpeg.ProbeMediaDuration(videoPath)
	if err != nil || videoDur <= 0 {
		videoDur = 0
	}
	audioDur, _ := ffmpeg.ProbeMediaDuration(audioPath)

	args := []string{"-y", "-i", videoPath, "-i", audioPath}

	var vf []string
	if subtitlePath != "" && ffmpegSupportsSubtitles() {
		escaped := strings.ReplaceAll(subtitlePath, `\`, `/`)
		escaped = strings.ReplaceAll(escaped, `:`, `\:`)
		escaped = strings.ReplaceAll(escaped, `'`, `\'`)
		forceStyle := "FontSize=20,PrimaryColour=&HFFFFFF&,OutlineColour=&H000000&,Outline=2"
		vf = append(vf, fmt.Sprintf("subtitles=filename='%s':force_style='%s'", escaped, forceStyle))
	}
	if len(vf) > 0 {
		args = append(args, "-vf", strings.Join(vf, ","))
	}

	if videoDur > 0 {
		args = append(args,
			"-filter_complex", composeAudioMatchVideoFilter(videoDur, audioDur),
			"-map", "0:v", "-map", "[aout]",
		)
	} else {
		args = append(args, "-map", "0:v", "-map", "1:a")
	}

	args = append(args,
		"-c:v", "libx264", "-preset", "medium", "-crf", "18",
		"-c:a", "aac",
	)
	if videoDur > 0 {
		args = append(args, "-t", fmt.Sprintf("%.3f", videoDur))
	}
	args = append(args, outPath)

	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg compose: %s: %w", string(out), err)
	}
	return nil
}

// composeAudioMatchVideoFilter time-stretches dialogue to video length (atempo) without truncating speech.
func composeAudioMatchVideoFilter(videoDur, audioDur float64) string {
	durStr := fmt.Sprintf("%.3f", videoDur)
	base := "[1:a]"
	if videoDur <= 0 {
		return base + "anull[aout]"
	}
	if audioDur <= 0 {
		return fmt.Sprintf("%sapad=whole_dur=%s[aout]", base, durStr)
	}

	ratio := audioDur / videoDur
	const eps = 0.02
	mid := base
	if math.Abs(ratio-1.0) >= eps {
		if chain := buildAtempoChain(ratio); chain != "" {
			mid += chain + ","
		}
	}
	return fmt.Sprintf("%sasetpts=PTS-STARTPTS,apad=whole_dur=%s[aout]", mid, durStr)
}

// buildAtempoChain returns comma-separated atempo filters whose product equals ratio.
// ratio > 1 speeds up (longer audio → shorter); ratio < 1 slows down (shorter audio → longer).
func buildAtempoChain(ratio float64) string {
	const eps = 0.01
	var parts []string
	f := ratio
	for f > 2.0+eps {
		parts = append(parts, "atempo=2.0")
		f /= 2.0
	}
	for f < 0.5-eps {
		parts = append(parts, "atempo=0.5")
		f /= 0.5
	}
	if math.Abs(f-1.0) > eps {
		parts = append(parts, fmt.Sprintf("atempo=%.6f", f))
	}
	return strings.Join(parts, ",")
}

func synthesizeDialogueTracks(ctx context.Context, db *sql.DB, v adapter.Vendor, workDir, projectID string, lines []storyboard.ParsedDialogue, videoDur float64) (audioPath, subtitlePath, primaryVoice string, err error) {
	partPaths := make([]string, 0, len(lines))
	for i, ln := range lines {
		voiceID := voice.LookupCharacterVoice(db, projectID, ln.Speaker)
		if i == 0 {
			primaryVoice = voiceID
		}
		audioBytes, synErr := adapter.SynthesizeSpeech(ctx, v, ln.PureText, voiceID)
		if synErr != nil {
			return "", "", "", fmt.Errorf("TTS 合成失败（%s）: %w", ln.Speaker, synErr)
		}
		part := filepath.Join(workDir, fmt.Sprintf("line_%d.mp3", i))
		if writeErr := os.WriteFile(part, audioBytes, 0644); writeErr != nil {
			return "", "", "", writeErr
		}
		partPaths = append(partPaths, part)
	}
	audioPath = filepath.Join(workDir, "dialogue.mp3")
	if err := concatDialogueAudio(partPaths, audioPath); err != nil {
		return "", "", "", err
	}
	subtitlePath = filepath.Join(workDir, "dialogue.srt")
	srt := buildDialogueSRT(lines, videoDur)
	if err := os.WriteFile(subtitlePath, []byte(srt), 0644); err != nil {
		return "", "", "", err
	}
	return audioPath, subtitlePath, primaryVoice, nil
}

func concatDialogueAudio(parts []string, output string) error {
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
	out, err := exec.Command("ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i", listPath, "-c:a", "libmp3lame", "-q:a", "4", output).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg concat audio: %s", string(out))
	}
	return nil
}

func buildDialogueSRT(lines []storyboard.ParsedDialogue, videoDur float64) string {
	if len(lines) == 0 {
		return ""
	}
	totalWeight := 0
	for _, ln := range lines {
		totalWeight += storyboard.DialogueLineWeight(ln.PureText)
	}
	if totalWeight < 1 {
		totalWeight = 1
	}
	start := 0.5
	usable := videoDur - 1.0
	if usable < 1 {
		usable = videoDur * 0.9
	}
	if usable < 0.5 {
		usable = 0.5
	}
	var b strings.Builder
	for i, ln := range lines {
		share := float64(storyboard.DialogueLineWeight(ln.PureText)) / float64(totalWeight)
		dur := usable * share
		if dur < 0.8 {
			dur = 0.8
		}
		end := start + dur
		if end > videoDur-0.2 {
			end = videoDur - 0.2
		}
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", i+1, formatSRTTime(start), formatSRTTime(end), ln.PureText)
		start = end + 0.15
	}
	return b.String()
}

func formatSRTTime(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	h := int(sec) / 3600
	m := (int(sec) % 3600) / 60
	s := int(sec) % 60
	ms := int((sec - float64(int(sec))) * 1000)
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}

func formatComposeMessage(shotNumber int, lines []storyboard.ParsedDialogue) string {
	if len(lines) == 1 {
		return fmt.Sprintf("第 %d 镜对白已合成（%s：%s）", shotNumber, lines[0].Speaker, lines[0].PureText)
	}
	var parts []string
	for _, ln := range lines {
		parts = append(parts, fmt.Sprintf("%s：%s", ln.Speaker, ln.PureText))
	}
	return fmt.Sprintf("第 %d 镜对白已合成（%d 句：%s）", shotNumber, len(lines), strings.Join(parts, "；"))
}

// EffectiveClipFileURL returns composed URL when available.
func EffectiveClipFileURL(c ShotClip) string {
	if u := strings.TrimSpace(c.ComposedFileURL); u != "" {
		return u
	}
	return c.FileURL
}
