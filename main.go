package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"toonflow/adapter"
	"toonflow/api"
	"toonflow/auth"
	"toonflow/engine"
	"toonflow/skill"
	"toonflow/storage"
	"toonflow/task"
	"toonflow/ws"
	"toonflow/config"
	"toonflow/logger"
)

func main() {
	// Initialize logger
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("ToonFlow starting...")

	cfg := config.Load()

	if err := logger.Init(cfg.LogDir); err != nil {
		log.Fatalf("Failed to init logger: %v", err)
	}
	logger.Default.Info("system", "ToonFlow starting log_dir="+cfg.LogDir)

	// Initialize database
	db, err := storage.Init(cfg.DBPath)
	if err != nil {
		logger.Default.Error("system", "database init failed", err)
		log.Fatalf("Failed to init database: %v", err)
	}
	defer db.Close()
	logger.Default.Info("system", "Database initialized")

	// Initialize skill manager
	skillMgr := skill.NewManager(cfg.SkillsDir)
	if err := skillMgr.Load(); err != nil {
		logger.Default.Error("system", "failed to load skills", err)
		log.Printf("Warning: failed to load skills: %v", err)
	}
	logger.Default.Info("system", "Skills loaded from "+cfg.SkillsDir)

	// Resolve AI vendor from DB / environment
	v := adapter.ResolveFromDB(db.DB, cfg.DefaultVendor)
	logger.Default.Info("system", "Using adapter: "+v.VendorConfig().Name)

	// Session store
	sessions := auth.NewStore(24 * time.Hour)

	// Initialize WebSocket manager
	broadcaster := ws.NewConnManager()
	broadcaster.Run()

	// Initialize task queue
	queue := task.NewQueue(cfg.MaxConcurrentTasks)

	// Initialize pipeline with broadcaster
	pipelineCfg := &engine.Config{
		OutputDir:   cfg.OutputDir,
		TaskTimeout: cfg.TaskTimeout,
	}
	pipeline := engine.New(v, skillMgr, pipelineCfg, broadcaster, db.DB)

	// Wire WebSocket generation handler
	genSvc := ws.NewGenerationService(pipeline, queue, db.DB, cfg.OutputDir, cfg.TaskTimeout)
	broadcaster.SetGenerationService(genSvc)
	wfSvc := ws.NewWorkflowService(db.DB, cfg.DefaultVendor, skillMgr, cfg.TaskTimeout)
	wfSvc.SetTaskRunner(queue, pipeline, cfg.OutputDir)
	broadcaster.SetWorkflowService(wfSvc)

	// Create directories
	os.MkdirAll(cfg.OutputDir, 0755)

	// Determine static directory
	staticDir := "static"
	if _, err := os.Stat(staticDir); err != nil {
		staticDir = "."
	}

	// Setup Gin router
	router := api.SetupRouter(
		db,
		queue,
		pipelineCfg,
		pipeline,
		skillMgr,
		v,
		broadcaster,
		sessions,
		cfg.OutputDir,
		staticDir,
		cfg.Port,
	)

	addr := ":" + itoa(cfg.Port)
	logger.Default.Info("system", "Listening on http://localhost"+addr)
	log.Printf("Listening on http://localhost%s", addr)

	server := &http.Server{
		Addr:    addr,
		Handler: router,
		// AI 分析/分集等接口可能耗时数分钟
		ReadTimeout:  15 * time.Minute,
		WriteTimeout: 15 * time.Minute,
	}

	// Graceful shutdown
	go func() {
		sigch := make(chan os.Signal, 1)
		signal.Notify(sigch, syscall.SIGINT, syscall.SIGTERM)
		<-sigch
		log.Println("Shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
	log.Println("Server stopped")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
