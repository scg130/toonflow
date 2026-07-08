package media

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"toonflow/adapter"
	"toonflow/service/asset"
	"toonflow/service/internal/duration"
	"toonflow/service/internal/fsutil"
	"toonflow/service/project"
	"toonflow/service/storyboard"
	"toonflow/task"
)

// ShotHasAllBeatImages reports whether every beat in a shot has generated keyframe media.
func ShotHasAllBeatImages(it task.StoryboardItem) bool {
	return storyboard.ShotHasAllBeatImages(it)
}

// SelectEvenKeyframeURLs picks up to n URLs evenly spaced across the slice.
func SelectEvenKeyframeURLs(urls []string, n int) []string {
	if n <= 0 || len(urls) == 0 {
		return nil
	}
	if len(urls) <= n {
		return urls
	}
	if n == 1 {
		return []string{urls[0]}
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		idx := int(float64(i)*float64(len(urls)-1)/float64(n-1) + 0.5)
		out = append(out, urls[idx])
	}
	return out
}

// ResolveShotKeyframeCDNURLs returns Agnes CDN URLs for beat keyframes in time order (capped at MaxBeatsPerShot).
func ResolveShotKeyframeCDNURLs(ctx context.Context, db *sql.DB, v adapter.Vendor, outputDir, projectID, episodeID string, shot *storyboard.ShotMeta) ([]string, error) {
	if shot == nil {
		return nil, fmt.Errorf("invalid shot")
	}
	if len(shot.Beats) < 2 {
		u, err := ResolveShotImageCDNURL(ctx, db, v, outputDir, projectID, episodeID, shot)
		if err != nil {
			return nil, err
		}
		if u == "" {
			return nil, fmt.Errorf("请先生成第 %d 镜关键帧图片", shot.ShotNumber)
		}
		return []string{u, u}, nil
	}
	pub, canPublish := v.(adapter.ImageCDNPublisher)
	var urls []string
	for i, b := range shot.Beats {
		if u := beatCDNURL(b); u != "" {
			urls = append(urls, u)
			continue
		}
		if b.ImageURL == "" {
			return nil, fmt.Errorf("请先生成第 %d 镜第 %d 个关键帧图片", shot.ShotNumber, i+1)
		}
		local, ok := fsutil.PublicURLToLocal(outputDir, b.ImageURL)
		if !ok {
			return nil, fmt.Errorf("第 %d 镜关键帧 %d 缺少 Agnes CDN 地址，请重新生成", shot.ShotNumber, i+1)
		}
		if !canPublish {
			return nil, fmt.Errorf("第 %d 镜关键帧 %d 缺少 Agnes CDN 地址，请重新生成", shot.ShotNumber, i+1)
		}
		remote, err := pub.PublishImageForVideo(ctx, local)
		if err != nil {
			return nil, fmt.Errorf("上传第 %d 镜关键帧 %d 失败: %w", shot.ShotNumber, i+1, err)
		}
		urls = append(urls, remote)
	}
	if len(urls) < 2 {
		return nil, fmt.Errorf("第 %d 镜至少需要 2 张关键帧图片才能生成视频", shot.ShotNumber)
	}
	return SelectEvenKeyframeURLs(urls, duration.MaxBeatsPerShot), nil
}

func beatCDNURL(b task.ShotBeat) string {
	if adapter.IsCDNImageURL(b.ImageRemoteURL) {
		return b.ImageRemoteURL
	}
	if adapter.IsCDNImageURL(b.ImageURL) {
		return b.ImageURL
	}
	return ""
}

// LastBeatCDNURL returns the last beat's CDN URL for cross-shot continuity.
func LastBeatCDNURL(shot *storyboard.ShotMeta) string {
	if shot == nil || len(shot.Beats) == 0 {
		if shot != nil {
			if u := beatCDNURL(task.ShotBeat{ImageURL: shot.ImageURL, ImageRemoteURL: shot.ImageRemoteURL}); u != "" {
				return u
			}
		}
		return ""
	}
	last := shot.Beats[len(shot.Beats)-1]
	return beatCDNURL(last)
}

// BuildBeatImagePrompt composes an image prompt for one keyframe beat.
func BuildBeatImagePrompt(item task.StoryboardItem, beat task.ShotBeat, style, videoRatio, assetPrompt, styleAnchor string) string {
	base := project.BuildShotImagePrompt(item, style, videoRatio, assetPrompt, styleAnchor)
	action := strings.TrimSpace(beat.Action)
	if action == "" {
		action = strings.TrimSpace(item.Description)
	}
	return fmt.Sprintf("%s, keyframe still at %.1fs: %s, exact moment frozen frame, high fidelity composition",
		base, beat.Time, action)
}

// GenerateShotKeyframeImages generates one still per beat and persists URLs on the storyboard.
func GenerateShotKeyframeImages(ctx context.Context, db *sql.DB, v adapter.Vendor, imageModel, outputDir, projectID, episodeID string,
	shotNumber int, taskID, artStyle, videoRatio string) ([]task.ShotBeat, error) {
	shot, err := storyboard.LoadShot(db, projectID, episodeID, shotNumber)
	if err != nil {
		return nil, err
	}
	if len(shot.Beats) < 2 {
		return nil, fmt.Errorf("第 %d 镜缺少时间节点方案(beats)，请重新生成分镜", shotNumber)
	}
	shot.Beats = storyboard.CapShotBeats(shot.Beats, shot.Duration, shot.Description)
	if imageModel == "" {
		imageModel = adapter.DefaultImageModel
	}
	styleAnchor := project.LoadProjectStyleAnchor(db, projectID)
	item := shotToStoryboardItem(shot)
	item = asset.SanitizeStoryboardItemForImage(db, projectID, item)
	refURL, assetPrompt, _ := asset.ShotImageParams(db, projectID, item)
	assets, _ := asset.LoadProjectAssets(db, projectID)
	aspect := project.ResolutionToVideoRatio(videoRatioToResolution(videoRatio))

	beats := make([]task.ShotBeat, len(shot.Beats))
	copy(beats, shot.Beats)
	imgDir := keyframeImageDir(outputDir, taskID, projectID, episodeID)

	for i := range beats {
		if beats[i].ImageURL != "" || beats[i].ImageRemoteURL != "" {
			continue
		}
		prompt := BuildBeatImagePrompt(item, beats[i], artStyle, videoRatio, assetPrompt, styleAnchor)
		if len(assets) > 0 {
			prompt = asset.SanitizeFinalImagePrompt(prompt, item, assets)
		}
		resp, err := RequestShotImageWithRetry(ctx, v, imageModel, aspect, prompt, refURL)
		if err != nil {
			return nil, fmt.Errorf("第 %d 镜关键帧 %d: %w", shotNumber, i+1, err)
		}
		localPath := keyframeLocalPath(imgDir, shotNumber, i)
		if err := saveShotImageResponse(ctx, localPath, resp); err != nil {
			return nil, err
		}
		publicURL := keyframePublicURL(taskID, projectID, episodeID, shotNumber, i)
		remote := publishImageRemote(ctx, v, localPath, resp)
		beats[i].ImageURL = publicURL
		beats[i].ImageRemoteURL = remote
	}

	if err := storyboard.UpdateStoryboardShotKeyframes(db, projectID, episodeID, shotNumber, beats); err != nil {
		return nil, err
	}
	return beats, nil
}

func shotToStoryboardItem(shot *storyboard.ShotMeta) task.StoryboardItem {
	if shot == nil {
		return task.StoryboardItem{}
	}
	return task.StoryboardItem{
		ShotNumber: shot.ShotNumber, Scene: "", Description: shot.Description,
		Camera: shot.Camera, Duration: shot.Duration, Prompt: shot.Prompt,
		Lighting: shot.Lighting, ActionContinue: shot.ActionContinue,
		Dialogue: shot.Dialogue, Beats: shot.Beats,
		ImageURL: shot.ImageURL, ImageRemoteURL: shot.ImageRemoteURL,
	}
}

func videoRatioToResolution(ratio string) string {
	switch strings.TrimSpace(ratio) {
	case "9:16", "720x1280", "1080x1920":
		return "720x1280"
	default:
		return "1280x720"
	}
}

func keyframeImageDir(outputDir, taskID, projectID, episodeID string) string {
	if taskID != "" {
		return fmt.Sprintf("%s/%s", outputDir, taskID)
	}
	return fmt.Sprintf("%s/keyframes/%s/%s", outputDir, projectID, episodeID)
}

func keyframeLocalPath(dir string, shotNumber, beatIdx int) string {
	return fmt.Sprintf("%s/shot_%03d_k%d.png", dir, shotNumber, beatIdx)
}

func keyframePublicURL(taskID, projectID, episodeID string, shotNumber, beatIdx int) string {
	if taskID != "" {
		return fmt.Sprintf("/output/%s/shot_%03d_k%d.png", taskID, shotNumber, beatIdx)
	}
	return fmt.Sprintf("/output/keyframes/%s/%s/shot_%03d_k%d.png", projectID, episodeID, shotNumber, beatIdx)
}

func publishImageRemote(ctx context.Context, v adapter.Vendor, localPath string, resp *adapter.ImageResponse) string {
	if resp != nil && adapter.IsCDNImageURL(resp.RemoteURL) {
		return resp.RemoteURL
	}
	if resp != nil && adapter.IsCDNImageURL(resp.DataURL) {
		return resp.DataURL
	}
	if pub, ok := v.(adapter.ImageCDNPublisher); ok {
		if u, err := pub.PublishImageForVideo(ctx, localPath); err == nil && adapter.IsCDNImageURL(u) {
			return u
		}
	}
	return ""
}

func saveShotImageResponse(ctx context.Context, path string, resp *adapter.ImageResponse) error {
	if resp == nil {
		return fmt.Errorf("empty image response")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if resp.DataURL != "" && !strings.HasPrefix(resp.DataURL, "http://") && !strings.HasPrefix(resp.DataURL, "https://") {
		idx := strings.Index(resp.DataURL, "base64,")
		if idx == -1 {
			return fmt.Errorf("invalid data URL")
		}
		decoded, err := base64.StdEncoding.DecodeString(resp.DataURL[idx+7:])
		if err != nil {
			return fmt.Errorf("decode base64: %w", err)
		}
		return os.WriteFile(path, decoded, 0644)
	}
	if strings.HasPrefix(resp.DataURL, "http://") || strings.HasPrefix(resp.DataURL, "https://") {
		return adapter.DownloadHTTPURL(ctx, path, resp.DataURL)
	}
	return fmt.Errorf("no image data in response")
}
