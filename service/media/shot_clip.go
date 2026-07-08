package media

import (
	"toonflow/service/asset"
	"toonflow/service/storyboard"
	"toonflow/service/project"
	"toonflow/service/internal/fsutil"
	"toonflow/task"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"toonflow/adapter"
	"toonflow/logger"
	"toonflow/service/internal/ffmpeg"
)

// ShotClip is one generated video version for a storyboard shot.
type ShotClip struct {
	ID              string  `json:"id"`
	ProjectID       string  `json:"project_id"`
	EpisodeID       string  `json:"episode_id"`
	ShotNumber      int     `json:"shot_number"`
	Version         int     `json:"version"`
	Prompt          string  `json:"prompt,omitempty"`
	SourceImageURL  string  `json:"source_image_url,omitempty"`
	FileURL         string  `json:"file_url,omitempty"`
	Duration        float64 `json:"duration"`
	Status          string  `json:"status"`
	Source          string  `json:"source,omitempty"` // ai | fallback
	IsSelected      bool    `json:"is_selected"`
	CoherenceScore  float64 `json:"coherence_score,omitempty"`
	CoherenceJSON   string  `json:"coherence_json,omitempty"`
	ComposedFileURL string  `json:"composed_file_url,omitempty"`
	CreatedAt       string  `json:"created_at,omitempty"`
}

// ListShotClips returns all clip versions for a project episode.
func ListShotClips(db *sql.DB, projectID, episodeID string) ([]ShotClip, error) {
	rows, err := db.Query(`
		SELECT id, project_id, episode_id, shot_number, version, prompt, source_image_url,
		       file_url, duration, status, COALESCE(source, 'ai'), is_selected,
		       COALESCE(coherence_score, 0), COALESCE(coherence_json, ''), COALESCE(composed_file_url, ''), created_at
		FROM o_shot_clip
		WHERE project_id = ? AND episode_id = ?
		ORDER BY shot_number ASC, version ASC`, projectID, episodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clips []ShotClip
	for rows.Next() {
		var c ShotClip
		var selected int
		var createdAt time.Time
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.EpisodeID, &c.ShotNumber, &c.Version,
			&c.Prompt, &c.SourceImageURL, &c.FileURL, &c.Duration, &c.Status, &c.Source, &selected,
			&c.CoherenceScore, &c.CoherenceJSON, &c.ComposedFileURL, &createdAt); err != nil {
			continue
		}
		c.IsSelected = selected == 1
		c.CreatedAt = createdAt.Format(time.RFC3339)
		clips = append(clips, c)
	}
	return clips, nil
}

// ShotClipOptions optional controls for chained video generation.
type ShotClipOptions struct {
	// ContinuityImageURL is the previous clip's last frame (Agnes CDN). When set, used as I2V input.
	ContinuityImageURL string
	// Versions generates multiple AI versions and auto-selects the highest coherence score (max 2).
	Versions int
}

// GenerateShotClip creates a new video version for one storyboard shot.
func GenerateShotClip(ctx context.Context, db *sql.DB, v adapter.Vendor, outputDir, projectID, episodeID string, shotNumber int, opts *ShotClipOptions) (*ShotClip, error) {
	logger.CtxTrace(ctx, "shot video generate start project=%s episode=%s shot=%d", projectID, episodeID, shotNumber)

	shot, err := storyboard.LoadShot(db, projectID, episodeID, shotNumber)
	if err != nil {
		return nil, err
	}

	var artStyle, videoModel, videoRatio string
	_ = db.QueryRow("SELECT art_style, COALESCE(NULLIF(video_model,''), ''), video_ratio FROM o_project WHERE id = ?", projectID).
		Scan(&artStyle, &videoModel, &videoRatio)
	if videoModel == "" {
		videoModel = adapter.DefaultVideoModel
	}

	stylePrompt := project.LookupArtStylePrompt(db, artStyle)
	styleAnchor := project.LoadProjectStyleAnchor(db, projectID)
	assets, _ := asset.LoadProjectAssets(db, projectID)
	humanSubject := len(assets) == 0 || asset.ShotHasHumanRole(shotToItem(shot), assets)
	prompt, negativePrompt := buildShotVideoPrompt(shot, artStyle, stylePrompt, styleAnchor, humanSubject)
	logger.CtxTrace(ctx, "shot video prompt shot=%d prompt=%s", shotNumber, prompt)

	width, height := videoSizeForRatio(videoRatio)

	// Continuity: prepend previous shot's last keyframe for same-scene links.
	continuityFrame := ""
	switch {
	case opts != nil && adapter.IsCDNImageURL(opts.ContinuityImageURL):
		continuityFrame = opts.ContinuityImageURL
		logger.CtxTrace(ctx, "shot video continuity keyframe shot=%d url=%s", shotNumber, continuityFrame)
	case shot.SceneLink == task.SceneLinkContinuous:
		if url := resolvePrevShotLastKeyframe(ctx, db, outputDir, projectID, episodeID, shotNumber, v); url != "" {
			continuityFrame = url
			logger.CtxTrace(ctx, "shot video derived continuity keyframe shot=%d url=%s", shotNumber, continuityFrame)
		}
	}

	keyframeURLs, err := ResolveShotKeyframeCDNURLs(ctx, db, v, outputDir, projectID, episodeID, shot)
	if err != nil {
		return nil, err
	}
	if continuityFrame != "" && (len(keyframeURLs) == 0 || keyframeURLs[0] != continuityFrame) {
		keyframeURLs = append([]string{continuityFrame}, keyframeURLs...)
	}
	if len(keyframeURLs) < 2 {
		return nil, fmt.Errorf("第 %d 镜至少需要 2 张关键帧图片，请先生成关键帧", shotNumber)
	}

	duration := ResolveShotVideoDuration(shot.Duration)
	logger.CtxTrace(ctx, "shot video duration shot=%d requested=%.1fs", shotNumber, duration)

	version, err := nextClipVersion(db, projectID, episodeID, shotNumber)
	if err != nil {
		return nil, err
	}

	versions := 1
	if opts != nil && opts.Versions > 1 {
		versions = opts.Versions
		if versions > 2 {
			versions = 2
		}
	}

	var candidates []*ShotClip
	for vi := 0; vi < versions; vi++ {
		clip, genErr := generateOneShotClip(ctx, db, v, outputDir, projectID, episodeID, shotNumber, shot,
			prompt, negativePrompt, keyframeURLs, videoModel, width, height, duration, version+vi)
		if genErr != nil {
			return nil, genErr
		}
		candidates = append(candidates, clip)
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("AI 图生视频未返回视频结果，请稍后重试")
	}

	best, score, _ := SelectBestScoredClip(ctx, outputDir, derefClips(candidates), shot)
	if best == nil {
		best = candidates[len(candidates)-1]
	}

	// Mark best version selected; demote others.
	_, _ = db.Exec(`UPDATE o_shot_clip SET is_selected = 0 WHERE project_id = ? AND episode_id = ? AND shot_number = ?`,
		projectID, episodeID, shotNumber)
	_, _ = db.Exec(`UPDATE o_shot_clip SET is_selected = 1 WHERE id = ?`, best.ID)
	if score != nil {
		_, _ = db.Exec(`UPDATE o_shot_clip SET coherence_score = ?, coherence_json = ? WHERE id = ?`,
			score.Total, CoherenceScoreJSON(score), best.ID)
		best.CoherenceScore = score.Total
		best.CoherenceJSON = CoherenceScoreJSON(score)
	}
	best.IsSelected = true
	logger.CtxTrace(ctx, "shot video done shot=%d version=%d coherence=%.1f", shotNumber, best.Version, best.CoherenceScore)
	return best, nil
}

// resolvePrevShotContinuityFrame extracts and publishes the previous shot's
// selected clip last frame as a CDN URL for same-scene I2V continuity. Returns
// "" (caller falls back to this shot's own image) if unavailable.
func resolvePrevShotContinuityFrame(ctx context.Context, db *sql.DB, v adapter.Vendor, outputDir, projectID, episodeID string, shotNumber int) string {
	if shotNumber <= 1 {
		return ""
	}
	prev, err := SelectedClipForShot(db, projectID, episodeID, shotNumber-1)
	if err != nil || prev == nil || prev.FileURL == "" {
		return ""
	}
	local, ok := fsutil.PublicURLToLocal(outputDir, prev.FileURL)
	if !ok {
		return ""
	}
	workDir, err := os.MkdirTemp(filepath.Join(outputDir, "clips", projectID, episodeID), "cont_")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(workDir)
	url, err := ContinuityFrameFromClip(ctx, v, outputDir, local, workDir, shotNumber)
	if err != nil {
		logger.CtxTrace(ctx, "derive continuity frame failed shot=%d: %v", shotNumber, err)
		return ""
	}
	return url
}

func derefClips(ptrs []*ShotClip) []ShotClip {
	out := make([]ShotClip, len(ptrs))
	for i, p := range ptrs {
		out[i] = *p
	}
	return out
}

func shotToItem(shot *storyboard.ShotMeta) task.StoryboardItem {
	if shot == nil {
		return task.StoryboardItem{}
	}
	return task.StoryboardItem{
		ShotNumber:  shot.ShotNumber,
		Description: shot.Description,
		Prompt:      shot.Prompt,
		Camera:      shot.Camera,
	}
}

// resolvePrevShotLastKeyframe returns the previous shot's last beat CDN URL for continuity.
func resolvePrevShotLastKeyframe(ctx context.Context, db *sql.DB, outputDir, projectID, episodeID string, shotNumber int, v adapter.Vendor) string {
	if shotNumber <= 1 {
		return ""
	}
	prev, err := storyboard.LoadShot(db, projectID, episodeID, shotNumber-1)
	if err != nil || prev == nil {
		return ""
	}
	if u := LastBeatCDNURL(prev); u != "" {
		return u
	}
	// Fallback: extract last frame from previous clip if keyframes missing.
	return resolvePrevShotContinuityFrame(ctx, db, v, outputDir, projectID, episodeID, shotNumber)
}

func generateOneShotClip(ctx context.Context, db *sql.DB, v adapter.Vendor, outputDir, projectID, episodeID string,
	shotNumber int, shot *storyboard.ShotMeta, prompt, negativePrompt string, keyframeURLs []string, videoModel string,
	width, height int, duration float64, version int) (*ShotClip, error) {

	clipID := fmt.Sprintf("clip_%d", time.Now().UnixNano())
	clipDir := filepath.Join(outputDir, "clips", projectID, episodeID)
	if err := os.MkdirAll(clipDir, 0755); err != nil {
		return nil, err
	}
	localFile := filepath.Join(clipDir, fmt.Sprintf("shot_%03d_v%d.mp4", shotNumber, version))

	if v == nil || len(keyframeURLs) < 2 {
		return nil, fmt.Errorf("第 %d 镜至少需要 2 张关键帧图片才能生成视频", shotNumber)
	}

	params := adapter.VideoParams{
		Prompt:    prompt,
		Model:     videoModel,
		Duration:  float32(duration),
		Width:     width,
		Height:    height,
		Negative:  negativePrompt,
		Keyframes: keyframeURLs,
	}

	err := RequestShotVideoWithRetry(ctx, v, videoModel, params, localFile)
	if err != nil {
		return nil, err
	}

	if probed, probeErr := ffmpeg.ProbeMediaDuration(localFile); probeErr == nil && probed > 0 {
		duration = probed
	}

	fileURL := clipPublicURL(projectID, episodeID, shotNumber, version)
	source := "ai"

	_, err = db.Exec(`
		INSERT INTO o_shot_clip (id, project_id, episode_id, shot_number, version, prompt, source_image_url, file_url, duration, status, source, is_selected)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'ready', ?, 0)`,
		clipID, projectID, episodeID, shotNumber, version, prompt, shot.ImageURL, fileURL, duration, source)
	if err != nil {
		return nil, err
	}

	return &ShotClip{
		ID: clipID, ProjectID: projectID, EpisodeID: episodeID, ShotNumber: shotNumber,
		Version: version, Prompt: prompt, SourceImageURL: shot.ImageURL, FileURL: fileURL,
		Duration: duration, Status: "ready", Source: source,
	}, nil
}

// SelectShotClip marks one version as the active clip for its shot.
func SelectShotClip(db *sql.DB, clipID string) error {
	var projectID, episodeID string
	var shotNumber int
	err := db.QueryRow(`SELECT project_id, episode_id, shot_number FROM o_shot_clip WHERE id = ?`, clipID).
		Scan(&projectID, &episodeID, &shotNumber)
	if err != nil {
		return err
	}
	_, _ = db.Exec(`UPDATE o_shot_clip SET is_selected = 0 WHERE project_id = ? AND episode_id = ? AND shot_number = ?`,
		projectID, episodeID, shotNumber)
	_, err = db.Exec(`UPDATE o_shot_clip SET is_selected = 1 WHERE id = ?`, clipID)
	return err
}

// DeleteShotClip removes a clip version and its file.
func DeleteShotClip(db *sql.DB, outputDir, clipID string) error {
	var fileURL string
	var isSelected int
	var projectID, episodeID string
	var shotNumber int
	err := db.QueryRow(`
		SELECT file_url, is_selected, project_id, episode_id, shot_number
		FROM o_shot_clip WHERE id = ?`, clipID).
		Scan(&fileURL, &isSelected, &projectID, &episodeID, &shotNumber)
	if err != nil {
		return err
	}
	if local, ok := fsutil.PublicURLToLocal(outputDir, fileURL); ok {
		_ = os.Remove(local)
	}
	_, err = db.Exec(`DELETE FROM o_shot_clip WHERE id = ?`, clipID)
	if err != nil {
		return err
	}
	if isSelected == 1 {
		var nextID string
		if db.QueryRow(`
			SELECT id FROM o_shot_clip
			WHERE project_id = ? AND episode_id = ? AND shot_number = ?
			ORDER BY version DESC LIMIT 1`,
			projectID, episodeID, shotNumber).Scan(&nextID) == nil {
			_, _ = db.Exec(`UPDATE o_shot_clip SET is_selected = 1 WHERE id = ?`, nextID)
		}
	}
	return nil
}

func nextClipVersion(db *sql.DB, projectID, episodeID string, shotNumber int) (int, error) {
	var maxVer int
	err := db.QueryRow(`
		SELECT COALESCE(MAX(version), 0) FROM o_shot_clip
		WHERE project_id = ? AND episode_id = ? AND shot_number = ?`,
		projectID, episodeID, shotNumber).Scan(&maxVer)
	return maxVer + 1, err
}

func clipPublicURL(projectID, episodeID string, shotNumber, version int) string {
	return fmt.Sprintf("/output/clips/%s/%s/shot_%03d_v%d.mp4", projectID, episodeID, shotNumber, version)
}

func downloadFile(ctx context.Context, url, dest string) error {
	return adapter.DownloadHTTPURL(ctx, dest, url)
}

func imageURLForVideo(shot *storyboard.ShotMeta) string {
	// 图生视频只传 Agnes CDN URL，绝不传 base64 或本地 /output/ 路径
	if adapter.IsCDNImageURL(shot.ImageRemoteURL) {
		return shot.ImageRemoteURL
	}
	if adapter.IsCDNImageURL(shot.ImageURL) {
		return shot.ImageURL
	}
	return ""
}

func videoSizeForRatio(ratio string) (int, int) {
	// Match Agnes image generation aspect ratios for better I2V consistency.
	switch strings.TrimSpace(ratio) {
	case "9:16", "720x1280", "1080x1920":
		return 576, 1024
	case "1:1":
		return 768, 768
	default:
		return 1024, 576
	}
}
