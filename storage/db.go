package storage

import (
	"database/sql"
	"fmt"
	"time"

	"toonflow/auth"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps a sql.DB handle with helper methods.
type DB struct {
	*sql.DB
}

// Init opens (or creates) the SQLite database and runs migrations.
func Init(dbPath string) (*DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(1)
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

// migrate creates all tables matching ToonFlow's schema.
func (db *DB) migrate() error {
	// Add columns that may not exist in old databases
	db.Exec(`ALTER TABLE o_artStyle ADD COLUMN label TEXT`)
	db.Exec(`ALTER TABLE o_artStyle ADD COLUMN director_planning TEXT`)
	db.Exec(`ALTER TABLE o_artStyle ADD COLUMN director_storyboard TEXT`)
	db.Exec(`ALTER TABLE o_artStyle ADD COLUMN director_table_style TEXT`)
	db.Exec(`ALTER TABLE o_project ADD COLUMN intro TEXT DEFAULT ''`)
	db.Exec(`ALTER TABLE o_project ADD COLUMN type TEXT DEFAULT ''`)
	db.Exec(`ALTER TABLE o_project ADD COLUMN director_manual TEXT`)
	db.Exec(`ALTER TABLE o_project ADD COLUMN image_model TEXT DEFAULT ''`)
	db.Exec(`ALTER TABLE o_project ADD COLUMN video_model TEXT DEFAULT ''`)
	db.Exec(`ALTER TABLE o_project ADD COLUMN video_ratio TEXT DEFAULT '16:9'`)
	db.Exec(`ALTER TABLE o_project ADD COLUMN mode TEXT DEFAULT '[]'`)
	db.Exec(`ALTER TABLE o_project ADD COLUMN create_time DATETIME DEFAULT CURRENT_TIMESTAMP`)
	db.Exec(`ALTER TABLE o_project ADD COLUMN update_time DATETIME DEFAULT CURRENT_TIMESTAMP`)
	// Create user table
	db.Exec(`CREATE TABLE IF NOT EXISTS o_user (
		id TEXT PRIMARY KEY,
		username TEXT NOT NULL UNIQUE,
		password TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	// Create asset and storyboard tables
	db.Exec(`CREATE TABLE IF NOT EXISTS o_assets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id TEXT NOT NULL,
		user_id TEXT DEFAULT '',
		name TEXT NOT NULL,
		desc TEXT,
		type TEXT NOT NULL,
		file_url TEXT,
		parent_id INTEGER DEFAULT 0,
		derive TEXT DEFAULT '[]',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(project_id, name, type)
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS o_storyboard (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		scene_name TEXT,
		segment_num INTEGER DEFAULT 1,
		assets_ref TEXT DEFAULT '[]',
		shots TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS o_storyboard_panel (
		id TEXT PRIMARY KEY,
		shot_id TEXT NOT NULL,
		shot_number INTEGER,
		video_desc TEXT,
		prompt TEXT,
		track INTEGER DEFAULT 1,
		duration REAL DEFAULT 3.0,
		associate_asset_ids TEXT DEFAULT '[]',
		should_gen_img TEXT DEFAULT 'true',
		status TEXT DEFAULT 'pending',
		image_url TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS o_image (
		id TEXT PRIMARY KEY,
		asset_id INTEGER,
		prompt TEXT,
		file_url TEXT,
		status TEXT DEFAULT 'pending',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS o_video (
		id TEXT PRIMARY KEY,
		project_id TEXT,
		prompt TEXT,
		file_url TEXT,
		duration REAL,
		status TEXT DEFAULT 'pending',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS o_video_track (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		track_num INTEGER,
		video_id TEXT,
		duration REAL,
		prompt TEXT,
		selected INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS o_agent_work (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		episode_id TEXT DEFAULT '',
		work_type TEXT NOT NULL,
		content TEXT NOT NULL DEFAULT '',
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS o_source_text (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		volume TEXT DEFAULT '正文卷',
		chapter_name TEXT DEFAULT '',
		content TEXT NOT NULL,
		events TEXT DEFAULT '',
		sort_num INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS o_episode (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		episode_num INTEGER NOT NULL DEFAULT 1,
		title TEXT NOT NULL DEFAULT '',
		params_json TEXT DEFAULT '{}',
		script_content TEXT DEFAULT '',
		events_ref TEXT DEFAULT '',
		status TEXT DEFAULT 'draft',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS o_chat_message (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		episode_id TEXT DEFAULT '',
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		action_json TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	// Now create the main tables
	schema := `
	-- 供应商配置（对标 ToonFlow o_vendorConfig）
	CREATE TABLE IF NOT EXISTS o_vendorConfig (
		id            TEXT PRIMARY KEY,
		name          TEXT NOT NULL,
		version       TEXT NOT NULL DEFAULT '1.0.0',
		input_values  TEXT NOT NULL DEFAULT '{}',
		models_json   TEXT NOT NULL DEFAULT '[]',
		enable        INTEGER NOT NULL DEFAULT 1,
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- AI 模型配置（对标 ToonFlow t_config）
	CREATE TABLE IF NOT EXISTS t_config (
		key         TEXT PRIMARY KEY,
		value       TEXT NOT NULL DEFAULT '',
		description TEXT,
		updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- 画风定义（对标 ToonFlow o_artStyle）
	CREATE TABLE IF NOT EXISTS o_artStyle (
		id                       INTEGER PRIMARY KEY AUTOINCREMENT,
		name                     TEXT NOT NULL UNIQUE,
		label                    TEXT,
		file_url                 TEXT,
		prompt                   TEXT NOT NULL DEFAULT '',
		director_planning        TEXT,
		director_storyboard      TEXT,
		director_table_style     TEXT,
		created_at               DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- 应用设置
	CREATE TABLE IF NOT EXISTS o_setting (
		key        TEXT PRIMARY KEY,
		value      TEXT NOT NULL DEFAULT '',
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- 项目（对标 ToonFlow o_project）
	CREATE TABLE IF NOT EXISTS o_project (
		id              TEXT PRIMARY KEY,
		user_id         TEXT DEFAULT '',
		name            TEXT NOT NULL DEFAULT '',
		intro           TEXT,
		type            TEXT DEFAULT '',          -- 题材类型
		art_style       TEXT DEFAULT '',          -- 画风名称
		director_manual TEXT,                      -- 导演手记
		image_model     TEXT DEFAULT '',          -- 图片生成模型
		video_model     TEXT DEFAULT '',          -- 视频生成模型
		video_ratio     TEXT DEFAULT '16:9',      -- 画面比例
		mode            TEXT DEFAULT '[]',        -- 视频生成模式 JSON
		status          TEXT DEFAULT 'draft',     -- draft/processing/done/error
		create_time     DATETIME DEFAULT CURRENT_TIMESTAMP,
		update_time     DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- 资产（角色/场景/道具，对标 ToonFlow o_assets）
	CREATE TABLE IF NOT EXISTS o_assets (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id    TEXT NOT NULL,
		user_id       TEXT DEFAULT '',
		name          TEXT NOT NULL,
		desc          TEXT,
		type          TEXT NOT NULL,              -- role/scene/prop
		file_url      TEXT,
		parent_id     INTEGER DEFAULT 0,
		derive        TEXT DEFAULT '[]',          -- 衍生变体 JSON
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(project_id, name, type)
	);

	-- 分镜表（对标 ToonFlow o_agentWorkData.storyboardTable）
	CREATE TABLE IF NOT EXISTS o_storyboard (
		id            TEXT PRIMARY KEY,
		project_id    TEXT NOT NULL,
		scene_name    TEXT,
		segment_num   INTEGER DEFAULT 1,
		assets_ref    TEXT DEFAULT '[]',          -- 引用资产 ID JSON
		shots         TEXT NOT NULL,              -- 分镜表格 JSON array
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- 分镜面板（对标 ToonFlow o_agentWorkData.storyboard）
	CREATE TABLE IF NOT EXISTS o_storyboard_panel (
		id            TEXT PRIMARY KEY,
		shot_id       TEXT NOT NULL,
		shot_number   INTEGER,
		video_desc    TEXT,
		prompt        TEXT,
		track         INTEGER DEFAULT 1,
		duration      REAL DEFAULT 3.0,
		associate_asset_ids TEXT DEFAULT '[]',
		should_gen_img TEXT DEFAULT 'true',
		status        TEXT DEFAULT 'pending',     -- pending/generating/done/error
		image_url     TEXT,
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- 生成的图片
	CREATE TABLE IF NOT EXISTS o_image (
		id            TEXT PRIMARY KEY,
		asset_id      INTEGER,
		prompt        TEXT,
		file_url      TEXT,
		status        TEXT DEFAULT 'pending',
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- 生成的视频
	CREATE TABLE IF NOT EXISTS o_video (
		id            TEXT PRIMARY KEY,
		project_id    TEXT,
		prompt        TEXT,
		file_url      TEXT,
		duration      REAL,
		status        TEXT DEFAULT 'pending',     -- pending/generating/done/error
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- 视频轨道（对标 ToonFlow o_videoTrack）
	CREATE TABLE IF NOT EXISTS o_video_track (
		id            TEXT PRIMARY KEY,
		project_id    TEXT NOT NULL,
		track_num     INTEGER,
		video_id      TEXT,
		duration      REAL,
		prompt        TEXT,
		selected      INTEGER DEFAULT 0,
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- 任务中心（对标 ToonFlow o_tasks）
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

	-- 代理部署配置（对标 ToonFlow o_agentDeploy）
	CREATE TABLE IF NOT EXISTS o_agentDeploy (
		key           TEXT PRIMARY KEY,
		agent_type    TEXT NOT NULL,
		model         TEXT,
		temperature   REAL DEFAULT 0.7,
		max_tokens    INTEGER DEFAULT 4096,
		use_mode      TEXT DEFAULT 'auto',
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- 种子默认画风
	INSERT OR IGNORE INTO o_artStyle (name, label, prompt) VALUES
		('2D_90s_japanese_anime', '90年代日式动画', '复古90年代日本动画风格，手绘平涂上色，色彩饱和度适中，线条清晰，背景具有胶片质感。'),
		('2D_chinese_guofeng', '中国古风', '中国传统水墨画风格，淡雅配色，留白意境，线条流畅如书法，山水人物具有国画韵味。'),
		('2D_flat_design', '扁平设计', '现代扁平化设计风格，几何图形构成，纯色块填充，简洁明快，适合信息传达。'),
		('2D_mature_urban_romance', '成熟都市浪漫', '都市成熟画风，细腻光影，暖色调，人物表情丰富，场景具有电影感。'),
		('3D_anime_render', '3D动漫渲染', '3D动漫渲染风格，类似吉卜力工作室质感，立体光影，细腻材质，色彩明亮温暖。'),
		('3D_chinese_traditional', '3D中国风', '3D渲染结合中国传统美学，青绿山水配色，建筑具有中式特色，人物造型融合古装元素。'),
		('3D_clay_stopmotion', '黏土定格', '黏土定格动画风格，材质具有黏土颗粒感，边缘圆润柔和，色彩饱和度高，充满童趣。'),
		('3D_guofeng_cyber', '国风赛博朋克', '中国传统元素与赛博朋克融合，霓虹灯光效，古风建筑结合未来科技，暗色调高对比度。'),
		('realpeople_ancient_chinese', '真人古装', '真人实拍古装风格，服饰考究，妆容精致，场景具有历史感，光影自然柔和。'),
		('realpeople_modern_city', '真人现代都市', '真人实拍现代都市风格，自然光效，真实质感，场景为现代城市环境。'),
		('realpeople_urban_modern', '真人都市现代', '真人实拍都市现代风格，时尚穿搭，精致妆容，场景为现代化都市生活空间。');

	-- 种子默认设置
	INSERT OR IGNORE INTO o_setting (key, value) VALUES
		('output_dir', 'output'),
		('default_fps', '24'),
		('default_resolution', '1280x720'),
		('default_frame_duration', '3.0'),
		('ffmpeg_path', 'ffmpeg'),
		('max_concurrent_tasks', '5'),
		('task_timeout', '600');

	-- 种子代理配置
	INSERT OR IGNORE INTO o_agentDeploy (key, agent_type, model, temperature, max_tokens) VALUES
		('script_agent', 'script', 'agnes-2.0-flash', 0.7, 8000),
		('production_agent', 'production', 'agnes-2.0-flash', 0.7, 8000),
		('director_agent', 'director', 'agnes-2.0-flash', 0.7, 4096),
		('supervision_agent', 'supervision', 'agnes-2.0-flash', 0.3, 1000);
	`
	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	if err := auth.SeedAdmin(db.DB); err != nil {
		return fmt.Errorf("seed admin: %w", err)
	}
	// Backfill user_id columns on databases created before auth support
	db.Exec(`ALTER TABLE o_project ADD COLUMN user_id TEXT DEFAULT ''`)
	db.Exec(`ALTER TABLE o_assets ADD COLUMN user_id TEXT DEFAULT ''`)
	// Bind legacy rows without owner to default admin
	db.Exec(`UPDATE o_project SET user_id = ? WHERE user_id IS NULL OR user_id = ''`, auth.DefaultAdminID)
	db.Exec(`UPDATE o_assets SET user_id = ? WHERE user_id IS NULL OR user_id = ''`, auth.DefaultAdminID)
	return nil
}
