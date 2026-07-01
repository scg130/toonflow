package service

import (
	"context"
	"database/sql"
	"fmt"

	"toonflow/adapter"
)

// ResolveShotImageCDNURL returns an Agnes CDN URL for video I2V, uploading local files if needed.
func ResolveShotImageCDNURL(ctx context.Context, db *sql.DB, v adapter.Vendor, outputDir, projectID, episodeID string, shot *storyboardShot) (string, error) {
	if shot == nil {
		return "", fmt.Errorf("invalid shot")
	}
	if u := imageURLForVideo(shot); u != "" {
		return u, nil
	}
	if shot.ImageURL == "" {
		return "", fmt.Errorf("请先生成第 %d 镜图片", shot.ShotNumber)
	}
	local, ok := publicURLToLocal(outputDir, shot.ImageURL)
	if !ok {
		return "", fmt.Errorf("缺少 Agnes 图片远程地址（24h URL），请重新生成该分镜图片后再生成视频")
	}
	pub, ok := v.(adapter.ImageCDNPublisher)
	if !ok {
		return "", fmt.Errorf("缺少 Agnes 图片远程地址（24h URL），请重新生成该分镜图片后再生成视频")
	}
	remote, err := pub.PublishImageForVideo(ctx, local)
	if err != nil {
		return "", fmt.Errorf("上传分镜图片失败: %w", err)
	}
	if db != nil && projectID != "" && episodeID != "" {
		_ = UpdateStoryboardShotMedia(db, projectID, episodeID, shot.ShotNumber, shot.ImageURL, remote)
	}
	return remote, nil
}
