package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"toonflow/adapter"
	"toonflow/logger"
)

// ShotClip is one generated video version for a storyboard shot.
type ShotClip struct {
	ID             string  `json:"id"`
	ProjectID      string  `json:"project_id"`
	EpisodeID      string  `json:"episode_id"`
	ShotNumber     int     `json:"shot_number"`
	Version        int     `json:"version"`
	Prompt         string  `json:"prompt,omitempty"`
	SourceImageURL string  `json:"source_image_url,omitempty"`
	FileURL        string  `json:"file_url,omitempty"`
	Duration       float64 `json:"duration"`
	Status         string  `json:"status"`
	Source         string  `json:"source,omitempty"` // ai | fallback
	IsSelected     bool    `json:"is_selected"`
	CreatedAt      string  `json:"created_at,omitempty"`
}

// ListShotClips returns all clip versions for a project episode.
func ListShotClips(db *sql.DB, projectID, episodeID string) ([]ShotClip, error) {
	rows, err := db.Query(`
		SELECT id, project_id, episode_id, shot_number, version, prompt, source_image_url,
		       file_url, duration, status, COALESCE(source, 'ai'), is_selected, created_at
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
			&c.Prompt, &c.SourceImageURL, &c.FileURL, &c.Duration, &c.Status, &c.Source, &selected, &createdAt); err != nil {
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
}

// GenerateShotClip creates a new video version for one storyboard shot.
func GenerateShotClip(ctx context.Context, db *sql.DB, v adapter.Vendor, outputDir, projectID, episodeID string, shotNumber int, opts *ShotClipOptions) (*ShotClip, error) {
	logger.CtxTrace(ctx, "shot video generate start project=%s episode=%s shot=%d", projectID, episodeID, shotNumber)

	shot, err := loadStoryboardShot(db, projectID, episodeID, shotNumber)
	if err != nil {
		return nil, err
	}

	var artStyle, videoModel, videoRatio string
	_ = db.QueryRow("SELECT art_style, COALESCE(NULLIF(video_model,''), ''), video_ratio FROM o_project WHERE id = ?", projectID).
		Scan(&artStyle, &videoModel, &videoRatio)
	if videoModel == "" {
		videoModel = adapter.DefaultVideoModel
	}

	stylePrompt := lookupArtStylePrompt(db, artStyle)
	prompt, negativePrompt := buildShotVideoPrompt(shot, artStyle, stylePrompt)
	logger.CtxTrace(ctx, "shot video prompt shot=%d prompt=%s", shotNumber, prompt)

	width, height := videoSizeForRatio(videoRatio)
	storyboardImage, err := ResolveShotImageCDNURL(ctx, db, v, outputDir, projectID, episodeID, shot)
	if err != nil {
		return nil, err
	}
	imageInput := storyboardImage
	if opts != nil && adapter.IsCDNImageURL(opts.ContinuityImageURL) {
		imageInput = opts.ContinuityImageURL
		logger.CtxTrace(ctx, "shot video continuity frame shot=%d url=%s", shotNumber, imageInput)
	}
	if imageInput == "" {
		return nil, fmt.Errorf("请先生成第 %d 镜图片后再生成视频", shotNumber)
	}

	version, err := nextClipVersion(db, projectID, episodeID, shotNumber)
	if err != nil {
		return nil, err
	}

	clipID := fmt.Sprintf("clip_%d", time.Now().UnixNano())
	clipDir := filepath.Join(outputDir, "clips", projectID, episodeID)
	if err := os.MkdirAll(clipDir, 0755); err != nil {
		return nil, err
	}
	localFile := filepath.Join(clipDir, fmt.Sprintf("shot_%03d_v%d.mp4", shotNumber, version))
	duration := ResolveShotVideoDuration(shot.Duration)

	var fileURL string
	var source string
	var apiErr error
	if v != nil && imageInput != "" {
		resp, err := v.VideoRequest(ctx, videoModel, adapter.VideoParams{
			Prompt:   prompt,
			ImageURL: imageInput,
			Model:    videoModel,
			Duration: float32(duration),
			Width:    width,
			Height:   height,
			Negative: negativePrompt,
		})
		if err == nil && resp != nil && resp.VideoURL != "" {
			if dlErr := downloadFile(ctx, resp.VideoURL, localFile); dlErr != nil {
				apiErr = dlErr
			logger.CtxError(ctx, dlErr, "agnes video download failed shot=%d", shotNumber)
			} else {
				fileURL = clipPublicURL(projectID, episodeID, shotNumber, version)
				source = "ai"
			}
		} else if err != nil {
			apiErr = err
			logger.CtxError(ctx, err, "agnes video request failed shot=%d", shotNumber)
		}
	} else if imageInput == "" && shot.ImageURL != "" {
		apiErr = fmt.Errorf("缺少 Agnes 图片远程地址（24h URL），请重新生成该分镜图片后再生成视频")
		logger.CtxTrace(ctx, "shot video missing remote image url shot=%d", shotNumber)
	}
	if fileURL == "" {
		if apiErr != nil {
			return nil, apiErr
		}
		if imageInput == "" {
			return nil, fmt.Errorf("请先生成第 %d 镜图片后再生成视频", shotNumber)
		}
		return nil, fmt.Errorf("AI 图生视频未返回视频结果，请稍后重试")
	}

	_, _ = db.Exec(`UPDATE o_shot_clip SET is_selected = 0 WHERE project_id = ? AND episode_id = ? AND shot_number = ?`,
		projectID, episodeID, shotNumber)

	isFirst, _ := isFirstClipVersion(db, projectID, episodeID, shotNumber)
	selected := 0
	if isFirst {
		selected = 1
	}

	_, err = db.Exec(`
		INSERT INTO o_shot_clip (id, project_id, episode_id, shot_number, version, prompt, source_image_url, file_url, duration, status, source, is_selected)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'ready', ?, ?)`,
		clipID, projectID, episodeID, shotNumber, version, prompt, shot.ImageURL, fileURL, duration, source, selected)
	if err != nil {
		return nil, err
	}

	return &ShotClip{
		ID: clipID, ProjectID: projectID, EpisodeID: episodeID, ShotNumber: shotNumber,
		Version: version, Prompt: prompt, SourceImageURL: shot.ImageURL, FileURL: fileURL,
		Duration: duration, Status: "ready", Source: source, IsSelected: isFirst,
	}, nil
}

// GenerateShotClipsSequential generates clips in shot order, chaining each shot from the previous clip's last frame.
func GenerateShotClipsSequential(ctx context.Context, db *sql.DB, v adapter.Vendor, outputDir, projectID, episodeID string, shotNumbers []int) ([]*ShotClip, error) {
	ordered := SortShotNumbers(shotNumbers)
	if len(ordered) == 0 {
		return nil, fmt.Errorf("请至少选择一个分镜")
	}

	workDir, err := os.MkdirTemp(filepath.Join(outputDir, "clips", projectID, episodeID), "chain_")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)

	var clips []*ShotClip
	var continuityURL string
	for i, shotNum := range ordered {
		var opts *ShotClipOptions
		if i > 0 && continuityURL != "" {
			opts = &ShotClipOptions{ContinuityImageURL: continuityURL}
		}
		clip, err := GenerateShotClip(ctx, db, v, outputDir, projectID, episodeID, shotNum, opts)
		if err != nil {
			return clips, fmt.Errorf("第 %d 镜: %w", shotNum, err)
		}
		clips = append(clips, clip)

		local, ok := publicURLToLocal(outputDir, clip.FileURL)
		if !ok {
			logger.CtxTrace(ctx, "continuity skip shot=%d: clip file not local", shotNum)
			continuityURL = ""
			continue
		}
		nextURL, err := ContinuityFrameFromClip(ctx, v, outputDir, local, workDir, shotNum)
		if err != nil {
			logger.CtxTrace(ctx, "continuity frame failed shot=%d: %v", shotNum, err)
			continuityURL = ""
			continue
		}
		continuityURL = nextURL
	}
	return clips, nil
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
	if local, ok := publicURLToLocal(outputDir, fileURL); ok {
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

func loadStoryboardShot(db *sql.DB, projectID, episodeID string, shotNumber int) (*storyboardShot, error) {
	sbID := fmt.Sprintf("sb_%s_%s", projectID, episodeID)
	var shotsJSON string
	err := db.QueryRow(`SELECT shots FROM o_storyboard WHERE id = ?`, sbID).Scan(&shotsJSON)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("分镜不存在")
	}
	if err != nil {
		return nil, err
	}
	items, err := parseStoryboardResponse(shotsJSON)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if it.ShotNumber == shotNumber {
			return &storyboardShot{
				ShotNumber:     it.ShotNumber,
				Description:    it.Description,
				Prompt:         it.Prompt,
				Camera:         it.Camera,
				Duration:       it.Duration,
				ImageURL:       it.ImageURL,
				ImageRemoteURL: it.ImageRemoteURL,
			}, nil
		}
	}
	return nil, fmt.Errorf("未找到第 %d 镜", shotNumber)
}

type storyboardShot struct {
	ShotNumber     int
	Description    string
	Prompt         string
	Camera         string
	Duration       float64
	ImageURL       string
	ImageRemoteURL string
}

func nextClipVersion(db *sql.DB, projectID, episodeID string, shotNumber int) (int, error) {
	var maxVer int
	err := db.QueryRow(`
		SELECT COALESCE(MAX(version), 0) FROM o_shot_clip
		WHERE project_id = ? AND episode_id = ? AND shot_number = ?`,
		projectID, episodeID, shotNumber).Scan(&maxVer)
	return maxVer + 1, err
}

func isFirstClipVersion(db *sql.DB, projectID, episodeID string, shotNumber int) (bool, error) {
	var n int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM o_shot_clip WHERE project_id = ? AND episode_id = ? AND shot_number = ?`,
		projectID, episodeID, shotNumber).Scan(&n)
	return n == 0, err
}

func clipPublicURL(projectID, episodeID string, shotNumber, version int) string {
	return fmt.Sprintf("/output/clips/%s/%s/shot_%03d_v%d.mp4", projectID, episodeID, shotNumber, version)
}

func downloadFile(ctx context.Context, url, dest string) error {
	return adapter.DownloadHTTPURL(ctx, dest, url)
}

func publicURLToLocal(outputDir, fileURL string) (string, bool) {
	rel := strings.TrimPrefix(fileURL, "/output/")
	if rel == fileURL || strings.Contains(rel, "..") {
		return "", false
	}
	localPath := filepath.Join(outputDir, rel)
	absPath, err := filepath.Abs(localPath)
	if err != nil {
		absPath = localPath
	}
	if _, err := os.Stat(absPath); err != nil {
		return "", false
	}
	return absPath, true
}

func imageURLForVideo(shot *storyboardShot) string {
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

func lookupArtStylePrompt(db *sql.DB, artStyle string) string {
	if artStyle == "" {
		return ""
	}
	var prompt string
	err := db.QueryRow(`SELECT prompt FROM o_artStyle WHERE name = ?`, artStyle).Scan(&prompt)
	if err != nil || strings.TrimSpace(prompt) == "" {
		return ""
	}
	return strings.TrimSpace(prompt)
}

func mapCameraToVideoMotion(camera string) string {
	c := strings.TrimSpace(camera)
	if c == "" {
		return "cinematic camera with subtle motivated movement"
	}
	lower := strings.ToLower(c)
	switch {
	case strings.Contains(lower, "dolly zoom") || strings.Contains(lower, "希区库克") || strings.Contains(lower, "vertigo"):
		return "dolly zoom vertigo effect, background compression while subject scales"
	case strings.Contains(lower, "rack focus") || strings.Contains(lower, "跟焦") || strings.Contains(lower, "移焦"):
		return "rack focus pull between foreground and background planes"
	case strings.Contains(lower, "slow motion") || strings.Contains(lower, "慢镜头") || strings.Contains(lower, "升格"):
		return "slow motion high frame rate capture, smooth temporal detail"
	case strings.Contains(lower, "推近") || strings.Contains(lower, "push") || strings.Contains(lower, "dolly in"):
		return "slow cinematic dolly push-in toward subject"
	case strings.Contains(lower, "拉远") || strings.Contains(lower, "pull") || strings.Contains(lower, "dolly out"):
		return "smooth dolly pull-back revealing environment"
	case strings.Contains(lower, "环绕") || strings.Contains(lower, "orbit"):
		return "orbital camera circling around subject"
	case strings.Contains(lower, "仰拍") || strings.Contains(lower, "low angle"):
		return "low angle heroic camera looking up at subject"
	case strings.Contains(lower, "俯拍") || strings.Contains(lower, "high angle"):
		return "high angle overhead establishing shot"
	case strings.Contains(lower, "跟拍") || strings.Contains(lower, "tracking"):
		return "tracking shot following subject movement"
	case strings.Contains(lower, "摇") || strings.Contains(lower, "pan"):
		return "smooth horizontal pan camera movement"
	case strings.Contains(lower, "tilt") || strings.Contains(lower, "俯仰"):
		return "cinematic tilt camera movement"
	case strings.Contains(lower, "crane") || strings.Contains(lower, "升降"):
		return "crane shot vertical camera movement"
	case strings.Contains(lower, "固定") || strings.Contains(lower, "静止") || strings.Contains(lower, "static"):
		return "locked-off tripod shot with living scene motion"
	case strings.Contains(lower, "手持") || strings.Contains(lower, "handheld"):
		return "subtle handheld documentary camera energy"
	case strings.Contains(lower, "航拍") || strings.Contains(lower, "drone"):
		return "aerial drone flyover cinematic movement"
	default:
		return "camera motion: " + c
	}
}
