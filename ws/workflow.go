package ws

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"toonflow/adapter"
	"toonflow/logger"
	"toonflow/service"
	"toonflow/skill"
	"toonflow/task"
)

var allowedWorkflowActions = map[string]bool{
	"analyze_events":             true,
	"split_episodes":             true,
	"generate_skeleton":          true,
	"generate_strategy":          true,
	"generate_script":            true,
	"generate_storyboard":        true,
	"extract_assets":             true,
	"generate_shot_image":        true,
	"batch_generate_shot_images": true,
	"generate_shot_video":        true,
	"batch_generate_shot_videos": true,
	"delete_shot_clip":           true,
}

var workflowNeedsEpisode = map[string]bool{
	"generate_skeleton":          true,
	"generate_strategy":          true,
	"generate_script":            true,
	"generate_storyboard":        true,
	"extract_assets":             true,
	"generate_shot_image":        true,
	"batch_generate_shot_images": true,
	"generate_shot_video":        true,
	"batch_generate_shot_videos": true,
}

// WorkflowService handles WebSocket-triggered project workflow steps.
type WorkflowService struct {
	DB              *sql.DB
	DefaultVendorID string
	SkillMgr        *skill.Manager
	Timeout         time.Duration
	Queue           *task.Queue
	Pipeline        PipelineRunner
	OutputDir       string
}

// NewWorkflowService creates a workflow service for WS actions.
func NewWorkflowService(db *sql.DB, defaultVendorID string, skillMgr *skill.Manager, timeout time.Duration) *WorkflowService {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	if defaultVendorID == "" {
		defaultVendorID = "agnes_ai"
	}
	return &WorkflowService{
		DB:              db,
		DefaultVendorID: defaultVendorID,
		SkillMgr:        skillMgr,
		Timeout:         timeout,
	}
}

// SetTaskRunner wires queue/pipeline for image and video generation tasks.
func (wfs *WorkflowService) SetTaskRunner(q *task.Queue, pipeline PipelineRunner, outputDir string) {
	wfs.Queue = q
	wfs.Pipeline = pipeline
	wfs.OutputDir = outputDir
}

func (wfs *WorkflowService) resolveVendor() adapter.Vendor {
	if wfs == nil {
		return nil
	}
	return adapter.ResolveFromDB(wfs.DB, wfs.DefaultVendorID)
}

func (wfs *WorkflowService) handleRunWorkflow(cm *ConnManager, userID string, req *WSRequest) {
	if wfs == nil || wfs.DB == nil {
		cm.Broadcast(WSResponse{Code: 1, Msg: "workflow service unavailable", Step: "workflow_error"})
		return
	}
	if userID == "" {
		cm.Broadcast(WSResponse{Code: 1, Msg: "unauthorized", Step: "workflow_error"})
		return
	}

	action := req.WorkflowAction
	if action == "" {
		cm.Broadcast(WSResponse{Code: 1, Msg: "workflow_action is required", Step: "workflow_error"})
		return
	}
	if !allowedWorkflowActions[action] {
		cm.Broadcast(WSResponse{Code: 1, Msg: fmt.Sprintf("unsupported workflow action: %s", action), Step: "workflow_error"})
		return
	}
	if req.ProjectID == "" {
		cm.Broadcast(WSResponse{Code: 1, Msg: "project_id is required", Step: "workflow_error"})
		return
	}
	if !wfs.ownsProject(userID, req.ProjectID) {
		cm.Broadcast(WSResponse{Code: 1, Msg: "project not found", Step: "workflow_error"})
		return
	}
	if workflowNeedsEpisode[action] && req.EpisodeID == "" {
		cm.Broadcast(WSResponse{Code: 1, Msg: "请先选择一集", Step: "workflow_error"})
		return
	}
	if action == "delete_shot_clip" && req.ClipID == "" {
		cm.Broadcast(WSResponse{Code: 1, Msg: "clip_id is required", Step: "workflow_error"})
		return
	}

	logID := fmt.Sprintf("wf_%d", time.Now().UnixNano())
	cm.Broadcast(WSResponse{
		Code: 0, Msg: "任务已接收", Step: "waiting", Progress: 0,
		Data: MustMarshalJSON(map[string]interface{}{
			"log_id": logID, "project_id": req.ProjectID, "action": action,
		}),
	})

	go wfs.runWorkflow(cm, userID, req, logID, action)
}

func (wfs *WorkflowService) runWorkflow(cm *ConnManager, userID string, req *WSRequest, logID, action string) {
	ctx, cancel := context.WithTimeout(context.Background(), wfs.Timeout)
	defer cancel()

	ctx = logger.WithID(ctx, logID)
	ctx = service.WithProgress(ctx, func(step string, progress float32, message string) {
		cm.Broadcast(WSResponse{
			Code: 0, Msg: message, Step: "chat_progress", Progress: progress,
			Data: MustMarshalJSON(map[string]interface{}{
				"log_id": logID, "project_id": req.ProjectID, "action": step,
			}),
		})
	})

	switch action {
	case "batch_generate_shot_images":
		out, err := wfs.runBatchImages(ctx, cm, userID, req)
		wfs.finishWorkflow(cm, req, logID, action, out, err)
		return
	case "generate_shot_video":
		out, err := wfs.runShotVideos(ctx, cm, userID, req, false)
		wfs.finishWorkflow(cm, req, logID, action, out, err)
		return
	case "batch_generate_shot_videos":
		out, err := wfs.runShotVideos(ctx, cm, userID, req, true)
		wfs.finishWorkflow(cm, req, logID, action, out, err)
		return
	case "delete_shot_clip":
		out, err := wfs.runDeleteClip(req)
		wfs.finishWorkflow(cm, req, logID, action, out, err)
		return
	}

	v := wfs.resolveVendor()
	agent := &service.AgentChat{DB: wfs.DB, Vendor: v, SkillMgr: wfs.SkillMgr}
	intent := &service.ChatActionIntent{Type: action, Params: req.WorkflowParams}
	resp, err := agent.RunAction(ctx, userID, req.ProjectID, req.EpisodeID, "general", intent)
	if err != nil {
		logger.CtxError(ctx, err, "ws workflow failed action=%s project=%s", action, req.ProjectID)
		cm.Broadcast(WSResponse{
			Code: 1, Msg: err.Error(), Step: "workflow_error", Progress: 0,
			Data: MustMarshalJSON(map[string]interface{}{
				"log_id": logID, "project_id": req.ProjectID, "action": action,
			}),
		})
		return
	}

	reply := ""
	if resp != nil {
		reply = resp.Reply
	}
	data := map[string]interface{}{
		"log_id":     logID,
		"project_id": req.ProjectID,
		"action":     action,
		"reply":      reply,
	}
	if resp != nil {
		if resp.Work != nil {
			data["work"] = resp.Work
		}
		if resp.Action != nil {
			data["action_result"] = resp.Action
		}
	}

	logger.CtxTrace(ctx, "ws workflow done action=%s project=%s", action, req.ProjectID)
	cm.Broadcast(WSResponse{
		Code: 0, Msg: reply, Step: "workflow_done", Progress: 100,
		Data: MustMarshalJSON(data),
	})
}

type workflowOutcome struct {
	reply string
	work  interface{}
	extra map[string]interface{}
}

func (wfs *WorkflowService) finishWorkflow(cm *ConnManager, req *WSRequest, logID, action string, out workflowOutcome, err error) {
	if err != nil {
		cm.Broadcast(WSResponse{
			Code: 1, Msg: err.Error(), Step: "workflow_error", Progress: 0,
			Data: MustMarshalJSON(map[string]interface{}{
				"log_id": logID, "project_id": req.ProjectID, "action": action,
			}),
		})
		return
	}
	data := map[string]interface{}{
		"log_id":     logID,
		"project_id": req.ProjectID,
		"action":     action,
		"reply":      out.reply,
	}
	if out.work != nil {
		data["work"] = out.work
	}
	for k, v := range out.extra {
		data[k] = v
	}
	cm.Broadcast(WSResponse{
		Code: 0, Msg: out.reply, Step: "workflow_done", Progress: 100,
		Data: MustMarshalJSON(data),
	})
}

func (wfs *WorkflowService) runBatchImages(ctx context.Context, cm *ConnManager, userID string, req *WSRequest) (workflowOutcome, error) {
	if wfs.Queue == nil || wfs.Pipeline == nil {
		return workflowOutcome{}, fmt.Errorf("生成服务不可用")
	}
	shots := req.ShotNumbers
	if len(shots) == 0 {
		return workflowOutcome{}, fmt.Errorf("请指定要生成的镜号")
	}
	if err := service.RequireProjectAssets(wfs.DB, req.ProjectID); err != nil {
		return workflowOutcome{}, err
	}
	items, err := service.LoadStoryboardItems(wfs.DB, req.ProjectID, req.EpisodeID)
	if err != nil || len(items) == 0 {
		return workflowOutcome{}, fmt.Errorf("请先生成分镜")
	}

	var artStyle, videoRatio string
	_ = wfs.DB.QueryRow("SELECT art_style, video_ratio FROM o_project WHERE id = ?", req.ProjectID).Scan(&artStyle, &videoRatio)
	resolution := "1280x720"
	if videoRatio == "9:16" {
		resolution = "720x1280"
	}

	id := fmt.Sprintf("task_%d", time.Now().UnixNano())
	tk := task.NewTask(id, req.ProjectID, "", artStyle, 3, resolution, 24, wfs.Timeout)
	tk.UserID = userID
	tk.Mode = "images"
	tk.EpisodeID = req.EpisodeID
	tk.GenerateShots = shots
	tk.Storyboard = items
	service.EnrichTaskMeta(wfs.DB, tk)
	tk.SetState(task.StateWaiting, tk.Title)
	wfs.broadcastTaskUpdate(cm, tk, "任务已接收")

	wfs.Queue.Submit(tk, func(runCtx context.Context, t *task.Task) error {
		runCtx = logger.WithID(runCtx, t.ID)
		if err := wfs.Pipeline.Execute(runCtx, t); err != nil {
			wfs.broadcastTaskUpdate(cm, t, err.Error())
			return err
		}
		if t.ProjectID != "" && len(t.Storyboard) > 0 {
			_ = service.SaveStoryboardItems(wfs.DB, t.ProjectID, t.EpisodeID, t.Storyboard)
		}
		wfs.broadcastTaskUpdate(cm, t, "图片生成完成")
		return nil
	})

	reply := fmt.Sprintf("已提交 %d 个分镜的图片生成任务", len(shots))
	return workflowOutcome{
		reply: reply,
		extra: map[string]interface{}{
			"task_id":      tk.ID,
			"shot_numbers": shots,
		},
	}, nil
}

func (wfs *WorkflowService) runShotVideos(ctx context.Context, cm *ConnManager, userID string, req *WSRequest, batch bool) (workflowOutcome, error) {
	if wfs.Queue == nil {
		return workflowOutcome{}, fmt.Errorf("生成服务不可用")
	}
	shots := req.ShotNumbers
	if !batch {
		if len(shots) == 0 && req.WorkflowParams != nil {
			if raw := req.WorkflowParams["shot_number"]; raw != "" {
				if n, err := strconv.Atoi(raw); err == nil && n > 0 {
					shots = []int{n}
				}
			}
		}
		if len(shots) == 0 {
			return workflowOutcome{}, fmt.Errorf("请指定镜号")
		}
		shots = shots[:1]
	}
	if len(shots) == 0 {
		return workflowOutcome{}, fmt.Errorf("请至少选择一个分镜")
	}

	var taskIDs []string
	for _, shotNum := range shots {
		tk, err := wfs.submitShotVideoTask(ctx, cm, userID, req.ProjectID, req.EpisodeID, shotNum)
		if err != nil {
			return workflowOutcome{}, err
		}
		taskIDs = append(taskIDs, tk.ID)
	}

	reply := fmt.Sprintf("已提交 %d 个分镜的视频生成任务", len(shots))
	return workflowOutcome{
		reply: reply,
		extra: map[string]interface{}{
			"task_ids":     taskIDs,
			"shot_numbers": shots,
		},
	}, nil
}

func (wfs *WorkflowService) submitShotVideoTask(ctx context.Context, cm *ConnManager, userID, projectID, episodeID string, shotNum int) (*task.Task, error) {
	id := fmt.Sprintf("task_%d", time.Now().UnixNano())
	tk := task.NewTask(id, projectID, "", "", 3, "1280x720", 24, 15*time.Minute)
	tk.UserID = userID
	tk.Mode = "video"
	tk.EpisodeID = episodeID
	tk.GenerateShots = []int{shotNum}
	service.EnrichTaskMeta(wfs.DB, tk)
	tk.SetState(task.StateWaiting, tk.Title)
	wfs.broadcastTaskUpdate(cm, tk, "视频任务已接收")

	wfs.Queue.Submit(tk, func(runCtx context.Context, t *task.Task) error {
		runCtx = logger.WithID(runCtx, t.ID)
		t.SetState(task.StateVideoGen, t.Title)
		t.UpdateProgress(5)
		wfs.broadcastTaskUpdate(cm, t, "视频生成中")

		clip, err := service.GenerateShotClip(runCtx, wfs.DB, wfs.resolveVendor(), wfs.OutputDir, projectID, episodeID, shotNum)
		if err != nil {
			wfs.broadcastTaskUpdate(cm, t, err.Error())
			return err
		}
		t.UpdateProgress(100)
		t.SetState(task.StateDone, t.Title)
		wfs.broadcastTaskUpdate(cm, t, "视频生成完成")
		logger.CtxTrace(runCtx, "shot video task done shot=%d version=%d source=%s", shotNum, clip.Version, clip.Source)
		return nil
	})
	return tk, nil
}

func (wfs *WorkflowService) runDeleteClip(req *WSRequest) (workflowOutcome, error) {
	if err := service.DeleteShotClip(wfs.DB, wfs.OutputDir, req.ClipID); err != nil {
		return workflowOutcome{}, err
	}
	return workflowOutcome{reply: "✅ 视频版本已删除"}, nil
}

func (wfs *WorkflowService) broadcastTaskUpdate(cm *ConnManager, t *task.Task, msg string) {
	if cm == nil || t == nil {
		return
	}
	cm.Broadcast(WSResponse{
		Code:     0,
		Msg:      msg,
		Step:     string(t.State),
		Progress: t.Progress,
		Data: MustMarshalJSON(map[string]interface{}{
			"task_id":     t.ID,
			"task_update": true,
			"title":       t.Title,
			"state":       t.State,
			"mode":        t.Mode,
			"project_id":  t.ProjectID,
			"episode_id":  t.EpisodeID,
		}),
	})
}

func (wfs *WorkflowService) ownsProject(userID, projectID string) bool {
	var owner string
	err := wfs.DB.QueryRow("SELECT user_id FROM o_project WHERE id = ?", projectID).Scan(&owner)
	return err == nil && owner == userID
}
