package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"toonflow/adapter"
	"toonflow/skill"
	"toonflow/task"
)

// EpisodePipelineStep describes one automation step.
type EpisodePipelineStep struct {
	ID    string
	Label string
}

var episodePipelineOrder = []EpisodePipelineStep{
	{ID: "generate_skeleton", Label: "故事骨架"},
	{ID: "generate_strategy", Label: "改编策略"},
	{ID: "generate_script", Label: "剧本"},
	{ID: "generate_storyboard", Label: "分镜"},
	{ID: "extract_assets", Label: "提取资产"},
	{ID: "batch_generate_shot_images", Label: "批量生图"},
	{ID: "batch_generate_shot_videos", Label: "批量生视频"},
}

// EpisodePipelineDeps holds services needed to run the full episode flow.
type EpisodePipelineDeps struct {
	DB         *sql.DB
	Vendor     adapter.Vendor
	SkillMgr   *skill.Manager
	Queue      *task.Queue
	Pipeline   interface{ Execute(context.Context, *task.Task) error }
	OutputDir  string
	TaskTimeout time.Duration
}

// EpisodePipelineResult summarizes a completed run.
type EpisodePipelineResult struct {
	StepsRun   []string `json:"steps_run"`
	StepsSkip  []string `json:"steps_skipped"`
	ShotImages int      `json:"shot_images,omitempty"`
	ShotVideos int      `json:"shot_videos,omitempty"`
}

// PlanEpisodePipeline returns steps still needed for an episode.
func PlanEpisodePipeline(db *sql.DB, projectID, episodeID string) ([]EpisodePipelineStep, []EpisodePipelineStep, error) {
	if db == nil || projectID == "" || episodeID == "" {
		return nil, nil, fmt.Errorf("project_id and episode_id required")
	}
	var pending, skipped []EpisodePipelineStep
	for _, step := range episodePipelineOrder {
		done, err := episodeStepDone(db, projectID, episodeID, step.ID)
		if err != nil {
			return nil, nil, err
		}
		if done {
			skipped = append(skipped, step)
		} else {
			pending = append(pending, step)
		}
	}
	return pending, skipped, nil
}

func episodeStepDone(db *sql.DB, projectID, episodeID, stepID string) (bool, error) {
	switch stepID {
	case "generate_skeleton":
		return hasAgentWork(db, projectID, episodeID, "skeleton"), nil
	case "generate_strategy":
		return hasAgentWork(db, projectID, episodeID, "strategy"), nil
	case "generate_script":
		return hasEpisodeScript(db, episodeID), nil
	case "generate_storyboard":
		items, err := LoadStoryboardItems(db, projectID, episodeID)
		return err == nil && len(items) > 0, err
	case "extract_assets":
		n, err := CountProjectAssets(db, projectID)
		return n > 0, err
	case "batch_generate_shot_images":
		need, err := shotsNeedingImages(db, projectID, episodeID)
		return len(need) == 0, err
	case "batch_generate_shot_videos":
		need, err := shotsNeedingVideos(db, projectID, episodeID)
		return len(need) == 0, err
	default:
		return true, nil
	}
}

func hasAgentWork(db *sql.DB, projectID, episodeID, workType string) bool {
	var content string
	err := db.QueryRow(`
		SELECT content FROM o_agent_work
		WHERE project_id = ? AND episode_id = ? AND work_type = ?`,
		projectID, episodeID, workType).Scan(&content)
	return err == nil && strings.TrimSpace(content) != ""
}

func hasEpisodeScript(db *sql.DB, episodeID string) bool {
	var script string
	err := db.QueryRow(`SELECT COALESCE(script_content,'') FROM o_episode WHERE id = ?`, episodeID).Scan(&script)
	return err == nil && strings.TrimSpace(script) != ""
}

func shotsNeedingImages(db *sql.DB, projectID, episodeID string) ([]int, error) {
	items, err := LoadStoryboardItems(db, projectID, episodeID)
	if err != nil {
		return nil, err
	}
	var need []int
	for _, it := range items {
		if strings.TrimSpace(it.ImageURL) == "" {
			need = append(need, it.ShotNumber)
		}
	}
	return SortShotNumbers(need), nil
}

func shotsNeedingVideos(db *sql.DB, projectID, episodeID string) ([]int, error) {
	items, err := LoadStoryboardItems(db, projectID, episodeID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	rows, err := db.Query(`
		SELECT DISTINCT shot_number FROM o_shot_clip
		WHERE project_id = ? AND episode_id = ? AND is_selected = 1`,
		projectID, episodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hasClip := map[int]bool{}
	for rows.Next() {
		var n int
		if rows.Scan(&n) == nil {
			hasClip[n] = true
		}
	}
	var need []int
	for _, it := range items {
		if !hasClip[it.ShotNumber] {
			need = append(need, it.ShotNumber)
		}
	}
	return SortShotNumbers(need), nil
}

// RunEpisodePipeline executes all pending steps for one episode sequentially.
func RunEpisodePipeline(ctx context.Context, deps EpisodePipelineDeps, userID, projectID, episodeID string) (*EpisodePipelineResult, error) {
	if deps.DB == nil || deps.Vendor == nil {
		return nil, fmt.Errorf("pipeline dependencies unavailable")
	}
	if episodeID == "" {
		return nil, fmt.Errorf("请先选择一集")
	}

	pending, skipped, err := PlanEpisodePipeline(deps.DB, projectID, episodeID)
	if err != nil {
		return nil, err
	}
	if len(pending) == 0 {
		return &EpisodePipelineResult{StepsSkip: stepIDs(skipped)}, nil
	}

	result := &EpisodePipelineResult{
		StepsSkip: stepIDs(skipped),
	}
	agent := &AgentChat{DB: deps.DB, Vendor: deps.Vendor, SkillMgr: deps.SkillMgr}
	total := len(pending)

	for i, step := range pending {
		if err := WaitIfPaused(ctx); err != nil {
			return result, err
		}

		basePct := float32(i) / float32(total) * 100
		stepPct := 100 / float32(total)
		ReportProgress(ctx, step.ID, basePct+stepPct*0.1,
			fmt.Sprintf("[%d/%d] %s — 准备中...", i+1, total, step.Label))

		var runErr error
		switch step.ID {
		case "generate_skeleton", "generate_strategy", "generate_script", "generate_storyboard", "extract_assets":
			intent := &ChatActionIntent{Type: step.ID}
			ReportProgress(ctx, step.ID, basePct+stepPct*0.3,
				fmt.Sprintf("[%d/%d] 正在%s...", i+1, total, step.Label))
			_, _, runErr = agent.executeAction(ctx, userID, projectID, episodeID, "general", intent, step.Label)
		case "batch_generate_shot_images":
			shots, err := shotsNeedingImages(deps.DB, projectID, episodeID)
			if err != nil {
				return result, err
			}
			if len(shots) == 0 {
				continue
			}
			ReportProgress(ctx, step.ID, basePct+stepPct*0.2,
				fmt.Sprintf("[%d/%d] 批量生图（%d 镜）...", i+1, total, len(shots)))
			stepCtx := WithStepProgress(ctx, step.ID, basePct+stepPct*0.2, stepPct*0.7)
			runErr = runEpisodeBatchImages(stepCtx, deps, userID, projectID, episodeID, shots)
			if runErr == nil {
				result.ShotImages = len(shots)
			}
		case "batch_generate_shot_videos":
			shots, err := shotsNeedingVideos(deps.DB, projectID, episodeID)
			if err != nil {
				return result, err
			}
			if len(shots) == 0 {
				continue
			}
			ReportProgress(ctx, step.ID, basePct+stepPct*0.2,
				fmt.Sprintf("[%d/%d] 串行生视频（%d 镜）...", i+1, total, len(shots)))
			stepCtx := WithStepProgress(ctx, step.ID, basePct+stepPct*0.2, stepPct*0.7)
			var outcome *BatchVideoOutcome
			outcome, runErr = GenerateShotClipsSequential(stepCtx, deps.DB, deps.Vendor, deps.OutputDir, projectID, episodeID, shots)
			if outcome != nil {
				result.ShotVideos = len(outcome.Clips)
			}
		default:
			runErr = fmt.Errorf("unknown step: %s", step.ID)
		}

		if runErr != nil {
			return result, fmt.Errorf("%s失败: %w", step.Label, runErr)
		}
		result.StepsRun = append(result.StepsRun, step.ID)
		ReportProgress(ctx, step.ID, basePct+stepPct,
			fmt.Sprintf("[%d/%d] %s 完成", i+1, total, step.Label))
	}

	ReportProgress(ctx, "episode_pipeline", 100, "分集流水线全部完成")
	return result, nil
}

func runEpisodeBatchImages(ctx context.Context, deps EpisodePipelineDeps, userID, projectID, episodeID string, shots []int) error {
	if deps.Queue == nil || deps.Pipeline == nil {
		return fmt.Errorf("生成服务不可用")
	}
	if err := RequireProjectAssets(deps.DB, projectID); err != nil {
		return err
	}
	items, err := LoadStoryboardItems(deps.DB, projectID, episodeID)
	if err != nil || len(items) == 0 {
		return fmt.Errorf("请先生成分镜")
	}

	var artStyle, videoRatio string
	_ = deps.DB.QueryRow("SELECT art_style, video_ratio FROM o_project WHERE id = ?", projectID).Scan(&artStyle, &videoRatio)
	resolution := "1280x720"
	if videoRatio == "9:16" {
		resolution = "720x1280"
	}

	timeout := deps.TaskTimeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}

	id := fmt.Sprintf("task_%d", time.Now().UnixNano())
	tk := task.NewTask(id, projectID, "", artStyle, 3, resolution, 24, timeout)
	tk.UserID = userID
	tk.Mode = "images"
	tk.EpisodeID = episodeID
	tk.GenerateShots = shots
	tk.Storyboard = items
	EnrichTaskMeta(deps.DB, tk)

	done := make(chan error, 1)
	deps.Queue.Submit(tk, func(runCtx context.Context, t *task.Task) error {
		runCtx = InheritPipelineContext(ctx, runCtx)
		err := deps.Pipeline.Execute(runCtx, t)
		if err == nil && t.ProjectID != "" && len(t.Storyboard) > 0 {
			_ = SaveStoryboardItems(deps.DB, t.ProjectID, t.EpisodeID, t.Storyboard)
		}
		done <- err
		return err
	})

	waitTimeout := timeout + 2*time.Minute
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		tk.Cancel()
		return ctx.Err()
	case <-time.After(waitTimeout):
		tk.Cancel()
		return fmt.Errorf("批量生图超时")
	}
}

func stepIDs(steps []EpisodePipelineStep) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.ID
	}
	return out
}

// EpisodeStepIDs exports step id list for API responses.
func EpisodeStepIDs(steps []EpisodePipelineStep) []string {
	return stepIDs(steps)
}

// EpisodePipelineTimeout estimates a safe upper bound for a full episode pipeline run.
func EpisodePipelineTimeout(pendingSteps int) time.Duration {
	if pendingSteps <= 0 {
		pendingSteps = 1
	}
	d := time.Duration(pendingSteps) * 45 * time.Minute
	if d < 2*time.Hour {
		d = 2 * time.Hour
	}
	const max = 12 * time.Hour
	if d > max {
		d = max
	}
	return d
}

// EpisodeStepLabel returns a human label for a step id.
func EpisodeStepLabel(stepID string) string {
	for _, s := range episodePipelineOrder {
		if s.ID == stepID {
			return s.Label
		}
	}
	return stepID
}
