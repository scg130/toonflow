package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"toonflow/adapter"
	"toonflow/engine"
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
	skillMgr      *skill.Manager
	adapter       adapter.Vendor
	wsBroadcaster *ws.ConnManager
	outputDir     string
	staticDir     string
	port          int
}

// NewRouter creates a new Router with the given dependencies.
func NewRouter(db *storage.DB, queue *task.Queue, engineCfg *engine.Config, skillMgr *skill.Manager, v adapter.Vendor, wsBC *ws.ConnManager, outputDir, staticDir string, port int) *Router {
	return &Router{
		db:            db,
		queue:         queue,
		cfg:           engineCfg,
		skillMgr:      skillMgr,
		adapter:       v,
		wsBroadcaster: wsBC,
		outputDir:     outputDir,
		staticDir:     staticDir,
		port:          port,
	}
}

// Setup registers all routes and returns a configured *gin.Engine.
func (r *Router) Setup() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.Default()

	// --- Static files ---
	if _, err := os.Stat(r.staticDir); err == nil {
		engine.StaticFS("/static", http.Dir(r.staticDir))
	}

	// --- Output files (generated images/videos) ---
	engine.GET("/output/*filepath", r.outputHandler)

	// --- Download ---
	engine.GET("/download/*filepath", r.downloadHandler)

	// --- WebSocket ---
	engine.GET("/ws", r.wsHandler)

	// --- API routes ---
	api := engine.Group("/api")
	{
		api.GET("/health", r.healthHandler)
		api.GET("/vendors", r.vendorsHandler)
		api.GET("/tasks", r.tasksHandler)

		// Projects
		api.GET("/projects", r.projectsListHandler)
		api.POST("/projects", r.projectsCreateHandler)
		api.GET("/projects/:id", r.projectDetailHandler)

		// Assets
		api.GET("/assets", r.assetsListHandler)
		api.POST("/assets", r.assetsCreateHandler)
		api.DELETE("/assets/:id", r.assetDeleteHandler)

		// Styles
		api.GET("/styles", r.stylesHandler)

		// Settings
		api.GET("/settings", r.settingsGetHandler)
		api.PUT("/settings", r.settingsUpdateHandler)
	}

	// --- Root: serve index.html ---
	engine.GET("/", r.indexHandler)

	return engine
}

// ======================== WebSocket ========================

func (r *Router) wsHandler(c *gin.Context) {
	r.wsBroadcaster.ServeHTTP(c.Writer, c.Request)
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

func (r *Router) vendorsHandler(c *gin.Context) {
	c.JSON(http.StatusOK, adapter.List())
}

// ======================== Tasks ========================

func (r *Router) tasksHandler(c *gin.Context) {
	c.JSON(http.StatusOK, r.queue.AllTasks())
}

// ======================== Projects ========================

func (r *Router) projectsListHandler(c *gin.Context) {
	rows, err := r.db.Query("SELECT id, name, intro, type, art_style, video_ratio, status, create_time FROM o_project ORDER BY create_time DESC")
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
	_, err := r.db.Exec(
		"INSERT INTO o_project (id, name, intro, type, art_style, video_ratio, image_model, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, proj.Name, proj.Intro, proj.Type, proj.ArtStyle, proj.VideoRatio, proj.ImageModel, proj.Status,
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

	var projName, intro, vType, artStyle, vRatio, imgModel, vidModel, mode, status, createTime string
	err := r.db.QueryRow(
		"SELECT name, intro, type, art_style, video_ratio, image_model, video_model, mode, status, create_time FROM o_project WHERE id = ?",
		id,
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
	})
}

// ======================== Assets ========================

func (r *Router) assetsListHandler(c *gin.Context) {
	projectId := c.Query("project_id")

	query := "SELECT id, name, desc, type, file_url FROM o_assets ORDER BY id"
	args := []interface{}{}
	if projectId != "" {
		query = "SELECT id, name, desc, type, file_url FROM o_assets WHERE project_id = ? ORDER BY id"
		args = append(args, projectId)
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	var assets []map[string]interface{}
	for rows.Next() {
		var id, name, desc, fileType, fileURL string
		if err := rows.Scan(&id, &name, &desc, &fileType, &fileURL); err != nil {
			continue
		}
		assets = append(assets, gin.H{
			"id":       id,
			"name":     name,
			"desc":     desc,
			"type":     fileType,
			"file_url": fileURL,
		})
	}
	c.JSON(http.StatusOK, assets)
}

func (r *Router) assetsCreateHandler(c *gin.Context) {
	var a struct {
		ProjectID string `json:"project_id"`
		Name      string `json:"name" binding:"required"`
		Desc      string `json:"desc"`
		Type      string `json:"type"`
	}
	if err := c.ShouldBindJSON(&a); err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}
	if a.Type == "" {
		a.Type = "role"
	}

	_, err := r.db.Exec("INSERT INTO o_assets (project_id, name, desc, type) VALUES (?, ?, ?, ?)",
		a.ProjectID, a.Name, a.Desc, a.Type)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	c.Status(http.StatusCreated)
}

func (r *Router) assetDeleteHandler(c *gin.Context) {
	id := c.Param("id")
	r.db.Exec("DELETE FROM o_assets WHERE id = ?", id)
	c.Status(http.StatusOK)
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
	c.File(filepath.Join(r.staticDir, "index.html"))
}
