package api

import (
	"os"

	"toonflow/adapter"
	"toonflow/auth"
	"toonflow/engine"
	"toonflow/skill"
	"toonflow/storage"
	"toonflow/task"
	"toonflow/ws"

	"github.com/gin-gonic/gin"
)

// SetupRouter creates a gin.Engine with all routes registered.
func SetupRouter(
	db *storage.DB,
	queue *task.Queue,
	engineCfg *engine.Config,
	pipeline *engine.Pipeline,
	skillMgr *skill.Manager,
	v adapter.Vendor,
	wsBC *ws.ConnManager,
	sessions *auth.Store,
	outputDir, staticDir string,
	port int,
) *gin.Engine {
	router := NewRouter(db, queue, engineCfg, pipeline, skillMgr, v, wsBC, sessions, outputDir, staticDir, port)
	return router.Setup()
}

// Ensure we have a fallback if static dir doesn't exist.
func init() {
	_ = gin.Mode() // ensure gin is initialized
}

// DefaultStaticDir returns the default static directory path.
func DefaultStaticDir() string {
	if _, err := os.Stat("static"); err == nil {
		return "static"
	}
	return "."
}

// DefaultOutputDir returns the default output directory path.
func DefaultOutputDir() string {
	if _, err := os.Stat("output"); err == nil {
		return "output"
	}
	return "."
}
