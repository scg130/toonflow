package storage

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps a sql.DB handle with helper methods.
type DB struct {
	*sql.DB
}

// Init opens (or creates) the SQLite database and runs migrations.
func Init(dbPath string) (*DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Connection pool settings
	db.SetMaxOpenConns(1) // SQLite recommends single writer
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	s := &DB{db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

// migrate creates all tables if they don't exist.
func (db *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS o_vendorConfig (
		id            TEXT PRIMARY KEY,
		name          TEXT NOT NULL,
		version       TEXT NOT NULL DEFAULT '1.0.0',
		config_json   TEXT NOT NULL DEFAULT '{}',
		models_json   TEXT NOT NULL DEFAULT '[]',
		enable        INTEGER NOT NULL DEFAULT 1,
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS t_config (
		key         TEXT PRIMARY KEY,
		value       TEXT NOT NULL DEFAULT '',
		description TEXT,
		updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS o_artStyle (
		id                       INTEGER PRIMARY KEY AUTOINCREMENT,
		name                     TEXT NOT NULL UNIQUE,
		description              TEXT,
		prompt_template          TEXT NOT NULL DEFAULT '',
		negative_prompt_template TEXT,
		created_at               DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS o_setting (
		key        TEXT PRIMARY KEY,
		value      TEXT NOT NULL DEFAULT '',
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS o_project (
		id             TEXT PRIMARY KEY,
		title          TEXT NOT NULL DEFAULT '',
		script         TEXT NOT NULL DEFAULT '',
		style_id       INTEGER REFERENCES o_artStyle(id),
		resolution     TEXT DEFAULT '1280x720',
		fps            INTEGER DEFAULT 24,
		frame_duration REAL DEFAULT 3.0,
		status         TEXT DEFAULT 'draft',
		created_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at     DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS o_tasks (
		id            TEXT PRIMARY KEY,
		project_id    TEXT REFERENCES o_project(id),
		step          TEXT NOT NULL DEFAULT 'waiting',
		state         TEXT NOT NULL DEFAULT 'waiting',
		progress      REAL DEFAULT 0.0,
		error_message TEXT,
		retry_count   INTEGER DEFAULT 0,
		max_retries   INTEGER DEFAULT 2,
		task_data     TEXT,
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Seed default settings
	INSERT OR IGNORE INTO o_setting (key, value) VALUES ('output_dir', 'output');
	INSERT OR IGNORE INTO o_setting (key, value) VALUES ('default_fps', '24');
	INSERT OR IGNORE INTO o_setting (key, value) VALUES ('default_resolution', '1280x720');
	INSERT OR IGNORE INTO o_setting (key, value) VALUES ('default_frame_duration', '3.0');
	INSERT OR IGNORE INTO o_setting (key, value) VALUES ('ffmpeg_path', 'ffmpeg');
	`
	_, err := db.Exec(schema)
	return err
}
