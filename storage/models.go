package storage

import "time"

// VendorConfig represents the o_vendorConfig table.
type VendorConfig struct {
	ID          string    `db:"id" json:"id"`
	Name        string    `db:"name" json:"name"`
	Version     string    `db:"version" json:"version"`
	InputValues string    `db:"input_values" json:"input_values"`
	ModelsJSON  string    `db:"models_json" json:"models_json"`
	Enable      int       `db:"enable" json:"enable"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
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
	ID                 int       `db:"id" json:"id"`
	Name               string    `db:"name" json:"name"`
	Label              string    `db:"label" json:"label,omitempty"`
	FileURL            string    `db:"file_url" json:"file_url,omitempty"`
	Prompt             string    `db:"prompt" json:"prompt"`
	DirectorPlanning   string    `db:"director_planning" json:"director_planning,omitempty"`
	DirectorStoryboard string    `db:"director_storyboard" json:"director_storyboard,omitempty"`
	DirectorTableStyle string    `db:"director_table_style" json:"director_table_style,omitempty"`
	CreatedAt          time.Time `db:"created_at" json:"created_at"`
}

// Setting represents the o_setting table.
type Setting struct {
	Key     string    `db:"key" json:"key"`
	Value   string    `db:"value" json:"value"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// Project represents the o_project table.
type Project struct {
	ID            string    `db:"id" json:"id"`
	Name          string    `db:"name" json:"name"`
	Intro         string    `db:"intro" json:"intro,omitempty"`
	Type          string    `db:"type" json:"type,omitempty"`
	ArtStyle      string    `db:"art_style" json:"art_style,omitempty"`
	DirectorManual string   `db:"director_manual" json:"director_manual,omitempty"`
	ImageModel    string    `db:"image_model" json:"image_model,omitempty"`
	VideoModel    string    `db:"video_model" json:"video_model,omitempty"`
	VideoRatio    string    `db:"video_ratio" json:"video_ratio"`
	Mode          string    `db:"mode" json:"mode,omitempty"`
	Status        string    `db:"status" json:"status"`
	CreateTime    time.Time `db:"create_time" json:"create_time"`
	UpdateTime    time.Time `db:"update_time" json:"update_time"`
}

// Asset represents the o_assets table.
type Asset struct {
	ID         int       `db:"id" json:"id"`
	ProjectID  string    `db:"project_id" json:"project_id"`
	Name       string    `db:"name" json:"name"`
	Desc       string    `db:"desc" json:"desc,omitempty"`
	Type       string    `db:"type" json:"type"` // role, scene, prop
	FileURL    string    `db:"file_url" json:"file_url,omitempty"`
	ParentID   int       `db:"parent_id" json:"parent_id"`
	Derive     string    `db:"derive" json:"derive,omitempty"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
}

// Storyboard represents the o_storyboard table.
type Storyboard struct {
	ID         string    `db:"id" json:"id"`
	ProjectID  string    `db:"project_id" json:"project_id"`
	SceneName  string    `db:"scene_name" json:"scene_name,omitempty"`
	SegmentNum int       `db:"segment_num" json:"segment_num"`
	AssetsRef  string    `db:"assets_ref" json:"assets_ref,omitempty"`
	Shots      string    `db:"shots" json:"shots"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
	UpdatedAt  time.Time `db:"updated_at" json:"updated_at"`
}

// StoryboardPanel represents the o_storyboard_panel table.
type StoryboardPanel struct {
	ID                 string    `db:"id" json:"id"`
	ShotID             string    `db:"shot_id" json:"shot_id"`
	ShotNumber         int       `db:"shot_number" json:"shot_number"`
	VideoDesc          string    `db:"video_desc" json:"video_desc,omitempty"`
	Prompt             string    `db:"prompt" json:"prompt,omitempty"`
	Track              int       `db:"track" json:"track"`
	Duration           float64   `db:"duration" json:"duration"`
	AssociateAssetIDs  string    `db:"associate_asset_ids" json:"associate_asset_ids,omitempty"`
	ShouldGenImg       string    `db:"should_gen_img" json:"should_gen_img"`
	Status             string    `db:"status" json:"status"`
	ImageURL           string    `db:"image_url" json:"image_url,omitempty"`
	CreatedAt          time.Time `db:"created_at" json:"created_at"`
	UpdatedAt          time.Time `db:"updated_at" json:"updated_at"`
}

// Image represents the o_image table.
type Image struct {
	ID       string    `db:"id" json:"id"`
	AssetID  int       `db:"asset_id" json:"asset_id,omitempty"`
	Prompt   string    `db:"prompt" json:"prompt,omitempty"`
	FileURL  string    `db:"file_url" json:"file_url,omitempty"`
	Status   string    `db:"status" json:"status"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// Video represents the o_video table.
type Video struct {
	ID        string    `db:"id" json:"id"`
	ProjectID string    `db:"project_id" json:"project_id,omitempty"`
	Prompt    string    `db:"prompt" json:"prompt,omitempty"`
	FileURL   string    `db:"file_url" json:"file_url,omitempty"`
	Duration  float64   `db:"duration" json:"duration"`
	Status    string    `db:"status" json:"status"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// VideoTrack represents the o_video_track table.
type VideoTrack struct {
	ID        string    `db:"id" json:"id"`
	ProjectID string    `db:"project_id" json:"project_id"`
	TrackNum  int       `db:"track_num" json:"track_num"`
	VideoID   string    `db:"video_id" json:"video_id,omitempty"`
	Duration  float64   `db:"duration" json:"duration"`
	Prompt    string    `db:"prompt" json:"prompt,omitempty"`
	Selected  int       `db:"selected" json:"selected"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
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

// AgentDeploy represents the o_agentDeploy table.
type AgentDeploy struct {
	Key         string    `db:"key" json:"key"`
	AgentType   string    `db:"agent_type" json:"agent_type"`
	Model       string    `db:"model" json:"model,omitempty"`
	Temperature float64   `db:"temperature" json:"temperature"`
	MaxTokens   int       `db:"max_tokens" json:"max_tokens"`
	UseMode     string    `db:"use_mode" json:"use_mode"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
}
