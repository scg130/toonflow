package main

import (
	"flag"
	"os"
	"path/filepath"
	"time"
)

// Config holds the application-wide configuration.
type Config struct {
	// Server
	Port     int
	DBPath   string
	OutputDir string

	// Concurrency
	MaxConcurrentTasks int
	TaskTimeout        time.Duration

	// AI Adapters
	DefaultVendor string

	// FFmpeg
	FFmpegPath string

	// Skills
	SkillsDir string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		Port:               8080,
		DBPath:             filepath.Join(home, ".toonflow", "toonflow.db"),
		OutputDir:          filepath.Join(".", "output"),
		MaxConcurrentTasks: 5,
		TaskTimeout:        10 * time.Minute,
		DefaultVendor:      "openai_compatible",
		FFmpegPath:         "ffmpeg",
		SkillsDir:          filepath.Join(".", "skills"),
	}
}

// Load parses CLI flags and environment variables, returning the resolved Config.
func Load() *Config {
	cfg := DefaultConfig()

	flag.IntVar(&cfg.Port, "port", cfg.Port, "HTTP server port")
	flag.StringVar(&cfg.DBPath, "db", cfg.DBPath, "SQLite database path")
	flag.StringVar(&cfg.OutputDir, "output-dir", cfg.OutputDir, "Output directory for generated files")
	flag.IntVar(&cfg.MaxConcurrentTasks, "max-concurrent", cfg.MaxConcurrentTasks, "Max concurrent generation tasks")
	flag.DurationVar(&cfg.TaskTimeout, "task-timeout", cfg.TaskTimeout, "Per-task timeout duration")
	flag.StringVar(&cfg.FFmpegPath, "ffmpeg", cfg.FFmpegPath, "FFmpeg binary path")
	flag.StringVar(&cfg.SkillsDir, "skills-dir", cfg.SkillsDir, "Skills markdown directory")

	// Environment variable overrides
	if p := os.Getenv("TOONFLOW_PORT"); p != "" {
		flag.CommandLine.Set("port", p)
	}
	if dp := os.Getenv("TOONFLOW_DB"); dp != "" {
		flag.CommandLine.Set("db", dp)
	}

	flag.Parse()

	// Ensure directories exist
	os.MkdirAll(filepath.Dir(cfg.DBPath), 0755)
	os.MkdirAll(cfg.OutputDir, 0755)
	os.MkdirAll(cfg.SkillsDir, 0755)

	return cfg
}
