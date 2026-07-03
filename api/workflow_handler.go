package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"toonflow/adapter"
	"toonflow/logger"
	"toonflow/service"
	"toonflow/ws"

	"github.com/gin-gonic/gin"
)

func (r *Router) requireProject(c *gin.Context) (string, bool) {
	projectID := c.Param("id")
	if projectID == "" || !r.ownsProject(currentUserID(c), projectID) {
		c.AbortWithStatus(http.StatusNotFound)
		return "", false
	}
	return projectID, true
}

// ======================== Source Text (原文) ========================

func (r *Router) sourceTextsListHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	rows, err := r.db.Query(`
		SELECT id, project_id, volume, chapter_name, content, events, sort_num, created_at
		FROM o_source_text WHERE project_id = ? ORDER BY sort_num`, projectID)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	var list []service.SourceText
	for rows.Next() {
		var s service.SourceText
		if err := rows.Scan(&s.ID, &s.ProjectID, &s.Volume, &s.ChapterName, &s.Content, &s.Events, &s.SortNum, &s.CreatedAt); err != nil {
			continue
		}
		list = append(list, s)
	}
	if list == nil {
		list = []service.SourceText{}
	}
	c.JSON(http.StatusOK, list)
}

func (r *Router) sourceTextsCreateHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	var req struct {
		Volume      string `json:"volume"`
		ChapterName string `json:"chapter_name"`
		Content     string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}
	if req.Volume == "" {
		req.Volume = "正文卷"
	}

	var maxSort int
	_ = r.db.QueryRow("SELECT COALESCE(MAX(sort_num), 0) FROM o_source_text WHERE project_id = ?", projectID).Scan(&maxSort)

	id := fmt.Sprintf("src_%d", time.Now().UnixNano())
	_, err := r.db.Exec(`
		INSERT INTO o_source_text (id, project_id, volume, chapter_name, content, sort_num)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, projectID, req.Volume, req.ChapterName, req.Content, maxSort+1,
	)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func (r *Router) sourceTextsDeleteHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	textID := c.Param("textId")
	res, err := r.db.Exec("DELETE FROM o_source_text WHERE id = ? AND project_id = ?", textID, projectID)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (r *Router) sourceTextsAnalyzeHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}

	cfg := adapter.ResolveConfigFromDB(r.db.DB)
	if cfg.APIKey == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"error": "未配置 AI API Key，请先在「设置 → AI 供应商」添加 Agnes-AI",
		})
		return
	}

	var chapterCount int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM o_source_text WHERE project_id = ?", projectID).Scan(&chapterCount); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "读取原文失败: " + userMsg(c, err)})
		return
	}
	if chapterCount == 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "请先导入原文"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()

	logID := LogID(c)
	ctx = service.WithProgress(ctx, func(step string, progress float32, message string) {
		if r.wsBroadcaster != nil {
			r.wsBroadcaster.Broadcast(ws.WSResponse{
				Code: 0, Msg: message, Step: "chat_progress", Progress: progress,
				Data: ws.MustMarshalJSON(map[string]interface{}{
					"log_id": logID, "project_id": projectID, "action": step,
				}),
			})
		}
	})

	logger.CtxTrace(ctx, "source text analyze start project=%s chapters=%d", projectID, chapterCount)

	v := r.resolveVendor()
	n, err := service.AnalyzeSourceEvents(ctx, r.db.DB, v, projectID)
	if err != nil {
		logger.CtxError(ctx, err, "source text analyze failed analyzed=%d", n)
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err), "analyzed": n, "log_id": LogID(c)})
		return
	}

	logger.CtxTrace(ctx, "source text analyze done analyzed=%d", n)

	items, _ := r.listSourceTextSummaries(projectID)
	c.JSON(http.StatusOK, gin.H{"analyzed": n, "items": items})
}

func (r *Router) listSourceTextSummaries(projectID string) ([]gin.H, error) {
	rows, err := r.db.Query(`
		SELECT id, chapter_name, events FROM o_source_text WHERE project_id = ? ORDER BY sort_num`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []gin.H
	for rows.Next() {
		var id, chapter, events string
		if rows.Scan(&id, &chapter, &events) == nil {
			items = append(items, gin.H{"id": id, "chapter_name": chapter, "events": events})
		}
	}
	return items, nil
}

// ======================== Episodes (分集) ========================

func (r *Router) episodesListHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	rows, err := r.db.Query(`
		SELECT id, project_id, episode_num, title, params_json, script_content, events_ref, status, created_at
		FROM o_episode WHERE project_id = ? ORDER BY episode_num`, projectID)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	var list []service.Episode
	for rows.Next() {
		var ep service.Episode
		var paramsJSON string
		if err := rows.Scan(&ep.ID, &ep.ProjectID, &ep.EpisodeNum, &ep.Title, &paramsJSON, &ep.ScriptContent, &ep.EventsRef, &ep.Status, &ep.CreatedAt); err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(paramsJSON), &ep.Params)
		list = append(list, ep)
	}
	if list == nil {
		list = []service.Episode{}
	}
	c.JSON(http.StatusOK, list)
}

func (r *Router) episodesSplitHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()

	logID := LogID(c)
	ctx = service.WithProgress(ctx, func(step string, progress float32, message string) {
		if r.wsBroadcaster != nil {
			r.wsBroadcaster.Broadcast(ws.WSResponse{
				Code: 0, Msg: message, Step: "chat_progress", Progress: progress,
				Data: ws.MustMarshalJSON(map[string]interface{}{
					"log_id": logID, "project_id": projectID, "action": step,
				}),
			})
		}
	})

	v := r.resolveVendor()
	eps, err := service.SplitEpisodes(ctx, r.db.DB, v, r.skillMgr, projectID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"episodes": eps})
}

func (r *Router) episodeUpdateHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	epID := c.Param("epId")
	var req struct {
		Title  string                 `json:"title"`
		Params service.EpisodeParams `json:"params"`
		Status string                 `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}

	paramsJSON, _ := json.Marshal(req.Params)
	res, err := r.db.Exec(`
		UPDATE o_episode SET
			title = COALESCE(NULLIF(?, ''), title),
			params_json = CASE WHEN ? = '{}' THEN params_json ELSE ? END,
			status = COALESCE(NULLIF(?, ''), status),
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND project_id = ?`,
		req.Title, string(paramsJSON), string(paramsJSON), req.Status, epID, projectID,
	)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ======================== Agent Work ========================

func (r *Router) agentWorkHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	workType := c.Query("type")
	episodeID := c.Query("episode_id")
	if workType == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "type required"})
		return
	}

	var content string
	err := r.db.QueryRow(`
		SELECT content FROM o_agent_work WHERE project_id = ? AND episode_id = ? AND work_type = ?`,
		projectID, episodeID, workType,
	).Scan(&content)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusOK, gin.H{"content": ""})
		return
	}
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": service.SanitizeWorkContent(content)})
}

func (r *Router) agentWorkGenerateHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	var req struct {
		EpisodeID string `json:"episode_id" binding:"required"`
		Type      string `json:"type" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}
	switch req.Type {
	case "skeleton", "strategy", "script":
	default:
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "type must be skeleton, strategy, or script"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()

	agent := &service.AgentChat{DB: r.db.DB, Vendor: r.resolveVendor(), SkillMgr: r.skillMgr}
	content, err := agent.GenerateWork(ctx, projectID, req.EpisodeID, req.Type)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": content, "type": req.Type})
}

// ======================== AI Chat ========================

func (r *Router) chatListHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	episodeID := c.Query("episode_id")
	rows, err := r.db.Query(`
		SELECT role, content, action_json, created_at FROM o_chat_message
		WHERE project_id = ? AND (episode_id = ? OR (? = '' AND episode_id = ''))
		ORDER BY created_at ASC LIMIT 100`,
		projectID, episodeID, episodeID,
	)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	var msgs []map[string]interface{}
	for rows.Next() {
		var role, content, action, createdAt string
		if rows.Scan(&role, &content, &action, &createdAt) == nil {
			msgs = append(msgs, gin.H{
				"role": role, "content": content, "action": action, "created_at": createdAt,
			})
		}
	}
	if msgs == nil {
		msgs = []map[string]interface{}{}
	}
	c.JSON(http.StatusOK, msgs)
}

func (r *Router) chatSendHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	var req struct {
		Message   string `json:"message" binding:"required"`
		EpisodeID string `json:"episode_id"`
		Stage     string `json:"stage"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}
	if req.Stage == "" {
		req.Stage = "general"
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()

	logID := LogID(c)
	progressFn := func(step string, progress float32, message string) {
		if r.wsBroadcaster != nil {
			r.wsBroadcaster.Broadcast(ws.WSResponse{
				Code:     0,
				Msg:      message,
				Step:     "chat_progress",
				Progress: progress,
				Data: ws.MustMarshalJSON(map[string]interface{}{
					"log_id":     logID,
					"project_id": projectID,
					"action":     step,
				}),
			})
		}
	}
	streamFn := func(delta string) {
		if r.wsBroadcaster == nil || delta == "" {
			return
		}
		r.wsBroadcaster.Broadcast(ws.WSResponse{
			Code: 0,
			Step: "chat_stream",
			Data: ws.MustMarshalJSON(map[string]interface{}{
				"log_id":     logID,
				"project_id": projectID,
				"delta":      delta,
			}),
		})
	}
	streamEndFn := func() {
		if r.wsBroadcaster == nil {
			return
		}
		r.wsBroadcaster.Broadcast(ws.WSResponse{
			Code: 0,
			Step: "chat_stream_end",
			Data: ws.MustMarshalJSON(map[string]interface{}{
				"log_id":     logID,
				"project_id": projectID,
			}),
		})
	}
	ctx = service.WithProgress(ctx, progressFn)
	ctx = service.WithStreamDelta(ctx, streamFn)
	ctx = service.WithStreamEnd(ctx, streamEndFn)

	agent := &service.AgentChat{DB: r.db.DB, Vendor: r.resolveVendor(), SkillMgr: r.skillMgr}
	resp, err := agent.HandleMessage(ctx, currentUserID(c), projectID, req.EpisodeID, req.Stage, req.Message)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (r *Router) chatActionHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	var req struct {
		Action    string            `json:"action" binding:"required"`
		EpisodeID string            `json:"episode_id"`
		Params    map[string]string `json:"params"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()

	logID := LogID(c)
	progressFn := func(step string, progress float32, message string) {
		if r.wsBroadcaster != nil {
			r.wsBroadcaster.Broadcast(ws.WSResponse{
				Code: 0, Msg: message, Step: "chat_progress", Progress: progress,
				Data: ws.MustMarshalJSON(map[string]interface{}{
					"log_id": logID, "project_id": projectID, "action": step,
				}),
			})
		}
	}
	ctx = service.WithProgress(ctx, progressFn)

	agent := &service.AgentChat{DB: r.db.DB, Vendor: r.resolveVendor(), SkillMgr: r.skillMgr}
	intent := &service.ChatActionIntent{Type: req.Action, Params: req.Params}
	resp, err := agent.RunAction(ctx, currentUserID(c), projectID, req.EpisodeID, "general", intent)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (r *Router) resolveVendor() adapter.Vendor {
	if r.adapter != nil {
		return r.adapter
	}
	return adapter.ResolveFromDB(r.db.DB, "agnes_ai")
}
