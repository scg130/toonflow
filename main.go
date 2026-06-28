package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	"toonflow/adapter"
	"toonflow/engine"
	"toonflow/skill"
	"toonflow/storage"
	"toonflow/task"
	"toonflow/ws"
)

func main() {
	cfg := Load()

	// Initialize logger
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)
	log.Printf("ToonFlow starting on port %d", cfg.Port)

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
	pipeline := engine.New(v, skillMgr, pipelineCfg, broadcaster)
	_ = pipeline // used when task queue submits work

	// Initialize task queue
	queue := task.NewQueue(cfg.MaxConcurrentTasks)

	// Create output directory
	os.MkdirAll(cfg.OutputDir, 0755)

	// --- HTTP Routes ---

	// Serve frontend
	staticDir := filepath.Join(".", "static")
	if _, err := os.Stat(staticDir); err == nil {
		http.Handle("/static/", http.StripPrefix("/static/",
			http.FileServer(http.Dir(staticDir))))
	}

	// Serve output files
	http.HandleFunc("/output/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path[len("/output/"):]
		fullPath := filepath.Join(cfg.OutputDir, path)
		if _, err := os.Stat(fullPath); err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, fullPath)
	})

	// Download endpoint
	http.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		filename := r.URL.Path[len("/download/"):]
		fullPath := filepath.Join(cfg.OutputDir, filename)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		http.ServeFile(w, r, fullPath)
	})

	// REST API: list vendors
	http.HandleFunc("/api/vendors", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(adapter.List())
	})

	// REST API: list tasks
	http.HandleFunc("/api/tasks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(queue.AllTasks())
	})

	// REST API: health check
	http.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":         "ok",
			"active_tasks":   queue.ActiveCount(),
			"max_concurrent": cfg.MaxConcurrentTasks,
		})
	})

	// WebSocket endpoint
	http.Handle("/ws", broadcaster)

	// Start server
	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("Server listening on http://localhost%s", addr)
	log.Printf("Open http://localhost%s in your browser", addr)

	server := &http.Server{
		Addr:         addr,
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
