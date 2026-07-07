package pipeline

import (
	svcagent "toonflow/service/agent"
	"toonflow/service/asset"
	"toonflow/service/core"
	"toonflow/service/media"
	"toonflow/service/storyboard"
	"toonflow/service/voice"
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
	Panel string // workbench panel hint for UI
}

var episodePipelineOrder = []EpisodePipelineStep{
	{ID: "generate_skeleton", Label: "故事骨架", Panel: "planning"},
	{ID: "generate_strategy", Label: "改编策略", Panel: "planning"},
	{ID: "generate_script", Label: "剧本", Panel: "planning"},
	{ID: "generate_storyboard", Label: "分镜", Panel: "storyboard"},
	{ID: "extract_assets", Label: "提取资产", Panel: "assets"},
	{ID: "assign_character_voices", Label: "分配音色", Panel: "assets"},
	{ID: "batch_generate_shot_images", Label: "批量生图", Panel: "storyboard"},
	{ID: "batch_generate_shot_videos", Label: "批量生视频", Panel: "storyboard"},
	{ID: "batch_compose_shots", Label: "对白合成", Panel: "video"},
}

// EpisodePipelineDeps holds services needed to run the full episode flow.
type EpisodePipelineDeps struct {
	DB          *sql.DB
	Vendor      adapter.Vendor
	SkillMgr    *skill.Manager
	Queue       *task.Queue
	Pipeline    interface{ Execute(context.Context, *task.Task) error }
	OutputDir   string
	TaskTimeout time.Duration
	NotifyTask  func(t *task.Task, msg string)
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
		items, err := storyboard.LoadStoryboardItems(db, projectID, episodeID)
		return err == nil && len(items) > 0, err
	case "extract_assets":
		items, err := storyboard.LoadStoryboardItems(db, projectID, episodeID)
		if err != nil {
			return false, err
		}
		if len(items) == 0 {
			return false, nil
		}
		n, err := asset.CountProjectAssets(db, projectID)
		return n > 0, err
	case "assign_character_voices":
		items, err := storyboard.LoadStoryboardItems(db, projectID, episodeID)
		if err != nil {
			return false, err
		}
		if len(items) == 0 {
			return false, nil
		}
		n, err := asset.CountProjectAssets(db, projectID)
		if err != nil || n == 0 {
			return false, err
		}
		return voice.RolesHaveVoices(db, projectID)
	case "batch_generate_shot_images":
		items, err := storyboard.LoadStoryboardItems(db, projectID, episodeID)
		if err != nil {
			return false, err
		}
		if len(items) == 0 {
			return false, nil
		}
		need, err := shotsNeedingImages(db, projectID, episodeID)
		return len(need) == 0, err
	case "batch_generate_shot_videos":
		items, err := storyboard.LoadStoryboardItems(db, projectID, episodeID)
		if err != nil {
			return false, err
		}
		if len(items) == 0 {
			return false, nil
		}
		need, err := shotsNeedingVideos(db, projectID, episodeID)
		return len(need) == 0, err
	case "batch_compose_shots":
		items, err := storyboard.LoadStoryboardItems(db, projectID, episodeID)
		if err != nil {
			return false, err
		}
		if len(items) == 0 {
			return false, nil
		}
		return shotsDialogueComposed(db, projectID, episodeID)
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
	items, err := storyboard.LoadStoryboardItems(db, projectID, episodeID)
	if err != nil {
		return nil, err
	}
	var need []int
	for _, it := range items {
		if strings.TrimSpace(it.ImageURL) == "" {
			need = append(need, it.ShotNumber)
		}
	}
	return media.SortShotNumbers(need), nil
}

func shotsNeedingVideos(db *sql.DB, projectID, episodeID string) ([]int, error) {
	items, err := storyboard.LoadStoryboardItems(db, projectID, episodeID)
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
	return media.SortShotNumbers(need), nil
}

func shotsDialogueComposed(db *sql.DB, projectID, episodeID string) (bool, error) {
	items, err := storyboard.LoadStoryboardItems(db, projectID, episodeID)
	if err != nil {
		return false, err
	}
	hasDialogue := false
	for _, it := range items {
		dlg := strings.TrimSpace(it.Dialogue)
		if dlg == "" {
			dlg = storyboard.ExtractDialogueFromDescription(it.Description)
		}
		if media.ParseDialogueForTTS(dlg).Ignorable {
			continue
		}
		hasDialogue = true
		clip, err := media.SelectedClipForShot(db, projectID, episodeID, it.ShotNumber)
		if err != nil || strings.TrimSpace(clip.ComposedFileURL) == "" {
			return false, nil
		}
	}
	if !hasDialogue {
		return true, nil
	}
	return true, nil
}

// EpisodeStepStatus is one row for the episode workbench step bar.
type EpisodeStepStatus struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Panel string `json:"panel"`
	Done  bool   `json:"done"`
}

// ListEpisodePipelineStatus returns all pipeline steps with completion flags.
func ListEpisodePipelineStatus(db *sql.DB, projectID, episodeID string) ([]EpisodeStepStatus, error) {
	if db == nil || projectID == "" || episodeID == "" {
		return nil, fmt.Errorf("project_id and episode_id required")
	}
	out := make([]EpisodeStepStatus, 0, len(episodePipelineOrder))
	for _, step := range episodePipelineOrder {
		done, err := episodeStepDone(db, projectID, episodeID, step.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, EpisodeStepStatus{
			ID: step.ID, Label: step.Label, Panel: step.Panel, Done: done,
		})
	}
	return out, nil
}

// RunEpisodePipeline executes all pending steps for one episode sequentially.
// After each step it re-plans so later steps (e.g. batch images) see updated storyboard state.
func RunEpisodePipeline(ctx context.Context, deps EpisodePipelineDeps, userID, projectID, episodeID string) (*EpisodePipelineResult, error) {
	if deps.DB == nil || deps.Vendor == nil {
		return nil, fmt.Errorf("pipeline dependencies unavailable")
	}
	if episodeID == "" {
		return nil, fmt.Errorf("请先选择一集")
	}

	result := &EpisodePipelineResult{}
	chatAgent := &svcagent.AgentChat{DB: deps.DB, Vendor: deps.Vendor, SkillMgr: deps.SkillMgr, OutputDir: deps.OutputDir}
	ran := map[string]bool{}

	for pass := 0; pass < len(episodePipelineOrder)+2; pass++ {
		if err := core.WaitIfPaused(ctx); err != nil {
			return result, err
		}

		pending, skipped, err := PlanEpisodePipeline(deps.DB, projectID, episodeID)
		if err != nil {
			return result, err
		}
		result.StepsSkip = stepIDs(skipped)
		if len(pending) == 0 {
			break
		}

		step := pending[0]
		if ran[step.ID] {
			return result, fmt.Errorf("%s未能推进流水线，请检查后重试", step.Label)
		}

		total := len(pending)
		core.ReportProgress(ctx, step.ID, float32(pass)*12,
			fmt.Sprintf("[待执行 %d 步] %s — 准备中...", total, step.Label))

		var runErr error
		switch step.ID {
		case "generate_skeleton", "generate_strategy", "generate_script", "generate_storyboard", "extract_assets":
			core.ReportProgress(ctx, step.ID, float32(pass)*12+5,
				fmt.Sprintf("正在%s...", step.Label))
			_, _, runErr = chatAgent.ExecuteAction(ctx, userID, projectID, episodeID, "general", intentForStep(step.ID), step.Label)
		case "assign_character_voices":
			core.ReportProgress(ctx, step.ID, float32(pass)*12+5, "正在分配角色音色...")
			execs := svcagent.NewAgentExecutors(deps.DB, deps.Vendor, deps.SkillMgr)
			_, runErr = (&svcagent.VoiceAssigner{execs}).AssignVoices(ctx, projectID)
		case "batch_generate_shot_images":
			shots, err := shotsNeedingImages(deps.DB, projectID, episodeID)
			if err != nil {
				return result, err
			}
			if len(shots) == 0 {
				ran[step.ID] = true
				continue
			}
			core.ReportProgress(ctx, step.ID, float32(pass)*12+5,
				fmt.Sprintf("批量生图（%d 镜）...", len(shots)))
			stepCtx := core.WithStepProgress(ctx, step.ID, float32(pass)*12+5, 10)
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
				ran[step.ID] = true
				continue
			}
			core.ReportProgress(ctx, step.ID, float32(pass)*12+5,
				fmt.Sprintf("串行生视频（%d 镜）...", len(shots)))
			stepCtx := core.WithStepProgress(ctx, step.ID, float32(pass)*12+5, 10)
			var outcome *media.BatchVideoOutcome
			outcome, runErr = media.GenerateShotClipsSequential(stepCtx, deps.DB, deps.Vendor, deps.OutputDir, projectID, episodeID, shots)
			if outcome != nil {
				result.ShotVideos = len(outcome.Clips)
			}
		case "batch_compose_shots":
			core.ReportProgress(ctx, step.ID, float32(pass)*12+5, "批量合成对白镜头...")
			n, _, err := media.BatchComposeShots(ctx, deps.DB, deps.Vendor, deps.OutputDir, projectID, episodeID)
			if err != nil {
				runErr = err
			} else if n == 0 {
				ran[step.ID] = true
				continue
			}
		default:
			runErr = fmt.Errorf("unknown step: %s", step.ID)
		}

		if runErr != nil {
			return result, fmt.Errorf("%s失败: %w", step.Label, runErr)
		}
		ran[step.ID] = true
		result.StepsRun = append(result.StepsRun, step.ID)
		core.ReportProgress(ctx, step.ID, float32(pass)*12+10,
			fmt.Sprintf("%s 完成", step.Label))
	}

	core.ReportProgress(ctx, "episode_pipeline", 100, "分集流水线全部完成")
	return result, nil
}

func intentForStep(stepID string) *svcagent.ChatActionIntent {
	return &svcagent.ChatActionIntent{Type: stepID}
}

func runEpisodeBatchImages(ctx context.Context, deps EpisodePipelineDeps, userID, projectID, episodeID string, shots []int) error {
	if deps.Queue == nil || deps.Pipeline == nil {
		return fmt.Errorf("生成服务不可用")
	}
	if err := asset.RequireProjectAssets(deps.DB, projectID); err != nil {
		return err
	}
	items, err := storyboard.LoadStoryboardItems(deps.DB, projectID, episodeID)
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
	needShots, err := shotsNeedingImages(deps.DB, projectID, episodeID)
	if err != nil {
		return err
	}
	if len(needShots) == 0 {
		return nil
	}
	tk.GenerateShots = needShots
	tk.SkipExistingImages = true
	tk.Storyboard = items
	core.EnrichTaskMeta(deps.DB, tk)
	tk.SetState(task.StateWaiting, tk.Title)
	if deps.NotifyTask != nil {
		deps.NotifyTask(tk, "批量生图任务已接收")
	}

	done := make(chan error, 1)
	deps.Queue.Submit(tk, func(runCtx context.Context, t *task.Task) error {
		runCtx = core.InheritPipelineContext(ctx, runCtx)
		t.SetState(task.StateDrawing, t.Title)
		if deps.NotifyTask != nil {
			deps.NotifyTask(t, "批量生图中")
		}
		err := deps.Pipeline.Execute(runCtx, t)
		if err != nil {
			if deps.NotifyTask != nil {
				deps.NotifyTask(t, core.MarkTaskFailed(t, err))
			}
		} else if deps.NotifyTask != nil {
			t.SetState(task.StateDone, t.Title)
			t.UpdateProgress(100)
			deps.NotifyTask(t, "批量生图完成")
		}
		if err == nil && t.ProjectID != "" && len(t.Storyboard) > 0 {
			_ = storyboard.SaveStoryboardItems(deps.DB, t.ProjectID, t.EpisodeID, t.Storyboard)
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
