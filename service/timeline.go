package service

import (
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
}

// TimelineTrack holds clips for one track.
type TimelineTrack struct {
	Type  string         `json:"type"` // video | audio
	Clips []TimelineClip `json:"clips"`
}

// TimelineEdit is the saved editor state per episode.
type TimelineEdit struct {
	ProjectID string          `json:"project_id"`
	EpisodeID string          `json:"episode_id"`
	Tracks    []TimelineTrack `json:"tracks"`
	UpdatedAt string          `json:"updated_at,omitempty"`
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
	videoTrack := findTrack(tl, "video")
	if videoTrack == nil || len(videoTrack.Clips) == 0 {
		return "", fmt.Errorf("时间线没有视频片段，请先添加分镜视频")
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
	for i, clip := range videoTrack.Clips {
		local, ok := publicURLToLocal(outputDir, clip.FileURL)
		if !ok {
			return "", fmt.Errorf("无效片段路径: %s", clip.FileURL)
		}
		part := filepath.Join(workDir, fmt.Sprintf("part_%03d.mp4", i))
		start := clip.Start
		end := clip.End
		if end <= 0 {
			end = clip.Duration
		}
		if end <= start {
			end = start + 1
		}
		args := []string{"-y", "-ss", fmt.Sprintf("%.3f", start), "-to", fmt.Sprintf("%.3f", end), "-i", local,
			"-c:v", "libx264", "-pix_fmt", "yuv420p", "-an", part}
		if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("trim clip %d: %s", i+1, string(out))
		}
		partFiles = append(partFiles, part)
	}

	concatList := filepath.Join(workDir, "concat.txt")
	f, err := os.Create(concatList)
	if err != nil {
		return "", err
	}
	for _, p := range partFiles {
		abs, _ := filepath.Abs(p)
		fmt.Fprintf(f, "file '%s'\n", strings.ReplaceAll(abs, "'", `'\''`))
	}
	f.Close()

	videoOnly := filepath.Join(workDir, "video_only.mp4")
	if out, err := exec.Command("ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i", concatList,
		"-c:v", "libx264", "-pix_fmt", "yuv420p", videoOnly).CombinedOutput(); err != nil {
		return "", fmt.Errorf("concat video: %s", string(out))
	}

	outName := fmt.Sprintf("final_%d.mp4", time.Now().UnixNano())
	outPath := filepath.Join(exportDir, outName)
	finalPath := videoOnly

	audioTrack := findTrack(tl, "audio")
	if audioTrack != nil && len(audioTrack.Clips) > 0 {
		audioClip := audioTrack.Clips[0]
		audioLocal, ok := publicURLToLocal(outputDir, audioClip.FileURL)
		if ok {
			mixed := filepath.Join(workDir, "mixed.mp4")
			args := []string{"-y", "-i", videoOnly, "-i", audioLocal,
				"-c:v", "copy", "-c:a", "aac", "-shortest", mixed}
			if audioClip.Start > 0 {
				args = []string{"-y", "-i", videoOnly,
					"-ss", fmt.Sprintf("%.3f", audioClip.Start), "-i", audioLocal,
					"-c:v", "copy", "-c:a", "aac", "-shortest", mixed}
			}
			if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
				return "", fmt.Errorf("mix audio: %s", string(out))
			}
			finalPath = mixed
		}
	}

	if err := copyFile(finalPath, outPath); err != nil {
		return "", err
	}

	publicURL := fmt.Sprintf("/output/exports/%s/%s/%s", tl.ProjectID, tl.EpisodeID, outName)
	return publicURL, nil
}

func buildDefaultTimeline(db *sql.DB, projectID, episodeID string) (*TimelineEdit, error) {
	clips, err := ListShotClips(db, projectID, episodeID)
	if err != nil {
		return &TimelineEdit{ProjectID: projectID, EpisodeID: episodeID, Tracks: []TimelineTrack{
			{Type: "video", Clips: []TimelineClip{}},
			{Type: "audio", Clips: []TimelineClip{}},
		}}, nil
	}
	selected := map[int]ShotClip{}
	for _, c := range clips {
		if c.IsSelected && c.Status == "ready" && c.FileURL != "" {
			selected[c.ShotNumber] = c
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
			FileURL:    c.FileURL,
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
				FileURL: c.FileURL, Start: 0, End: c.Duration, Duration: c.Duration,
			})
		}
	}
	return &TimelineEdit{
		ProjectID: projectID, EpisodeID: episodeID,
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

func copyFile(src, dst string) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, in, 0644)
}

// generateClipFromImage creates a Ken Burns style mp4 when AI video is unavailable.
func generateClipFromImage(imageURL, outputDir, dest string, duration float64, width, height int) error {
	local, ok := publicURLToLocal(outputDir, imageURL)
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
