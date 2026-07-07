package timeline

import (
	"toonflow/service/internal/ffmpeg"
	"toonflow/service/internal/fsutil"
	"toonflow/service/media"
	"toonflow/service/storyboard"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// TimelineClip is one segment on the video or audio track.
type TimelineClip struct {
	ID         string  `json:"id"`
	ShotClipID string  `json:"shot_clip_id,omitempty"`
	ShotNumber int     `json:"shot_number,omitempty"`
	Label      string  `json:"label,omitempty"`
	FileURL    string  `json:"file_url"`
	Start      float64 `json:"start"`  // trim in (seconds)
	End        float64 `json:"end"`    // trim out (seconds); 0 = full length
	Duration   float64 `json:"duration"`
	Offset     float64 `json:"offset,omitempty"` // audio offset on timeline
	// Per-clip edit (剪映基础)
	Transition   string  `json:"transition,omitempty"`    // none | fade | dip | wipe — after this clip
	TransitionDur float64 `json:"transition_dur,omitempty"` // override global transition seconds
	Speed        float64 `json:"speed,omitempty"`         // 0.5–2.0
	Volume       float64 `json:"volume,omitempty"`        // audio volume multiplier
	Brightness   float64 `json:"brightness,omitempty"`    // ffmpeg eq -1..1
	Contrast     float64 `json:"contrast,omitempty"`
	Saturation   float64 `json:"saturation,omitempty"`
	FadeIn       float64 `json:"fade_in,omitempty"`  // seconds
	FadeOut      float64 `json:"fade_out,omitempty"` // seconds
}

// TimelineExportSettings is persisted on TimelineEdit.export_settings.
type TimelineExportSettings struct {
	DefaultTransition  string  `json:"default_transition,omitempty"`
	TransitionDuration float64 `json:"transition_duration,omitempty"`
	TrimHeadFrames     int     `json:"trim_head_frames,omitempty"`
	TrimTailFrames     int     `json:"trim_tail_frames,omitempty"`
	GlobalBrightness   float64 `json:"global_brightness,omitempty"`
	GlobalContrast     float64 `json:"global_contrast,omitempty"`
	GlobalSaturation   float64 `json:"global_saturation,omitempty"`
}

// TimelineEdit is the saved editor state per episode.
type TimelineEdit struct {
	ProjectID         string                  `json:"project_id"`
	EpisodeID         string                  `json:"episode_id"`
	Tracks            []TimelineTrack         `json:"tracks"`
	Narration         *NarrationPlan          `json:"narration,omitempty"`
	ExportSettings    *TimelineExportSettings `json:"export_settings,omitempty"`
	ExportedVideoURL  string                  `json:"exported_video_url,omitempty"`  // 无旁白成片（对白已烧录）
	ExportedDuration  float64                 `json:"exported_duration,omitempty"`   // ffprobe 实测时长
	UpdatedAt         string                  `json:"updated_at,omitempty"`
}

// TimelineTrack holds clips for one track.
type TimelineTrack struct {
	Type  string         `json:"type"` // video | audio
	Clips []TimelineClip `json:"clips"`
}

// LoadTimeline loads timeline editor state.
func LoadTimeline(db *sql.DB, projectID, episodeID string) (*TimelineEdit, error) {
	var dataJSON string
	var updatedAt time.Time
	err := db.QueryRow(`
		SELECT data_json, updated_at FROM o_timeline WHERE project_id = ? AND episode_id = ?`,
		projectID, episodeID).Scan(&dataJSON, &updatedAt)
	if err == sql.ErrNoRows {
		return buildDefaultTimeline(db, projectID, episodeID)
	}
	if err != nil {
		return nil, err
	}
	var tl TimelineEdit
	if err := json.Unmarshal([]byte(dataJSON), &tl); err != nil {
		return buildDefaultTimeline(db, projectID, episodeID)
	}
	tl.ProjectID = projectID
	tl.EpisodeID = episodeID
	tl.UpdatedAt = updatedAt.Format(time.RFC3339)
	NormalizeTimelineEdit(&tl)
	return &tl, nil
}

// SaveTimeline persists timeline editor state.
func SaveTimeline(db *sql.DB, tl *TimelineEdit) error {
	data, err := json.Marshal(tl)
	if err != nil {
		return err
	}
	id := fmt.Sprintf("tl_%s_%s", tl.ProjectID, tl.EpisodeID)
	_, err = db.Exec(`
		INSERT INTO o_timeline (id, project_id, episode_id, data_json, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(project_id, episode_id) DO UPDATE SET data_json = excluded.data_json, updated_at = CURRENT_TIMESTAMP`,
		id, tl.ProjectID, tl.EpisodeID, string(data))
	return err
}

// ExportTimeline renders the timeline to a final mp4 using ffmpeg.
func ExportTimeline(outputDir string, tl *TimelineEdit) (string, error) {
	NormalizeTimelineEdit(tl)
	videoTrack := findTrack(tl, "video")
	if videoTrack == nil || len(videoTrack.Clips) == 0 {
		return "", fmt.Errorf("时间线没有视频片段，请先添加分镜视频")
	}
	settings := tl.ExportSettings
	if settings == nil {
		settings = DefaultExportSettings()
	}

	exportDir := filepath.Join(outputDir, "exports", tl.ProjectID, tl.EpisodeID)
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		return "", err
	}
	workDir, err := os.MkdirTemp(exportDir, "work_")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(workDir)

	var partFiles []string
	var partDurations []float64
	var transitions []string
	for i, clip := range videoTrack.Clips {
		local, ok := fsutil.PublicURLToLocal(outputDir, clip.FileURL)
		if !ok {
			return "", fmt.Errorf("无效片段路径: %s", clip.FileURL)
		}
		part := filepath.Join(workDir, fmt.Sprintf("part_%03d.mp4", i))
		dur, err := renderTimelinePart(local, part, clip, settings)
		if err != nil {
			return "", fmt.Errorf("clip %d: %w", i+1, err)
		}
		partFiles = append(partFiles, part)
		partDurations = append(partDurations, dur)
		if i < len(videoTrack.Clips)-1 {
			transitions = append(transitions, effectiveTransitionAfter(clip, settings))
		}
	}

	transDur := settings.TransitionDuration
	videoOnly := filepath.Join(workDir, "video_only.mp4")
	if err := concatTimelineParts(partFiles, partDurations, transitions, transDur, videoOnly); err != nil {
		return "", err
	}

	graded := filepath.Join(workDir, "graded.mp4")
	if err := applyGlobalGrade(videoOnly, graded, settings); err != nil {
		return "", err
	}
	finalVideo := graded

	outName := fmt.Sprintf("final_%d.mp4", time.Now().UnixNano())
	outPath := filepath.Join(exportDir, outName)
	finalPath := finalVideo

	audioTrack := findTrack(tl, "audio")
	if audioTrack != nil && len(audioTrack.Clips) > 0 {
		mixed := filepath.Join(workDir, "mixed.mp4")
		if err := mixTimelineAudio(finalVideo, audioTrack, mixed, outputDir); err != nil {
			return "", err
		}
		finalPath = mixed
	}

	if err := fsutil.CopyFile(finalPath, outPath); err != nil {
		return "", err
	}

	publicURL := fmt.Sprintf("/output/exports/%s/%s/%s", tl.ProjectID, tl.EpisodeID, outName)
	// Always refresh the exported reference: the video length is identical whether or
	// not narration audio is mixed in, so a stale narration audio track must not freeze
	// the measured duration used for narration planning.
	tl.ExportedVideoURL = publicURL
	if probed, err := ffmpeg.ProbeMediaDuration(outPath); err == nil && probed > 0 {
		tl.ExportedDuration = probed
	} else {
		tl.ExportedDuration = TimelineExportDuration(tl)
	}
	return publicURL, nil
}

// ReloadTimelineFromClips rebuilds the timeline from currently selected shot clips and saves it.
func ReloadTimelineFromClips(db *sql.DB, projectID, episodeID string) (*TimelineEdit, error) {
	var prevSettings *TimelineExportSettings
	if old, err := LoadTimeline(db, projectID, episodeID); err == nil && old != nil && old.ExportSettings != nil {
		prevSettings = old.ExportSettings
	}

	tl, err := buildDefaultTimeline(db, projectID, episodeID)
	if err != nil {
		return nil, err
	}
	if prevSettings != nil {
		tl.ExportSettings = prevSettings
		mergeExportSettingsDefaults(tl.ExportSettings)
	}
	if err := SaveTimeline(db, tl); err != nil {
		return nil, err
	}
	NormalizeTimelineEdit(tl)
	return tl, nil
}

// ClearTimeline removes all clips and narration, keeping default export settings.
func ClearTimeline(db *sql.DB, projectID, episodeID string) (*TimelineEdit, error) {
	tl := &TimelineEdit{
		ProjectID:      projectID,
		EpisodeID:      episodeID,
		ExportSettings: DefaultExportSettings(),
		Tracks: []TimelineTrack{
			{Type: "video", Clips: []TimelineClip{}},
			{Type: "audio", Clips: []TimelineClip{}},
		},
	}
	if err := SaveTimeline(db, tl); err != nil {
		return nil, err
	}
	return tl, nil
}

func buildDefaultTimeline(db *sql.DB, projectID, episodeID string) (*TimelineEdit, error) {
	clips, err := media.ListShotClips(db, projectID, episodeID)
	if err != nil {
		return &TimelineEdit{ProjectID: projectID, EpisodeID: episodeID, ExportSettings: DefaultExportSettings(), Tracks: []TimelineTrack{
			{Type: "video", Clips: []TimelineClip{}},
			{Type: "audio", Clips: []TimelineClip{}},
		}}, nil
	}
	selected := map[int]media.ShotClip{}
	for _, c := range clips {
		if c.IsSelected && c.Status == "ready" && c.FileURL != "" {
			selected[c.ShotNumber] = c
		}
	}
	// scene_link per shot drives boundary transitions: a "continuous" incoming
	// shot butt-joins its predecessor (no transition); a "transition" incoming
	// shot gets a visible transition mapped from its storyboard transition style.
	linkByShot := map[int]string{}
	transStyleByShot := map[int]string{}
	if items, err := storyboard.LoadStoryboardItems(db, projectID, episodeID); err == nil {
		for _, it := range items {
			linkByShot[it.ShotNumber] = it.SceneLink
			transStyleByShot[it.ShotNumber] = it.Transition
		}
	}

	var videoClips []TimelineClip
	for shotNum := 1; shotNum <= 999; shotNum++ {
		c, ok := selected[shotNum]
		if !ok {
			continue
		}
		videoClips = append(videoClips, TimelineClip{
			ID:         fmt.Sprintf("tlc_%d_%d", shotNum, c.Version),
			ShotClipID: c.ID,
			ShotNumber: c.ShotNumber,
			Label:      fmt.Sprintf("第 %d 镜 v%d", c.ShotNumber, c.Version),
			FileURL:    media.EffectiveClipFileURL(c),
			Start:      0,
			End:        c.Duration,
			Duration:   c.Duration,
		})
	}
	if len(videoClips) == 0 {
		for _, c := range clips {
			if c.Status != "ready" || c.FileURL == "" {
				continue
			}
			if _, exists := selected[c.ShotNumber]; exists {
				continue
			}
			selected[c.ShotNumber] = c
			videoClips = append(videoClips, TimelineClip{
				ID: fmt.Sprintf("tlc_%d_%d", c.ShotNumber, c.Version), ShotClipID: c.ID,
				ShotNumber: c.ShotNumber, Label: fmt.Sprintf("第 %d 镜 v%d", c.ShotNumber, c.Version),
				FileURL: media.EffectiveClipFileURL(c), Start: 0, End: c.Duration, Duration: c.Duration,
			})
		}
	}
	// Transition after clip j is decided by the NEXT clip's scene_link: continuous
	// → seamless butt-join ("none"); transition → visible transition effect.
	for j := 0; j < len(videoClips)-1; j++ {
		next := videoClips[j+1].ShotNumber
		videoClips[j].Transition = timelineTransitionForShot(linkByShot[next], transStyleByShot[next])
	}
	return &TimelineEdit{
		ProjectID: projectID, EpisodeID: episodeID,
		ExportSettings: DefaultExportSettings(),
		Tracks: []TimelineTrack{
			{Type: "video", Clips: videoClips},
			{Type: "audio", Clips: []TimelineClip{}},
		},
	}, nil
}

func findTrack(tl *TimelineEdit, typ string) *TimelineTrack {
	for i := range tl.Tracks {
		if tl.Tracks[i].Type == typ {
			return &tl.Tracks[i]
		}
	}
	return nil
}

// mixTimelineAudio mixes all audio clips (narration + BGM) onto the video.
func mixTimelineAudio(videoOnly string, audioTrack *TimelineTrack, dest, outputDir string) error {
	if audioTrack == nil || len(audioTrack.Clips) == 0 {
		return fsutil.CopyFile(videoOnly, dest)
	}
	var inputs []string
	var filters []string
	inputs = append(inputs, "-i", videoOnly)
	valid := 0
	for _, clip := range audioTrack.Clips {
		local, ok := fsutil.PublicURLToLocal(outputDir, clip.FileURL)
		if !ok {
			continue
		}
		inputs = append(inputs, "-i", local)
		delayMs := int((clip.Offset + clip.Start) * 1000)
		if delayMs < 0 {
			delayMs = 0
		}
		vol := clip.Volume
		if vol <= 0 {
			vol = 1
		}
		filters = append(filters, fmt.Sprintf("[%d:a]adelay=%d|%d,volume=%.2f[a%d]", valid+1, delayMs, delayMs, vol, valid))
		valid++
	}
	if valid == 0 {
		return fsutil.CopyFile(videoOnly, dest)
	}
	var mixInputs strings.Builder
	for i := 0; i < valid; i++ {
		mixInputs.WriteString(fmt.Sprintf("[a%d]", i))
	}
	filter := strings.Join(filters, ";") + ";" + mixInputs.String() +
		fmt.Sprintf("amix=inputs=%d:duration=first:dropout_transition=0[aout]", valid)

	videoDur, _ := ffmpeg.ProbeMediaDuration(videoOnly)
	args := []string{"-y"}
	args = append(args, inputs...)
	args = append(args, "-filter_complex", filter, "-map", "0:v", "-map", "[aout]",
		"-c:v", "copy", "-c:a", "aac")
	if videoDur > 0 {
		args = append(args, "-t", fmt.Sprintf("%.3f", videoDur))
	}
	args = append(args, dest)
	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mix audio: %s", string(out))
	}
	return nil
}

// generateClipFromImage creates a Ken Burns style mp4 when AI video is unavailable.
func generateClipFromImage(imageURL, outputDir, dest string, duration float64, width, height int) error {
	local, ok := fsutil.PublicURLToLocal(outputDir, imageURL)
	if !ok {
		return fmt.Errorf("image not found: %s", imageURL)
	}
	if duration <= 0 {
		duration = 3
	}
	if width <= 0 {
		width = 720
	}
	if height <= 0 {
		height = 1280
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	fps := 24
	frames := int(duration * float64(fps))
	if frames < fps {
		frames = fps
	}
	size := fmt.Sprintf("%dx%d", width, height)
	zoom := fmt.Sprintf("zoompan=z='min(zoom+0.0018,1.35)':d=%d:x='iw/2-(iw/zoom/2)':y='ih/2-(ih/zoom/2)':s=%s:fps=%d",
		frames, size, fps)
	vf := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,%s",
		width, height, width, height, zoom)
	args := []string{
		"-y", "-loop", "1", "-i", local,
		"-c:v", "libx264", "-t", strconv.FormatFloat(duration, 'f', 2, 64),
		"-pix_fmt", "yuv420p", "-vf", vf,
		dest,
	}
	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg image clip: %s", string(out))
	}
	return nil
}
