package storage

import "time"

// VendorConfig represents the o_vendorConfig table.
type VendorConfig struct {
	ID         string    `db:"id" json:"id"`
	Name       string    `db:"name" json:"name"`
	Version    string    `db:"version" json:"version"`
	ConfigJSON string    `db:"config_json" json:"config_json"`
	ModelsJSON string    `db:"models_json" json:"models_json"`
	Enable     int       `db:"enable" json:"enable"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
	UpdatedAt  time.Time `db:"updated_at" json:"updated_at"`
}

// AppConfig represents the t_config table.
type AppConfig struct {
	Key         string    `db:"key" json:"key"`
	Value       string    `db:"value" json:"value"`
	Description string    `db:"description" json:"description,omitempty"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
}

// ArtStyle represents the o_artStyle table.
type ArtStyle struct {
	ID                   int       `db:"id" json:"id"`
	Name                 string    `db:"name" json:"name"`
	Description          string    `db:"description" json:"description,omitempty"`
	PromptTemplate       string    `db:"prompt_template" json:"prompt_template"`
	NegativePromptTemplate string  `db:"negative_prompt_template" json:"negative_prompt_template,omitempty"`
	CreatedAt            time.Time `db:"created_at" json:"created_at"`
}

// Setting represents the o_setting table.
type Setting struct {
	Key     string    `db:"key" json:"key"`
	Value   string    `db:"value" json:"value"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// Project represents the o_project table.
type Project struct {
	ID             string    `db:"id" json:"id"`
	Title          string    `db:"title" json:"title"`
	Script         string    `db:"script" json:"script"`
	StyleID        int       `db:"style_id" json:"style_id,omitempty"`
	Resolution     string    `db:"resolution" json:"resolution"`
	FPS            int       `db:"fps" json:"fps"`
	FrameDuration  float64   `db:"frame_duration" json:"frame_duration"`
	Status         string    `db:"status" json:"status"`
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time `db:"updated_at" json:"updated_at"`
}

// Task represents the o_tasks table.
type Task struct {
	ID           string `db:"id" json:"id"`
	ProjectID    string `db:"project_id" json:"project_id,omitempty"`
	Step         string `db:"step" json:"step"`
	State        string `db:"state" json:"state"`
	Progress     float64 `db:"progress" json:"progress"`
	ErrorMessage string `db:"error_message" json:"error_message,omitempty"`
	RetryCount   int    `db:"retry_count" json:"retry_count"`
	MaxRetries   int    `db:"max_retries" json:"max_retries"`
	TaskData     string `db:"task_data" json:"task_data,omitempty"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
}
