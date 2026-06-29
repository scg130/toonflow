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
	textModel   string
	imageModel  string
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
		textModel:   adapter.DefaultTextModel,
		imageModel:  adapter.DefaultImageModel,
	}
}

// Execute runs the generation pipeline for a task according to its mode.
func (p *Pipeline) Execute(ctx context.Context, t *task.Task) error {
	mode := t.Mode
	if mode == "" {
		mode = "full"
	}

	taskDir := filepath.Join(p.cfg.OutputDir, t.ID)
	if mode != "parse" {
		if err := os.MkdirAll(taskDir, 0755); err != nil {
			return fmt.Errorf("create task dir: %w", err)
		}
	}

	// Step 1: Parse script (full or parse mode)
	if mode == "full" || mode == "parse" {
		t.SetState(task.StateParsing, "parse_script")
		p.broadcast(t, "剧本解析中...", 10, nil)

		items, err := p.parseScript(ctx, t)
		if err != nil {
			return fmt.Errorf("parse script: %w", err)
		}
		t.Storyboard = items
		t.UpdateProgress(30)
		t.SetState(task.StateStoryboard, "gen_storyboard")
		p.broadcast(t, "分镜生成完成", 30, map[string]interface{}{
			"storyboard": items,
			"current_shot": 0,
			"total_shots": len(items),
		})

		if mode == "parse" {
			t.SetState(task.StateDone, "finish")
			t.UpdateProgress(100)
			p.broadcast(t, "分镜生成完成", 100, map[string]interface{}{"storyboard": items})
			return nil
		}
	}

	// Step 2: Generate images (full or images mode)
	if mode == "full" || mode == "images" {
		if len(t.Storyboard) == 0 {
			return fmt.Errorf("no storyboard to generate images")
		}

		t.SetState(task.StateDrawing, "gen_image")
		total := len(t.Storyboard)
		for i, item := range t.Storyboard {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			progress := 30 + float32(i+1)/float32(total)*50
			t.UpdateProgress(progress)
			p.broadcast(t, fmt.Sprintf("生成中 (%d/%d)", i+1, total), progress, map[string]interface{}{
				"current_shot": i + 1,
				"total_shots":  total,
			})

			localPath := filepath.Join(taskDir, fmt.Sprintf("shot_%03d.png", item.ShotNumber))
			if err := p.genImage(ctx, t, item, localPath); err != nil {
				return fmt.Errorf("shot %d: %w", item.ShotNumber, err)
			}

			imageURL := fmt.Sprintf("/output/%s/shot_%03d.png", t.ID, item.ShotNumber)
			t.Storyboard[i].ImageURL = imageURL

			t.Images = append(t.Images, task.ImageArtifact{
				ShotNumber: item.ShotNumber,
				LocalPath:  localPath,
				DataURL:    imageURL,
				Status:     "done",
			})

			p.broadcast(t, fmt.Sprintf("图片完成 (%d/%d)", i+1, total), progress, map[string]interface{}{
				"current_shot": i + 1,
				"total_shots":  total,
				"shot":         t.Storyboard[i],
			})
		}

		if mode == "images" {
			t.SetState(task.StateDone, "finish")
			t.UpdateProgress(100)
			p.broadcast(t, "图片生成完成", 100, map[string]interface{}{
				"storyboard": t.Storyboard,
			})
			return nil
		}
	}

	// Step 3: Merge video (full or video mode)
	if mode == "full" || mode == "video" {
		if len(t.Images) == 0 {
			if err := p.loadImagesFromStoryboard(t); err != nil {
				return err
			}
		}
		if len(t.Images) == 0 {
			return fmt.Errorf("no images to merge")
		}

		t.SetState(task.StateMerging, "merge_video")
		p.broadcast(t, "视频合成中...", 85, nil)

		outputPath := filepath.Join(taskDir, "output.mp4")
		if err := mergeVideo(t, outputPath); err != nil {
			return fmt.Errorf("merge video: %w", err)
		}

		t.VideoPath = outputPath
		videoURL := fmt.Sprintf("/output/%s/output.mp4", t.ID)
		t.SetState(task.StateDone, "finish")
		t.UpdateProgress(100)
		p.broadcast(t, "生成完成！", 100, map[string]interface{}{
			"video_url":  videoURL,
			"storyboard": t.Storyboard,
		})
	}

	return nil
}

func (p *Pipeline) loadImagesFromStoryboard(t *task.Task) error {
	for _, item := range t.Storyboard {
		if item.ImageURL == "" {
			continue
		}
		rel := strings.TrimPrefix(item.ImageURL, "/output/")
		localPath := filepath.Join(p.cfg.OutputDir, rel)
		if _, err := os.Stat(localPath); err != nil {
			continue
		}
		t.Images = append(t.Images, task.ImageArtifact{
			ShotNumber: item.ShotNumber,
			LocalPath:  localPath,
			DataURL:    item.ImageURL,
			Status:     "done",
		})
	}
	if len(t.Images) == 0 {
		return fmt.Errorf("no generated images found in storyboard")
	}
	return nil
}

func (p *Pipeline) broadcast(t *task.Task, msg string, progress float32, extra map[string]interface{}) {
	if p.broadcaster == nil {
		return
	}
	data := map[string]interface{}{
		"task_id": t.ID,
	}
	for k, v := range extra {
		data[k] = v
	}
	p.broadcaster.Broadcast(ws.WSResponse{
		Code:     0,
		Msg:      msg,
		Step:     t.Step,
		Progress: progress,
		Data:     ws.MustMarshalJSON(data),
	})
}

func (p *Pipeline) parseScript(ctx context.Context, t *task.Task) ([]task.StoryboardItem, error) {
	systemPrompt := "You are a professional short drama storyboard artist. Parse the script into numbered shots.\n\n"
	systemPrompt += p.skillMgr.Get("art_skills") + "\n"
	systemPrompt += p.skillMgr.Get("production_execution") + "\n"
	systemPrompt += p.skillMgr.Get("story_skills") + "\n"
	if t.Style != "" {
		systemPrompt += fmt.Sprintf("\nArt style: %s. Maintain consistency.\n", t.Style)
	}

	resp, err := p.adapter.TextRequest(ctx, p.textModel, adapter.TextParams{
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
	prompt := item.Prompt
	if prompt == "" {
		prompt = item.Description
	}
	if t.Style != "" {
		prompt += ", " + t.Style + " art style"
	}
	if item.Camera != "" {
		prompt += ", camera: " + item.Camera
	}

	resp, err := p.adapter.ImageRequest(ctx, p.imageModel, adapter.ImageParams{
		Prompt:      prompt,
		Model:       p.imageModel,
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
			num++
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
