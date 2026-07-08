package engine

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"toonflow/adapter"
	"toonflow/logger"
	"toonflow/service"
	"toonflow/service/asset"
	"toonflow/service/storyboard"
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
	db          *sql.DB
	textModel   string
	imageModel  string
}

// Config holds pipeline configuration.
type Config struct {
	OutputDir   string
	TaskTimeout time.Duration
}

// New creates a new Pipeline.
func New(v adapter.Vendor, skillMgr *skill.Manager, cfg *Config, bc *ws.ConnManager, db *sql.DB) *Pipeline {
	return &Pipeline{
		adapter:     v,
		skillMgr:    skillMgr,
		cfg:         cfg,
		broadcaster: bc,
		db:          db,
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
	logger.CtxTrace(ctx, "pipeline start task=%s mode=%s project=%s episode=%s storyboard=%d",
		t.ID, mode, t.ProjectID, t.EpisodeID, len(t.Storyboard))

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
			"storyboard":   items,
			"current_shot": 0,
			"total_shots":  len(items),
		})

		if mode == "parse" {
			t.SetState(task.StateDone, "finish")
			t.UpdateProgress(100)
			p.broadcast(t, "分镜生成完成", 100, map[string]interface{}{"storyboard": items})
			return nil
		}
	}

	// Step 1.5: Ensure assets exist before image generation (full mode auto-extracts)
	if mode == "full" {
		if err := p.ensureProjectAssets(ctx, t); err != nil {
			return err
		}
	}
	if mode == "full" || mode == "images" {
		if err := p.requireProjectAssets(t); err != nil {
			return err
		}
	}

	// Step 2: Generate images (full or images mode)
	if mode == "full" || mode == "images" {
		if len(t.Storyboard) == 0 {
			return fmt.Errorf("no storyboard to generate images")
		}

		t.SetState(task.StateDrawing, "gen_image")
		selected := shotFilterSet(t.GenerateShots)
		indices := storyboardIndicesToGenerate(t.Storyboard, selected)
		sort.Slice(indices, func(i, j int) bool {
			return t.Storyboard[indices[i]].ShotNumber < t.Storyboard[indices[j]].ShotNumber
		})
		if len(indices) == 0 {
			return fmt.Errorf("no shots selected for image generation")
		}
		total := len(indices)
		liveStatus := service.PipelineStatusFromContext(ctx)
		for seq, idx := range indices {
			item := t.Storyboard[idx]
			if err := service.WaitIfPaused(ctx); err != nil {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			liveStatus.SetShot(item.ShotNumber, seq+1, total)

			if p.db != nil && t.ProjectID != "" && t.EpisodeID != "" {
				service.MergeShotMediaFromStore(p.db, t.ProjectID, t.EpisodeID, item.ShotNumber, &t.Storyboard[idx])
				item = t.Storyboard[idx]
			}
			if t.SkipExistingImages && service.ShotHasImage(item) {
				progress := 30 + float32(seq+1)/float32(total)*50
				localPct := float32(seq+1) / float32(total) * 100
				skipMsg := fmt.Sprintf("第 %d 镜关键帧已就绪，跳过 (%d/%d)", item.ShotNumber, seq+1, total)
				service.ReportStepProgress(ctx, localPct, skipMsg)
				t.UpdateProgress(progress)
				p.broadcast(t, skipMsg, progress, map[string]interface{}{
					"current_shot": seq + 1,
					"total_shots":  total,
					"shot":         t.Storyboard[idx],
					"skipped":      true,
				})
				continue
			}

			progress := 30 + float32(seq+1)/float32(total)*50
			localPct := float32(seq) / float32(total) * 100
			service.ReportStepProgress(ctx, localPct,
				fmt.Sprintf("正在生成第 %d 镜关键帧图片 (%d/%d)", item.ShotNumber, seq+1, total))
			t.UpdateProgress(progress)
			p.broadcast(t, fmt.Sprintf("关键帧生图中 (%d/%d)", seq+1, total), progress, map[string]interface{}{
				"current_shot": seq + 1,
				"total_shots":  total,
			})

			beats, err := p.genShotKeyframes(ctx, t, item, taskDir, func(beatIdx int, partial []task.ShotBeat) {
				t.Storyboard[idx].Beats = partial
				if len(partial) > 0 {
					t.Storyboard[idx].ImageURL = partial[0].ImageURL
					t.Storyboard[idx].ImageRemoteURL = partial[0].ImageRemoteURL
				}
				beatProgress := progress + float32(beatIdx+1)/float32(len(partial)+1)*2
				p.broadcast(t, fmt.Sprintf("第 %d 镜关键帧 %d/%d", item.ShotNumber, beatIdx+1, len(item.Beats)), beatProgress, map[string]interface{}{
					"current_shot": seq + 1,
					"total_shots":  total,
					"current_beat": beatIdx + 1,
					"total_beats":  len(item.Beats),
					"shot":         t.Storyboard[idx],
				})
			})
			if err != nil {
				return fmt.Errorf("shot %d: %w", item.ShotNumber, err)
			}
			t.Storyboard[idx].Beats = beats
			if len(beats) > 0 {
				t.Storyboard[idx].ImageURL = beats[0].ImageURL
				t.Storyboard[idx].ImageRemoteURL = beats[0].ImageRemoteURL
			}
			if p.db != nil && t.ProjectID != "" {
				_, _, assetIDs := service.ShotImageParams(p.db, t.ProjectID, item)
				if len(assetIDs) > 0 {
					t.Storyboard[idx].AssetIDs = assetIDs
				}
			}

			absLocalPath := ""
			if len(beats) > 0 && beats[0].ImageURL != "" {
				if lp, ok := resolveOutputFilePath(p.cfg.OutputDir, beats[0].ImageURL); ok {
					absLocalPath = lp
				}
			}

			t.Images = append(t.Images, task.ImageArtifact{
				ShotNumber: item.ShotNumber,
				LocalPath:  absLocalPath,
				DataURL:    t.Storyboard[idx].ImageURL,
				Status:     "done",
			})

			p.broadcast(t, fmt.Sprintf("关键帧完成 (%d/%d)", seq+1, total), progress, map[string]interface{}{
				"current_shot": seq + 1,
				"total_shots":  total,
				"shot":         t.Storyboard[idx],
			})
			if p.db != nil && t.ProjectID != "" && t.EpisodeID != "" {
				_ = service.UpdateStoryboardShotKeyframes(p.db, t.ProjectID, t.EpisodeID, item.ShotNumber, beats)
			}
			donePct := float32(seq+1) / float32(total) * 100
			service.ReportStepProgress(ctx, donePct,
				fmt.Sprintf("第 %d 镜关键帧完成 (%d/%d)", item.ShotNumber, seq+1, total))
		}

		if mode == "images" {
			t.SetState(task.StateDone, "finish")
			t.UpdateProgress(100)
			p.broadcast(t, "关键帧生成完成", 100, map[string]interface{}{
				"storyboard": t.Storyboard,
			})
			logger.CtxTrace(ctx, "pipeline done task=%s mode=%s", t.ID, mode)
			return nil
		}
	}

	// Step 3: Merge video (full or video mode)
	if mode == "full" || mode == "video" {
		if err := p.requireStoryboardImages(t); err != nil {
			return err
		}
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
		localPath, ok := resolveOutputFilePath(p.cfg.OutputDir, item.ImageURL)
		if !ok {
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
	sortImagesByShot(t.Images)
	return nil
}

func (p *Pipeline) broadcast(t *task.Task, msg string, progress float32, extra map[string]interface{}) {
	if p.broadcaster == nil {
		return
	}
	data := map[string]interface{}{
		"task_id":     t.ID,
		"task_update": true,
		"title":       t.Title,
		"state":       t.State,
		"mode":        t.Mode,
		"project_id":  t.ProjectID,
		"episode_id":  t.EpisodeID,
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

func (p *Pipeline) genShotKeyframes(ctx context.Context, t *task.Task, item task.StoryboardItem, taskDir string, onBeat func(beatIdx int, beats []task.ShotBeat)) ([]task.ShotBeat, error) {
	item.Beats = storyboard.CapShotBeats(item.Beats, item.Duration, item.Description)
	if len(item.Beats) < 2 {
		return nil, fmt.Errorf("第 %d 镜缺少 beats 时间节点，请重新生成分镜", item.ShotNumber)
	}
	var artStyle, videoRatio string
	if p.db != nil && t.ProjectID != "" {
		_ = p.db.QueryRow("SELECT art_style, video_ratio FROM o_project WHERE id = ?", t.ProjectID).Scan(&artStyle, &videoRatio)
	}
	item = service.SanitizeStoryboardItemForImage(p.db, t.ProjectID, item)
	refURL, assetPrompt, _ := service.ShotImageParams(p.db, t.ProjectID, item)
	assets, _ := asset.LoadProjectAssets(p.db, t.ProjectID)
	styleAnchor := service.LoadProjectStyleAnchor(p.db, t.ProjectID)
	aspect := service.ResolutionToVideoRatio(t.Resolution)

	beats := make([]task.ShotBeat, len(item.Beats))
	copy(beats, item.Beats)
	for i := range beats {
		if beats[i].ImageURL != "" || beats[i].ImageRemoteURL != "" {
			continue
		}
		prompt := service.BuildBeatImagePrompt(item, beats[i], t.Style, videoRatio, assetPrompt, styleAnchor)
		if len(assets) > 0 {
			prompt = asset.SanitizeFinalImagePrompt(prompt, item, assets)
		}
		logger.CtxTrace(ctx, "genKeyframe shot=%d beat=%d prompt=%s", item.ShotNumber, i, prompt)
		resp, err := service.RequestShotImageWithRetry(ctx, p.adapter, p.imageModel, aspect, prompt, refURL)
		if err != nil {
			if service.IsContentPolicyViolation(err) {
				return nil, fmt.Errorf("%s: %w", service.UserFacingImagePolicyMessage(item.ShotNumber), err)
			}
			return nil, err
		}
		localPath := filepath.Join(taskDir, fmt.Sprintf("shot_%03d_k%d.png", item.ShotNumber, i))
		if err := saveGeneratedImage(ctx, localPath, resp); err != nil {
			return nil, err
		}
		beats[i].ImageURL = fmt.Sprintf("/output/%s/shot_%03d_k%d.png", t.ID, item.ShotNumber, i)
		beats[i].ImageRemoteURL = publishRemoteFromImage(ctx, p.adapter, localPath, resp)
		if onBeat != nil {
			onBeat(i, beats)
		}
	}
	return beats, nil
}

func publishRemoteFromImage(ctx context.Context, v adapter.Vendor, localPath string, resp *adapter.ImageResponse) string {
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

func (p *Pipeline) genImage(ctx context.Context, t *task.Task, item task.StoryboardItem, localPath string) (string, error) {
	var refURL, assetPrompt string
	var assets []asset.ProjectAsset
	if p.db != nil && t.ProjectID != "" {
		item = service.SanitizeStoryboardItemForImage(p.db, t.ProjectID, item)
		assets, _ = asset.LoadProjectAssets(p.db, t.ProjectID)
		refURL, assetPrompt, _ = service.ShotImageParams(p.db, t.ProjectID, item)
	}
	prompt := service.BuildShotImagePrompt(item, t.Style, service.ResolutionToVideoRatio(t.Resolution), assetPrompt,
		service.LoadProjectStyleAnchor(p.db, t.ProjectID))
	if len(assets) > 0 {
		prompt = asset.SanitizeFinalImagePrompt(prompt, item, assets)
	}
	logger.CtxTrace(ctx, "genImage shot=%d prompt=%s", item.ShotNumber, prompt)

	resp, err := service.RequestShotImageWithRetry(ctx, p.adapter, p.imageModel, resToAspect(t.Resolution), prompt, refURL)
	if err != nil {
		if service.IsContentPolicyViolation(err) {
			logger.CtxError(ctx, err, "genImage shot=%d policy blocked after retries", item.ShotNumber)
			return "", fmt.Errorf("%s: %w", service.UserFacingImagePolicyMessage(item.ShotNumber), err)
		}
		logger.CtxError(ctx, err, "genImage shot=%d failed", item.ShotNumber)
		return "", err
	}
	logger.CtxTrace(ctx, "genImage shot=%d adapter resp model=%s data_url=%q remote_url=%q",
		item.ShotNumber, resp.Model, summarizeDataURL(resp.DataURL), resp.RemoteURL)
	if err := saveGeneratedImage(ctx, localPath, resp); err != nil {
		return "", err
	}
	remoteURL := ""
	if adapter.IsCDNImageURL(resp.RemoteURL) {
		remoteURL = resp.RemoteURL
	} else if adapter.IsCDNImageURL(resp.DataURL) {
		remoteURL = resp.DataURL
	}
	if remoteURL == "" {
		if pub, ok := p.adapter.(adapter.ImageCDNPublisher); ok {
			if u, pubErr := pub.PublishImageForVideo(ctx, localPath); pubErr == nil && adapter.IsCDNImageURL(u) {
				remoteURL = u
				logger.CtxTrace(ctx, "genImage shot=%d published remote_url=%s", item.ShotNumber, remoteURL)
			}
		}
	}
	if remoteURL != "" {
		logger.CtxTrace(ctx, "genImage shot=%d saved local=%s image_remote_url=%s", item.ShotNumber, localPath, remoteURL)
	} else {
		logger.CtxTrace(ctx, "genImage shot=%d saved local=%s (no image_remote_url, base64 only)", item.ShotNumber, localPath)
	}
	return remoteURL, nil
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

	sortImagesByShot(t.Images)

	for _, img := range t.Images {
		count := int(t.FrameDuration * float64(t.FPS))
		if count < 1 {
			count = 1
		}
		filePath := ffmpegConcatPath(img.LocalPath)
		for i := 0; i < count; i++ {
			fmt.Fprintf(f, "file '%s'\n", filePath)
		}
	}

	absOutput, err := filepath.Abs(outputPath)
	if err != nil {
		absOutput = outputPath
	}
	absList, err := filepath.Abs(listPath)
	if err != nil {
		absList = listPath
	}

	cmd := exec.Command("ffmpeg", "-y", "-f", "concat", "-safe", "0",
		"-i", absList, "-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-r", strconv.Itoa(t.FPS), absOutput)
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

func saveGeneratedImage(ctx context.Context, path string, resp *adapter.ImageResponse) error {
	if resp == nil {
		return fmt.Errorf("empty image response")
	}
	// Prefer inline base64 — avoids CDN download timeouts (platform-outputs.agnes-ai.space).
	if resp.DataURL != "" && !strings.HasPrefix(resp.DataURL, "http://") && !strings.HasPrefix(resp.DataURL, "https://") {
		return saveDataURL(path, resp.DataURL)
	}
	if strings.HasPrefix(resp.DataURL, "http://") || strings.HasPrefix(resp.DataURL, "https://") {
		return adapter.DownloadHTTPURL(ctx, path, resp.DataURL)
	}
	return fmt.Errorf("no image data in response")
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

func shotFilterSet(nums []int) map[int]bool {
	if len(nums) == 0 {
		return nil
	}
	set := make(map[int]bool, len(nums))
	for _, n := range nums {
		set[n] = true
	}
	return set
}

func storyboardIndicesToGenerate(items []task.StoryboardItem, selected map[int]bool) []int {
	var indices []int
	for i, item := range items {
		if selected == nil || selected[item.ShotNumber] {
			indices = append(indices, i)
		}
	}
	return indices
}

func resolveOutputFilePath(outputDir, imageURL string) (string, bool) {
	rel := strings.TrimPrefix(imageURL, "/output/")
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || strings.Contains(rel, "..") {
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

func ffmpegConcatPath(path string) string {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	return strings.ReplaceAll(absPath, "'", `'\''`)
}

func sortImagesByShot(images []task.ImageArtifact) {
	sort.Slice(images, func(i, j int) bool {
		return images[i].ShotNumber < images[j].ShotNumber
	})
}

func summarizeDataURL(s string) string {
	if strings.HasPrefix(s, "data:") {
		return fmt.Sprintf("<data-url len=%d>", len(s))
	}
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

func (p *Pipeline) requireProjectAssets(t *task.Task) error {
	if p.db == nil || t.ProjectID == "" {
		return fmt.Errorf("请先从剧本提取资产后再生成图片")
	}
	return service.RequireProjectAssets(p.db, t.ProjectID)
}

func (p *Pipeline) ensureProjectAssets(ctx context.Context, t *task.Task) error {
	if p.db == nil || t.ProjectID == "" {
		return fmt.Errorf("请先从剧本提取资产后再生成图片")
	}
	n, err := service.CountProjectAssets(p.db, t.ProjectID)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if t.EpisodeID == "" {
		return fmt.Errorf("请先从剧本提取资产后再生成图片")
	}

	t.SetState(task.StateStoryboard, "extract_assets")
	p.broadcast(t, "提取资产中...", 32, nil)
	_, err = service.ExtractAssetsFromEpisode(ctx, p.db, p.adapter, t.UserID, t.ProjectID, t.EpisodeID)
	if err != nil {
		return fmt.Errorf("提取资产: %w", err)
	}
	n, err = service.CountProjectAssets(p.db, t.ProjectID)
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("请先从剧本提取资产后再生成图片")
	}
	p.broadcast(t, "资产提取完成", 35, nil)
	return nil
}

func (p *Pipeline) requireStoryboardImages(t *task.Task) error {
	if len(t.Storyboard) == 0 {
		return fmt.Errorf("请先生成分镜图片")
	}
	for _, item := range t.Storyboard {
		if item.ImageURL != "" {
			continue
		}
		return fmt.Errorf("请先生成第 %d 镜图片后再生成视频", item.ShotNumber)
	}
	return nil
}
