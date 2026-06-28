package service

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"toonflow/task"
)

// MergeVideo uses FFmpeg to concatenate images into a video.
func MergeVideo(images []task.ImageArtifact, frameDuration float64, resolution string, fps int, outputPath string) error {
	if len(images) == 0 {
		return fmt.Errorf("no images to merge")
	}

	listPath := outputPath + "_list.txt"
	f, err := os.Create(listPath)
	if err != nil {
		return err
	}
	defer os.Remove(listPath)
	defer f.Close()

	for _, img := range images {
		count := int(frameDuration * float64(fps))
		if count < 1 {
			count = 1
		}
		for i := 0; i < count; i++ {
			fmt.Fprintf(f, "file '%s'\n", img.LocalPath)
		}
	}

	cmd := exec.Command(
		"ffmpeg", "-y",
		"-f", "concat", "-safe", "0",
		"-i", listPath,
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-r", strconv.Itoa(fps),
		outputPath,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg failed: %s: %w", stderr.String(), err)
	}

	return nil
}

// CleanTemp removes temporary files in a task directory.
func CleanTemp(dir string) {
	if dir != "" {
		os.RemoveAll(dir)
	}
}
