package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"toonflow/adapter"
	"toonflow/logger"
)

// ExtractLastVideoFrame saves the last frame of a local mp4 as PNG.
func ExtractLastVideoFrame(videoPath, destPNG string) error {
	if err := os.MkdirAll(filepath.Dir(destPNG), 0755); err != nil {
		return err
	}
	args := []string{
		"-y", "-sseof", "-0.04", "-i", videoPath,
		"-frames:v", "1", "-q:v", "2", destPNG,
	}
	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("extract last frame: %w: %s", err, string(out))
	}
	if _, err := os.Stat(destPNG); err != nil {
		return fmt.Errorf("extract last frame: output missing: %w", err)
	}
	return nil
}

// PublishContinuityFrame uploads a local PNG and returns an Agnes CDN URL for I2V chaining.
func PublishContinuityFrame(ctx context.Context, v adapter.Vendor, localPNG string) (string, error) {
	pub, ok := v.(adapter.ImageCDNPublisher)
	if !ok {
		return "", fmt.Errorf("当前模型不支持上传连贯参考帧")
	}
	url, err := pub.PublishImageForVideo(ctx, localPNG)
	if err != nil {
		return "", err
	}
	if !adapter.IsCDNImageURL(url) {
		return "", fmt.Errorf("连贯参考帧上传未返回有效 CDN URL")
	}
	return url, nil
}

// ContinuityFrameFromClip extracts and publishes the last frame of a generated clip.
func ContinuityFrameFromClip(ctx context.Context, v adapter.Vendor, outputDir, clipLocalPath, workDir string, shotNumber int) (string, error) {
	framePath := filepath.Join(workDir, fmt.Sprintf("continuity_shot_%03d.png", shotNumber))
	if err := ExtractLastVideoFrame(clipLocalPath, framePath); err != nil {
		return "", err
	}
	url, err := PublishContinuityFrame(ctx, v, framePath)
	if err != nil {
		return "", err
	}
	logger.CtxTrace(ctx, "continuity frame published shot=%d url=%s", shotNumber, url)
	return url, nil
}
