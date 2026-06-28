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
	"toonflow/engine"
	"toonflow/skill"
	"toonflow/storage"
	"toonflow/task"
	"toonflow/ws"
)

func main() {
	// Initialize logger
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("ToonFlow starting...")

	cfg := Load()

	// Initialize database
	db, err := storage.Init(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to init database: %v", err)
	}
	defer db.Close()
	log.Println("Database initialized")

	// Initialize skill manager
	skillMgr := skill.NewManager(cfg.SkillsDir)
	if err := skillMgr.Load(); err != nil {
		log.Printf("Warning: failed to load skills: %v", err)
	}
	log.Printf("Skills loaded from %s", cfg.SkillsDir)

	// Get default adapter
	v, ok := adapter.Get(cfg.DefaultVendor)
	if !ok {
		log.Fatalf("Unknown default vendor: %s", cfg.DefaultVendor)
	}
	log.Printf("Using adapter: %s", cfg.DefaultVendor)

	// Initialize WebSocket manager
	broadcaster := ws.NewConnManager()
	broadcaster.Run()

	// Initialize pipeline with broadcaster
	pipelineCfg := &engine.Config{
		OutputDir:   cfg.OutputDir,
		TaskTimeout: cfg.TaskTimeout,
	}
	_ = engine.New(v, skillMgr, pipelineCfg, broadcaster)

	// Initialize task queue
	queue := task.NewQueue(cfg.MaxConcurrentTasks)

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
		skillMgr,
		v,
		broadcaster,
		cfg.OutputDir,
		staticDir,
		cfg.Port,
	)

	addr := ":" + itoa(cfg.Port)
	log.Printf("Listening on http://localhost%s", addr)

	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
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
