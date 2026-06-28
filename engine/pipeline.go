package engine

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"toonflow/adapter"
	"toonflow/skill"
	"toonflow/task"
	"toonflow/ws"
)

// Pipeline orchestrates the full generation pipeline.
type Pipeline struct {
	adapter     adapter.Vendor
	skillMgr    *skill.Manager
	cfg         *Config
	broadcaster *ws.ConnManager
}

// Config holds pipeline configuration.
type Config struct {
	OutputDir   string
	TaskTimeout time.Duration
}

// New creates a new Pipeline.
func New(v adapter.Vendor, skillMgr *skill.Manager, cfg *Config, bc *ws.ConnManager) *Pipeline {
	return &Pipeline{
		adapter:     v,
		skillMgr:    skillMgr,
		cfg:         cfg,
		broadcaster: bc,
	}
}

// Execute runs the full generation pipeline for a task.
func (p *Pipeline) Execute(ctx context.Context, t *task.Task) error {
	taskDir := filepath.Join(p.cfg.OutputDir, t.ID)
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		return fmt.Errorf("create task dir: %w", err)
	}

	// Step 1: Parse script
	t.SetState(task.StateParsing, "parse_script")
	p.broadcast(t, "剧本解析中...", 10)

	items, err := p.parseScript(ctx, t)
	if err != nil {
		return fmt.Errorf("parse script: %w", err)
	}
	t.Storyboard = items
	t.UpdateProgress(30)
	p.broadcast(t, "分镜生成完成", 30)

	// Step 2: Generate images
	t.SetState(task.StateDrawing, "gen_image")
	for i, item := range items {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		progress := 30 + float32(i+1)/float32(len(items))*50
		t.UpdateProgress(progress)
		p.broadcast(t, fmt.Sprintf("生成中 (%d/%d)", i+1, len(items)), progress)

		localPath := filepath.Join(taskDir, fmt.Sprintf("shot_%03d.png", item.ShotNumber))
		if err := p.genImage(ctx, t, item, localPath); err != nil {
			return fmt.Errorf("shot %d: %w", item.ShotNumber, err)
		}

		t.Images = append(t.Images, task.ImageArtifact{
			ShotNumber: item.ShotNumber,
			LocalPath:  localPath,
			Status:     "done",
		})
	}

	// Step 3: Merge video
	t.SetState(task.StateMerging, "merge_video")
	p.broadcast(t, "视频合成中...", 85)

	outputPath := filepath.Join(taskDir, "output.mp4")
	if err := mergeVideo(t, outputPath); err != nil {
		return fmt.Errorf("merge video: %w", err)
	}

	t.VideoPath = outputPath
	t.SetState(task.StateDone, "finish")
	t.UpdateProgress(100)
	p.broadcast(t, "生成完成！", 100)

	return nil
}

func (p *Pipeline) broadcast(t *task.Task, msg string, progress float32) {
	if p.broadcaster != nil {
		p.broadcaster.Broadcast(ws.WSResponse{
			Code:     0,
			Msg:      msg,
			Step:     t.Step,
			Progress: progress,
			Data:     ws.MustMarshalJSON(map[string]interface{}{
				"task_id": t.ID,
			}),
		})
	}
}

func (p *Pipeline) parseScript(ctx context.Context, t *task.Task) ([]task.StoryboardItem, error) {
	systemPrompt := "You are a professional short drama storyboard artist. Parse the script into numbered shots.\n\n"
	systemPrompt += p.skillMgr.Get("art_skills") + "\n"
	systemPrompt += p.skillMgr.Get("production_execution") + "\n"
	systemPrompt += p.skillMgr.Get("story_skills") + "\n"
	if t.Style != "" {
		systemPrompt += fmt.Sprintf("\nArt style: %s. Maintain consistency.\n", t.Style)
	}

	resp, err := p.adapter.TextRequest(ctx, "gpt-4o", adapter.TextParams{
		Messages: []adapter.TextMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: t.Script},
		},
		Temperature: 0.7,
		MaxTokens:   8000,
	})
	if err != nil {
		return nil, err
	}

	return parseStoryboardText(resp.Content, t.Resolution), nil
}

func (p *Pipeline) genImage(ctx context.Context, t *task.Task, item task.StoryboardItem, localPath string) error {
	prompt := item.Description
	if t.Style != "" {
		prompt += ", " + t.Style + " art style"
	}
	if item.Camera != "" {
		prompt += ", camera: " + item.Camera
	}

	resp, err := p.adapter.ImageRequest(ctx, "dall-e-3", adapter.ImageParams{
		Prompt:      prompt,
		Model:       "dall-e-3",
		AspectRatio: resToAspect(t.Resolution),
	})
	if err != nil {
		return err
	}
	return saveDataURL(localPath, resp.DataURL)
}

func parseStoryboardText(text, resolution string) []task.StoryboardItem {
	var items []task.StoryboardItem
	lines := strings.Split(text, "\n")
	var cur *task.StoryboardItem
	num := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "镜头") || strings.HasPrefix(line, "Shot ") {
			if cur != nil {
				items = append(items, *cur)
			}
			num++
			cur = &task.StoryboardItem{ShotNumber: num, Duration: 3.0}
			continue
		}

		if cur == nil {
			cur = &task.StoryboardItem{ShotNumber: num, Description: line, Duration: 3.0}
			continue
		}

		if idx := strings.IndexAny(line, ":："); idx != -1 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			switch key {
			case "场景", "Scene":
				cur.Scene = val
			case "描述", "Description":
				cur.Description = val
			case "镜头", "Camera":
				cur.Camera = val
			case "时长", "Duration":
				fmt.Sscanf(val, "%f", &cur.Duration)
			case "Prompt":
				cur.Prompt = val
			}
		} else {
			cur.Description += " " + line
		}
	}
	if cur != nil {
		items = append(items, *cur)
	}
	if len(items) == 0 {
		items = append(items, task.StoryboardItem{ShotNumber: 1, Description: text, Duration: 3.0})
	}
	return items
}

func mergeVideo(t *task.Task, outputPath string) error {
	if len(t.Images) == 0 {
		return fmt.Errorf("no images to merge")
	}

	listPath := outputPath + "_list.txt"
	f, err := os.Create(listPath)
	if err != nil {
		return err
	}
	defer os.Remove(listPath)
	defer f.Close()

	for _, img := range t.Images {
		count := int(t.FrameDuration * float64(t.FPS))
		if count < 1 {
			count = 1
		}
		for i := 0; i < count; i++ {
			fmt.Fprintf(f, "file '%s'\n", img.LocalPath)
		}
	}

	cmd := exec.Command("ffmpeg", "-y", "-f", "concat", "-safe", "0",
		"-i", listPath, "-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-r", strconv.Itoa(t.FPS), outputPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %s", string(out))
	}
	return nil
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
