package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"toonflow/adapter"
	"toonflow/task"
)

// GenerateImages generates images for all storyboard items.
func GenerateImages(ctx context.Context, items []task.StoryboardItem, style, resolution, outputDir string, v adapter.Vendor) ([]task.ImageArtifact, error) {
	artifacts := make([]task.ImageArtifact, len(items))

	for i, item := range items {
		select {
		case <-ctx.Done():
			return artifacts, ctx.Err()
		default:
		}

		prompt := item.Description
		if style != "" {
			prompt += ", " + style + " art style"
		}
		if item.Camera != "" {
			prompt += ", camera: " + item.Camera
		}

		resp, err := v.ImageRequest(ctx, adapter.DefaultImageModel, adapter.ImageParams{
			Prompt:      prompt,
			Model:       adapter.DefaultImageModel,
			AspectRatio: resToAspect(resolution),
		})
		if err != nil {
			return artifacts, fmt.Errorf("shot %d: %w", item.ShotNumber, err)
		}

		localPath := filepath.Join(outputDir, fmt.Sprintf("shot_%03d.png", item.ShotNumber))
		if err := saveDataURL(localPath, resp.DataURL); err != nil {
			return artifacts, fmt.Errorf("shot %d save: %w", item.ShotNumber, err)
		}

		artifacts[i] = task.ImageArtifact{
			ShotNumber: item.ShotNumber,
			DataURL:    resp.DataURL,
			LocalPath:  localPath,
			Status:     "done",
		}
	}

	return artifacts, nil
}

func resToAspect(res string) string {
	switch res {
	case "1280x720", "1920x1080":
		return "16:9"
	case "720x1280", "1080x1920":
		return "9:16"
	default:
		return "16:9"
	}
}

func saveDataURL(path, dataURL string) error {
	idx := strings.Index(dataURL, "base64,")
	if idx == -1 {
		return fmt.Errorf("invalid data URL")
	}
	decoded, err := base64.StdEncoding.DecodeString(dataURL[idx+7:])
	if err != nil {
		return fmt.Errorf("decode base64: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, decoded, 0644)
}
