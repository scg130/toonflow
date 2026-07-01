package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"toonflow/adapter"
	"toonflow/auth"
	"toonflow/engine"
	"toonflow/service"
	"toonflow/skill"
	"toonflow/storage"
	"toonflow/task"
	"toonflow/ws"

	"github.com/gin-gonic/gin"
)

// Router holds dependencies for all HTTP handlers.
type Router struct {
	db            *storage.DB
	queue         *task.Queue
	cfg           *engine.Config
	pipeline      *engine.Pipeline
	skillMgr      *skill.Manager
	adapter       adapter.Vendor
	wsBroadcaster *ws.ConnManager
	sessions      *auth.Store
	outputDir     string
	staticDir     string
	port          int
}

// NewRouter creates a new Router with the given dependencies.
func NewRouter(db *storage.DB, queue *task.Queue, engineCfg *engine.Config, pipeline *engine.Pipeline, skillMgr *skill.Manager, v adapter.Vendor, wsBC *ws.ConnManager, sessions *auth.Store, outputDir, staticDir string, port int) *Router {
	return &Router{
		db:            db,
		queue:         queue,
		cfg:           engineCfg,
		pipeline:      pipeline,
		skillMgr:      skillMgr,
		adapter:       v,
		wsBroadcaster: wsBC,
		sessions:      sessions,
		outputDir:     outputDir,
		staticDir:     staticDir,
		port:          port,
	}
}

// Setup registers all routes and returns a configured *gin.Engine.
func (r *Router) Setup() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(RequestLogMiddleware())

	// --- Static files ---
	if _, err := os.Stat(r.staticDir); err == nil {
		engine.StaticFS("/static", http.Dir(r.staticDir))
	}

	// --- Output files (generated images/videos) ---
	engine.GET("/output/*filepath", r.outputHandler)

	// --- Download ---
	engine.GET("/download/*filepath", r.downloadHandler)

	// --- WebSocket (auth via query token) ---
	engine.GET("/ws", r.wsHandler)

	// --- Public API ---
	api := engine.Group("/api")
	{
		api.GET("/health", r.healthHandler)
		api.POST("/login", r.loginHandler)
	}

	// --- Protected API ---
	protected := api.Group("")
	protected.Use(AuthRequired(r.sessions))
	{
		protected.GET("/me", r.meHandler)
		protected.POST("/logout", r.logoutHandler)

		protected.GET("/vendors", r.vendorsHandler)
		protected.GET("/vendors/active", r.vendorsActiveHandler)
		protected.POST("/vendors", r.vendorsCreateHandler)
		protected.PATCH("/vendors/:id", r.vendorsUpdateHandler)
		protected.PUT("/vendors/:id", r.vendorsToggleHandler)
		protected.DELETE("/vendors/:id", r.vendorsDeleteHandler)
		protected.GET("/tasks", r.tasksHandler)

		protected.GET("/projects", r.projectsListHandler)
		protected.POST("/projects", r.projectsCreateHandler)
		protected.GET("/projects/:id", r.projectDetailHandler)
		protected.PUT("/projects/:id", r.projectUpdateHandler)
		protected.DELETE("/projects/:id", r.projectDeleteHandler)

		protected.GET("/projects/:id/source-texts", r.sourceTextsListHandler)
		protected.POST("/projects/:id/source-texts", r.sourceTextsCreateHandler)
		protected.DELETE("/projects/:id/source-texts/:textId", r.sourceTextsDeleteHandler)
		protected.POST("/projects/:id/source-texts/analyze", r.sourceTextsAnalyzeHandler)

		protected.GET("/projects/:id/episodes", r.episodesListHandler)
		protected.POST("/projects/:id/episodes/split", r.episodesSplitHandler)
		protected.PATCH("/projects/:id/episodes/:epId", r.episodeUpdateHandler)

		protected.GET("/projects/:id/agent-work", r.agentWorkHandler)
		protected.POST("/projects/:id/agent-work/generate", r.agentWorkGenerateHandler)
		protected.GET("/projects/:id/chat", r.chatListHandler)
		protected.POST("/projects/:id/chat", r.chatSendHandler)
		protected.POST("/projects/:id/chat/action", r.chatActionHandler)

		protected.GET("/projects/:id/assets", r.projectAssetsListHandler)
		protected.POST("/projects/:id/assets/extract", r.assetsExtractHandler)

		protected.GET("/assets", r.assetsListHandler)
		protected.POST("/assets", r.assetsCreateHandler)
		protected.PUT("/assets/:id", r.assetUpdateHandler)
		protected.DELETE("/assets/:id", r.assetDeleteHandler)

		protected.GET("/storyboards", r.storyboardsListHandler)
		protected.GET("/styles", r.stylesHandler)

		protected.GET("/projects/:id/shot-clips", r.shotClipsListHandler)
		protected.POST("/projects/:id/episodes/:epId/shots/:shotNum/generate-video", r.shotClipGenerateHandler)
		protected.POST("/projects/:id/generate/images", r.generateImagesHandler)
		protected.PUT("/shot-clips/:clipId/select", r.shotClipSelectHandler)
		protected.DELETE("/shot-clips/:clipId", r.shotClipDeleteHandler)
		protected.GET("/projects/:id/timeline", r.timelineGetHandler)
		protected.PUT("/projects/:id/timeline", r.timelineSaveHandler)
		protected.POST("/projects/:id/timeline/export", r.timelineExportHandler)

		protected.GET("/settings", r.settingsGetHandler)
		protected.PUT("/settings", r.settingsUpdateHandler)

		protected.POST("/models/test/text", r.modelTestTextHandler)
		protected.POST("/models/test/image", r.modelTestImageHandler)
		protected.POST("/models/test/video", r.modelTestVideoHandler)
	}

	// --- Root: serve index.html ---
	engine.GET("/", r.indexHandler)

	return engine
}

// ======================== WebSocket ========================

func (r *Router) wsHandler(c *gin.Context) {
	token := authToken(c)
	sess, ok := r.sessions.Get(token)
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	r.wsBroadcaster.ServeHTTP(c.Writer, c.Request, sess.UserID)
}

// ======================== Health ========================

func (r *Router) healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":         "ok",
		"active_tasks":   r.queue.ActiveCount(),
		"max_concurrent": 5,
	})
}

// ======================== Vendors ========================

func validateVendorAPIKey(key string) error {
	key = adapter.SanitizeAPIKey(key)
	if key == "" {
		return fmt.Errorf("API Key 不能为空")
	}
	if adapter.IsLikelyAPIURL(key) {
		return fmt.Errorf("API Key 不能是 URL，请填写 Agnes 控制台 (platform.agnes-ai.com) 获取的密钥")
	}
	return nil
}

func (r *Router) vendorsActiveHandler(c *gin.Context) {
	cfg := adapter.ResolveConfigFromDB(r.db.DB)
	c.JSON(http.StatusOK, gin.H{
		"base_url": cfg.Info.BaseURL,
		"key_hint": cfg.Info.KeyHint,
		"source":   cfg.Info.Source,
		"configured": cfg.APIKey != "",
	})
}

func (r *Router) vendorsHandler(c *gin.Context) {
	rows, err := r.db.Query("SELECT id, name, input_values, enable FROM o_vendorConfig ORDER BY created_at DESC")
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	var vendors []map[string]interface{}
	for rows.Next() {
		var id, name, inputValues string
		var enable int
		if err := rows.Scan(&id, &name, &inputValues, &enable); err != nil {
			continue
		}
		// Parse input_values to extract url and key
		var inputVals map[string]string
		if err := json.Unmarshal([]byte(inputValues), &inputVals); err != nil {
			inputVals = make(map[string]string)
		}
		vendors = append(vendors, gin.H{
			"id":     id,
			"name":   name,
			"url":    inputVals["url"],
			"enable": enable == 1,
		})
	}
	c.JSON(http.StatusOK, vendors)
}

func (r *Router) vendorsCreateHandler(c *gin.Context) {
	var req struct {
		Name   string `json:"name" binding:"required"`
		URL    string `json:"url" binding:"required"`
		APIKey string `json:"api_key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	apiKey := adapter.SanitizeAPIKey(req.APIKey)
	if err := validateVendorAPIKey(apiKey); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	normalizedURL := adapter.NormalizeAgnesBaseURL(req.URL)
	if apiKey == normalizedURL {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "API Key 与 API 地址相同，请检查是否填错字段"})
		return
	}

	// Validate key against Agnes API before saving
	v := adapter.NewAgnesAIVendor(normalizedURL, apiKey)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	if _, err := v.TextRequest(ctx, adapter.DefaultTextModel, adapter.TextParams{
		Messages:  []adapter.TextMessage{{Role: "user", Content: "ping"}},
		MaxTokens: 5,
	}); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "API Key 验证失败: " + err.Error()})
		return
	}

	id := fmt.Sprintf("vendor_%d", time.Now().UnixNano())
	inputValues, _ := json.Marshal(map[string]string{
		"url": normalizedURL,
		"key": apiKey,
	})
	_, err := r.db.Exec(
		"INSERT INTO o_vendorConfig (id, name, version, input_values, models_json, enable) VALUES (?, ?, ?, ?, ?, 1)",
		id, req.Name, "1.0.0", string(inputValues), "[]",
	)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	// Only one active vendor at a time
	r.db.Exec("UPDATE o_vendorConfig SET enable = 0 WHERE id != ?", id)
	r.adapter = adapter.ResolveFromDB(r.db.DB, "agnes_ai")
	c.JSON(http.StatusCreated, gin.H{"id": id, "name": req.Name})
}

func (r *Router) vendorsUpdateHandler(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Name   string `json:"name"`
		URL    string `json:"url"`
		APIKey string `json:"api_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	var inputValues string
	if err := r.db.QueryRow("SELECT input_values FROM o_vendorConfig WHERE id = ?", id).Scan(&inputValues); err != nil {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "vendor not found"})
		return
	}

	var vals map[string]string
	if err := json.Unmarshal([]byte(inputValues), &vals); err != nil {
		vals = make(map[string]string)
	}

	if req.Name != "" {
		r.db.Exec("UPDATE o_vendorConfig SET name = ? WHERE id = ?", req.Name, id)
	}
	if req.URL != "" {
		vals["url"] = adapter.NormalizeAgnesBaseURL(req.URL)
	}
	if req.APIKey != "" {
		apiKey := adapter.SanitizeAPIKey(req.APIKey)
		if err := validateVendorAPIKey(apiKey); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		url := vals["url"]
		if url == "" {
			url = vals["base_url"]
		}
		if apiKey == adapter.NormalizeAgnesBaseURL(url) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "API Key 与 API 地址相同，请检查是否填错字段"})
			return
		}
		v := adapter.NewAgnesAIVendor(url, apiKey)
		ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
		defer cancel()
		if _, err := v.TextRequest(ctx, adapter.DefaultTextModel, adapter.TextParams{
			Messages:  []adapter.TextMessage{{Role: "user", Content: "ping"}},
			MaxTokens: 5,
		}); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "API Key 验证失败: " + err.Error()})
			return
		}
		vals["key"] = apiKey
	}

	updated, _ := json.Marshal(vals)
	r.db.Exec("UPDATE o_vendorConfig SET input_values = ? WHERE id = ?", string(updated), id)
	r.adapter = adapter.ResolveFromDB(r.db.DB, "agnes_ai")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (r *Router) vendorsToggleHandler(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Enable int `json:"enable"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}
	if req.Enable == 1 {
		r.db.Exec("UPDATE o_vendorConfig SET enable = 0 WHERE id != ?", id)
	}
	r.db.Exec("UPDATE o_vendorConfig SET enable = ? WHERE id = ?", req.Enable, id)
	r.adapter = adapter.ResolveFromDB(r.db.DB, "agnes_ai")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (r *Router) vendorsDeleteHandler(c *gin.Context) {
	id := c.Param("id")
	r.db.Exec("DELETE FROM o_vendorConfig WHERE id = ?", id)
	r.adapter = adapter.ResolveFromDB(r.db.DB, "agnes_ai")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ======================== Tasks ========================

func (r *Router) tasksHandler(c *gin.Context) {
	userID := currentUserID(c)
	c.JSON(http.StatusOK, r.queue.AllTasksForUser(userID))
}

func (r *Router) broadcastTaskUpdate(t *task.Task, msg string) {
	if r.wsBroadcaster == nil || t == nil {
		return
	}
	r.wsBroadcaster.Broadcast(ws.WSResponse{
		Code:     0,
		Msg:      msg,
		Step:     string(t.State),
		Progress: t.Progress,
		Data: ws.MustMarshalJSON(map[string]interface{}{
			"task_id":     t.ID,
			"task_update": true,
			"title":       t.Title,
			"state":       t.State,
		}),
	})
}

// ======================== Projects ========================

func (r *Router) ownsProject(userID, projectID string) bool {
	var owner string
	err := r.db.QueryRow("SELECT user_id FROM o_project WHERE id = ?", projectID).Scan(&owner)
	return err == nil && owner == userID
}

func (r *Router) projectsListHandler(c *gin.Context) {
	userID := currentUserID(c)
	rows, err := r.db.Query(
		"SELECT id, name, intro, type, art_style, video_ratio, status, create_time FROM o_project WHERE user_id = ? ORDER BY create_time DESC",
		userID,
	)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	var projects []map[string]interface{}
	for rows.Next() {
		var id, name, intro, vType, artStyle, vRatio, status, createTime string
		if err := rows.Scan(&id, &name, &intro, &vType, &artStyle, &vRatio, &status, &createTime); err != nil {
			continue
		}
		projects = append(projects, gin.H{
			"id":          id,
			"name":        name,
			"intro":       intro,
			"type":        vType,
			"art_style":   artStyle,
			"video_ratio": vRatio,
			"status":      status,
			"create_time": createTime,
		})
	}
	c.JSON(http.StatusOK, projects)
}

func (r *Router) projectsCreateHandler(c *gin.Context) {
	var proj struct {
		Name       string `json:"name" binding:"required"`
		Intro      string `json:"intro"`
		Type       string `json:"type"`
		ArtStyle   string `json:"art_style"`
		VideoRatio string `json:"video_ratio"`
		ImageModel string `json:"image_model"`
		Status     string `json:"status"`
	}
	if err := c.ShouldBindJSON(&proj); err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}
	if proj.Status == "" {
		proj.Status = "draft"
	}
	if proj.VideoRatio == "" {
		proj.VideoRatio = "16:9"
	}

	id := fmt.Sprintf("proj_%d", time.Now().UnixNano())
	userID := currentUserID(c)
	_, err := r.db.Exec(
		"INSERT INTO o_project (id, user_id, name, intro, type, art_style, video_ratio, image_model, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		id, userID, proj.Name, proj.Intro, proj.Type, proj.ArtStyle, proj.VideoRatio, proj.ImageModel, proj.Status,
	)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "name": proj.Name, "status": "draft"})
}

func (r *Router) projectDetailHandler(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if !r.ownsProject(currentUserID(c), id) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	var projName, intro, vType, artStyle, vRatio, imgModel, vidModel, mode, status, createTime string
	err := r.db.QueryRow(
		"SELECT name, intro, type, art_style, video_ratio, image_model, video_model, mode, status, create_time FROM o_project WHERE id = ? AND user_id = ?",
		id, currentUserID(c),
	).Scan(&projName, &intro, &vType, &artStyle, &vRatio, &imgModel, &vidModel, &mode, &status, &createTime)
	if err == sql.ErrNoRows {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":            id,
		"name":          projName,
		"intro":         intro,
		"type":          vType,
		"art_style":     artStyle,
		"video_ratio":   vRatio,
		"image_model":   imgModel,
		"video_model":   vidModel,
		"mode":          mode,
		"status":        status,
		"create_time":   createTime,
		"assets":        r.queryAssetsForProject(id),
	})
}

func (r *Router) projectUpdateHandler(c *gin.Context) {
	id := c.Param("id")
	userID := currentUserID(c)
	if !r.ownsProject(userID, id) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	var proj struct {
		Name       string `json:"name"`
		Intro      string `json:"intro"`
		Type       string `json:"type"`
		ArtStyle   string `json:"art_style"`
		VideoRatio string `json:"video_ratio"`
		ImageModel string `json:"image_model"`
		VideoModel string `json:"video_model"`
		Status     string `json:"status"`
	}
	if err := c.ShouldBindJSON(&proj); err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	_, err := r.db.Exec(`
		UPDATE o_project SET
			name = COALESCE(NULLIF(?, ''), name),
			intro = ?,
			type = COALESCE(NULLIF(?, ''), type),
			art_style = COALESCE(NULLIF(?, ''), art_style),
			video_ratio = COALESCE(NULLIF(?, ''), video_ratio),
			image_model = COALESCE(NULLIF(?, ''), image_model),
			video_model = COALESCE(NULLIF(?, ''), video_model),
			status = COALESCE(NULLIF(?, ''), status),
			update_time = CURRENT_TIMESTAMP
		WHERE id = ? AND user_id = ?`,
		proj.Name, proj.Intro, proj.Type, proj.ArtStyle, proj.VideoRatio,
		proj.ImageModel, proj.VideoModel, proj.Status, id, userID,
	)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "status": "ok"})
}

func (r *Router) projectDeleteHandler(c *gin.Context) {
	id := c.Param("id")
	userID := currentUserID(c)
	if !r.ownsProject(userID, id) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	tx, err := r.db.Begin()
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM o_assets WHERE project_id = ? AND user_id = ?", id, userID); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
		if _, err := tx.Exec("DELETE FROM o_storyboard WHERE project_id = ?", id); err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		tx.Exec("DELETE FROM o_source_text WHERE project_id = ?", id)
		tx.Exec("DELETE FROM o_episode WHERE project_id = ?", id)
		tx.Exec("DELETE FROM o_chat_message WHERE project_id = ?", id)
		tx.Exec("DELETE FROM o_agent_work WHERE project_id = ?", id)
		if _, err := tx.Exec("DELETE FROM o_project WHERE id = ? AND user_id = ?", id, userID); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	if err := tx.Commit(); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ======================== Assets ========================

func (r *Router) assetsListHandler(c *gin.Context) {
	userID := currentUserID(c)
	projectID := c.Query("project_id")

	if projectID != "" && !r.ownsProject(userID, projectID) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	var query string
	var args []interface{}
	if projectID != "" {
		query = "SELECT id, name, desc, type, file_url FROM o_assets WHERE project_id = ? ORDER BY id"
		args = []interface{}{projectID}
	} else {
		query = "SELECT id, name, desc, type, file_url FROM o_assets WHERE user_id = ? ORDER BY id"
		args = []interface{}{userID}
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	var assets []map[string]interface{}
	for rows.Next() {
		var id int
		var name, fileType string
		var desc, fileURL sql.NullString
		if err := rows.Scan(&id, &name, &desc, &fileType, &fileURL); err != nil {
			continue
		}
		assets = append(assets, gin.H{
			"id":       id,
			"name":     name,
			"desc":     desc.String,
			"type":     fileType,
			"file_url": fileURL.String,
		})
	}
	if len(assets) == 0 {
		c.JSON(http.StatusOK, []interface{}{})
		return
	}
	c.JSON(http.StatusOK, assets)
}

func (r *Router) assetsCreateHandler(c *gin.Context) {
	var a struct {
		ProjectID string `json:"project_id"`
		Name      string `json:"name" binding:"required"`
		Desc      string `json:"desc"`
		Type      string `json:"type"`
		FileURL   string `json:"file_url"`
	}
	if err := c.ShouldBindJSON(&a); err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}
	if a.Type == "" {
		a.Type = "role"
	}
	userID := currentUserID(c)
	if a.ProjectID == "" || !r.ownsProject(userID, a.ProjectID) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid project"})
		return
	}

	_, err := r.db.Exec(
		"INSERT INTO o_assets (project_id, user_id, name, desc, type, file_url) VALUES (?, ?, ?, ?, ?, ?)",
		a.ProjectID, userID, a.Name, a.Desc, a.Type, strings.TrimSpace(a.FileURL),
	)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	c.Status(http.StatusCreated)
}

func (r *Router) assetUpdateHandler(c *gin.Context) {
	id := c.Param("id")
	userID := currentUserID(c)

	var projectID string
	err := r.db.QueryRow("SELECT project_id FROM o_assets WHERE id = ?", id).Scan(&projectID)
	if err == sql.ErrNoRows {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	if !r.ownsProject(userID, projectID) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	var a struct {
		Name    string `json:"name" binding:"required"`
		Desc    string `json:"desc"`
		Type    string `json:"type"`
		FileURL string `json:"file_url"`
	}
	if err := c.ShouldBindJSON(&a); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if a.Type == "" {
		a.Type = "role"
	}

	res, err := r.db.Exec(
		"UPDATE o_assets SET name = ?, desc = ?, type = ?, file_url = ? WHERE id = ?",
		a.Name, a.Desc, a.Type, strings.TrimSpace(a.FileURL), id,
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

func (r *Router) assetDeleteHandler(c *gin.Context) {
	id := c.Param("id")
	userID := currentUserID(c)

	var projectID string
	err := r.db.QueryRow("SELECT project_id FROM o_assets WHERE id = ?", id).Scan(&projectID)
	if err == sql.ErrNoRows {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	if !r.ownsProject(userID, projectID) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	res, err := r.db.Exec("DELETE FROM o_assets WHERE id = ?", id)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Status(http.StatusOK)
}

func (r *Router) projectAssetsListHandler(c *gin.Context) {
	projectID := c.Param("id")
	userID := currentUserID(c)
	if !r.ownsProject(userID, projectID) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	r.writeAssetsJSON(c, projectID)
}

func (r *Router) queryAssetsForProject(projectID string) []map[string]interface{} {
	rows, err := r.db.Query(
		"SELECT id, name, desc, type, file_url FROM o_assets WHERE project_id = ? ORDER BY id",
		projectID,
	)
	if err != nil {
		return []map[string]interface{}{}
	}
	defer rows.Close()

	var assets []map[string]interface{}
	for rows.Next() {
		var id int
		var name, fileType string
		var desc, fileURL sql.NullString
		if err := rows.Scan(&id, &name, &desc, &fileType, &fileURL); err != nil {
			continue
		}
		assets = append(assets, gin.H{
			"id":       id,
			"name":     name,
			"desc":     desc.String,
			"type":     fileType,
			"file_url": fileURL.String,
		})
	}
	if assets == nil {
		return []map[string]interface{}{}
	}
	return assets
}

func (r *Router) writeAssetsJSON(c *gin.Context, projectID string) {
	assets := r.queryAssetsForProject(projectID)
	if len(assets) == 0 {
		c.JSON(http.StatusOK, []interface{}{})
		return
	}
	c.JSON(http.StatusOK, assets)
}

func (r *Router) assetsExtractHandler(c *gin.Context) {
	projectID := c.Param("id")
	userID := currentUserID(c)
	if !r.ownsProject(userID, projectID) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	var req struct {
		EpisodeID string `json:"episode_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "episode_id required"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()

	v := r.resolveVendor()
	n, err := service.ExtractAssetsFromEpisode(ctx, r.db.DB, v, userID, projectID, req.EpisodeID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"count": n})
}

// ======================== Storyboards ========================

func (r *Router) storyboardsListHandler(c *gin.Context) {
	projectID := c.Query("project_id")
	if projectID == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "project_id required"})
		return
	}
	if !r.ownsProject(currentUserID(c), projectID) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	episodeID := c.Query("episode_id")
	var shotsJSON string
	var err error
	if episodeID != "" {
		sbID := fmt.Sprintf("sb_%s_%s", projectID, episodeID)
		err = r.db.QueryRow("SELECT shots FROM o_storyboard WHERE id = ?", sbID).Scan(&shotsJSON)
	} else {
		err = r.db.QueryRow(
			"SELECT shots FROM o_storyboard WHERE project_id = ? ORDER BY updated_at DESC LIMIT 1",
			projectID,
		).Scan(&shotsJSON)
	}
	if err == sql.ErrNoRows {
		c.JSON(http.StatusOK, []interface{}{})
		return
	}
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	var shots []task.StoryboardItem
	if err := json.Unmarshal([]byte(shotsJSON), &shots); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	shots = service.NormalizeStoryboardItems(shots)
	if episodeID != "" && len(shots) <= 1 {
		if refreshed := service.StoryboardFromRecentChat(r.db.DB, projectID, episodeID, 10); len(refreshed) > len(shots) {
			refreshed = service.MergeStoryboardMedia(shots, refreshed)
			refreshed = service.NormalizeStoryboardItems(refreshed)
			shotsJSON, _ := json.Marshal(refreshed)
			sbID := fmt.Sprintf("sb_%s_%s", projectID, episodeID)
			_, _ = r.db.Exec(`UPDATE o_storyboard SET shots = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, string(shotsJSON), sbID)
			shots = refreshed
		}
	}
	c.JSON(http.StatusOK, shots)
}

// ======================== Styles ========================

func (r *Router) stylesHandler(c *gin.Context) {
	rows, err := r.db.Query("SELECT id, name, label, prompt FROM o_artStyle ORDER BY id")
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	var styles []map[string]interface{}
	for rows.Next() {
		var id, name, label, prompt string
		if err := rows.Scan(&id, &name, &label, &prompt); err != nil {
			continue
		}
		styles = append(styles, gin.H{
			"id":     id,
			"name":   name,
			"label":  label,
			"prompt": prompt,
		})
	}
	c.JSON(http.StatusOK, styles)
}

// ======================== Settings ========================

func (r *Router) settingsGetHandler(c *gin.Context) {
	rows, err := r.db.Query("SELECT key, value FROM o_setting")
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		settings[k] = v
	}
	c.JSON(http.StatusOK, settings)
}

func (r *Router) settingsUpdateHandler(c *gin.Context) {
	var data map[string]string
	if err := c.ShouldBindJSON(&data); err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}
	for k, v := range data {
		r.db.Exec("INSERT OR REPLACE INTO o_setting (key, value) VALUES (?, ?)", k, v)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ======================== Static / Output / Download ========================

func (r *Router) outputHandler(c *gin.Context) {
	path := c.Param("filepath")
	if path == "" {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	fullPath := filepath.Join(r.outputDir, path)
	if _, err := os.Stat(fullPath); err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.File(fullPath)
}

func (r *Router) downloadHandler(c *gin.Context) {
	path := c.Param("filepath")
	if path == "" {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	fullPath := filepath.Join(r.outputDir, path)
	if _, err := os.Stat(fullPath); err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(path)))
	c.File(fullPath)
}

func (r *Router) indexHandler(c *gin.Context) {
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.File(filepath.Join(r.staticDir, "index.html"))
}
