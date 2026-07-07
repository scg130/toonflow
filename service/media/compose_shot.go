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
	"regexp"
	"strings"
	"time"

	"toonflow/adapter"
)

var (
	reDialogueSpeaker   = regexp.MustCompile(`^(.+?)[:：]`)
	ignoreTTSSpeakers = regexp.MustCompile(`^(环境音|环境声|音效|效果音|sfx|sound ?effect|bgm|背景音|背景音乐|ambient)$`)
	ignoreTTSText     = regexp.MustCompile(`^(无|无对白|无台词|无旁白|无需配音|none|null|n/a|na|环境音|音效|bgm|sfx|ambient)$`)
)

// ParsedDialogue holds speaker and speakable text from a storyboard line.
type ParsedDialogue struct {
	Speaker   string
	PureText  string
	Ignorable bool
	Raw       string
}

// ParseDialogueForTTS extracts speaker and text from dialogue field or description.
func ParseDialogueForTTS(dialogue string) ParsedDialogue {
	raw := strings.TrimSpace(dialogue)
	if raw == "" {
		return ParsedDialogue{Ignorable: true}
	}
	speaker := ""
	if m := reDialogueSpeaker.FindStringSubmatch(raw); len(m) >= 2 {
		speaker = voice.NormalizeSpeakerName(m[1])
	}
	pure := raw
	if parts := strings.SplitN(raw, "：", 2); len(parts) == 2 {
		pure = strings.TrimSpace(parts[1])
	} else if parts := strings.SplitN(raw, ":", 2); len(parts) == 2 {
		pure = strings.TrimSpace(parts[1])
	}
	pure = strings.TrimSpace(strings.NewReplacer("（", "", "）", "", "(", "", ")", "").Replace(pure))
	ignorable := (speaker != "" && ignoreTTSSpeakers.MatchString(speaker)) ||
		pure == "" || ignoreTTSText.MatchString(pure)
	return ParsedDialogue{Speaker: speaker, PureText: pure, Ignorable: ignorable, Raw: raw}
}

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
	dialogue := strings.TrimSpace(shot.Dialogue)
	parsed := ParseDialogueForTTS(dialogue)
	if parsed.Ignorable {
		return nil, fmt.Errorf("%s", ExplainComposeSkipReason(shotNumber, dialogue, parsed))
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

	voiceID := voice.LookupCharacterVoice(db, projectID, parsed.Speaker)
	audioBytes, err := adapter.SynthesizeSpeech(ctx, v, parsed.PureText, voiceID)
	if err != nil {
		return nil, fmt.Errorf("TTS 合成失败: %w", err)
	}
	workDir, err := os.MkdirTemp(composedDir, "compose_")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)

	audioPath := filepath.Join(workDir, "dialogue.mp3")
	if err := os.WriteFile(audioPath, audioBytes, 0644); err != nil {
		return nil, err
	}

	var subtitlePath string
	if parsed.PureText != "" {
		subtitlePath = filepath.Join(workDir, "dialogue.srt")
		endSec := int(videoDur - 0.5)
		if endSec < 1 {
			endSec = 1
		}
		srt := fmt.Sprintf("1\n00:00:00,500 --> 00:00:%02d,000\n%s\n",
			endSec, parsed.PureText)
		if err := os.WriteFile(subtitlePath, []byte(srt), 0644); err != nil {
			return nil, err
		}
	}

	if err := ffmpegComposeShot(videoPath, audioPath, subtitlePath, outPath); err != nil {
		return nil, err
	}

	publicURL := fmt.Sprintf("/output/composed/%s/%s/%s", projectID, episodeID, outName)
	_, err = db.Exec(`UPDATE o_shot_clip SET composed_file_url = ? WHERE id = ?`, publicURL, clip.ID)
	if err != nil {
		return nil, err
	}
	speaker := parsed.Speaker
	if speaker == "" {
		speaker = "旁白"
	}
	msg := fmt.Sprintf("第 %d 镜对白已合成（%s：%s）", shotNumber, speaker, parsed.PureText)
	return &ComposeShotResult{
		ComposedURL: publicURL,
		Mode:        "tts",
		ShotNumber:  shotNumber,
		Speaker:     speaker,
		Text:        parsed.PureText,
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
		parsed := ParseDialogueForTTS(strings.TrimSpace(it.Dialogue))
		if parsed.Ignorable {
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

// ExplainComposeSkipReason returns a user-facing hint when dialogue cannot be composed.
func ExplainComposeSkipReason(shotNumber int, raw string, parsed ParsedDialogue) string {
	if strings.TrimSpace(raw) == "" {
		return fmt.Sprintf("第 %d 镜未填写对白。请在分镜「对白」列（dialogue 字段）填写「角色名：台词」，例如「石昊：这一战，我不会退。」", shotNumber)
	}
	if parsed.Speaker != "" && ignoreTTSSpeakers.MatchString(parsed.Speaker) {
		return fmt.Sprintf("第 %d 镜对白为「%s」，属于音效/环境音，无需 TTS 配音", shotNumber, parsed.Raw)
	}
	if ignoreTTSText.MatchString(parsed.PureText) {
		return fmt.Sprintf("第 %d 镜对白「%s」无法配音，请填写具体台词", shotNumber, parsed.Raw)
	}
	if parsed.PureText == "" {
		return fmt.Sprintf("第 %d 镜对白格式有误，请使用「角色名：台词」，当前：%s", shotNumber, parsed.Raw)
	}
	return fmt.Sprintf("第 %d 镜对白无法合成：%s", shotNumber, parsed.Raw)
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
		"-c:v", "libx264", "-preset", "fast", "-crf", "23",
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

// EffectiveClipFileURL returns composed URL when available.
func EffectiveClipFileURL(c ShotClip) string {
	if u := strings.TrimSpace(c.ComposedFileURL); u != "" {
		return u
	}
	return c.FileURL
}
