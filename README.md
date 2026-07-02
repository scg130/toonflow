# ToonFlow — 短剧 AI 生成工具

基于 Go 的短剧 AI 创作工作台：从原文导入、策划分集、分镜出图、单镜图生视频，到时间线剪辑、旁白配音与成片导出。

## 功能特性

### 项目管理
- 多项目 / 多集管理，支持原文导入与事件分析、AI 分集
- 策划面板：骨架、改编策略、剧本生成（Agent + Skill）
- 项目级画风、画幅、模型配置

### 分镜与资产
- LLM 自动生成分镜（镜头描述、运镜、绘图 Prompt）
- 从剧本提取角色 / 场景资产，生图时保持风格与角色一致性
- 按镜批量或单镜生成图片（WebSocket 任务队列）

### 视频与剪辑
- 单镜图生视频（I2V），支持多版本选版与删除
- 时间线编辑器：载入分镜视频、裁剪入出点、多轨音频混音
- 旁白方案 AI 生成 + Edge 神经语音 TTS + 导出带旁白成片

### 交互与集成
- 聊天助手：自然语言驱动工作流（带意图识别与防误触）
- 快捷按钮经 WebSocket `run_workflow` 触发；任务完成实时刷新分镜/视频
- 可插拔 AI 适配器（默认 Agnes-AI：文本 / 图像 / 视频）
- Markdown Skill 模板驱动 Prompt 质量（运镜、一致性、合规等）

## 技术栈

| 层级 | 技术 |
|------|------|
| 后端 | Go 1.25+、Gin |
| 数据库 | SQLite（`github.com/mattn/go-sqlite3`） |
| 实时通信 | gorilla/websocket |
| 视频 / 音频 | FFmpeg |
| 旁白 TTS | Microsoft Edge 神经语音（`bytectlgo/edge-tts`） |
| 前端 | 原生 HTML + CSS + JS（静态资源 `static/`） |

## 快速开始

### 前置条件

- Go 1.25+
- FFmpeg（时间线导出、旁白混音、Ken Burns 兜底片段）
- Agnes-AI API Key（[platform.agnes-ai.com](https://platform.agnes-ai.com/)），或在应用内「设置 → 供应商」配置

### 安装与运行

```bash
go mod tidy
go run main.go
```

默认监听 **http://localhost:9090**。首次启动自动创建默认账号：

| 用户名 | 密码 |
|--------|------|
| `admin` | `admin` |

### 常用启动参数

```bash
go run main.go --port 9090
go run main.go --db ~/.toonflow/toonflow.db
go run main.go --output-dir ./output
go run main.go --skills-dir ./skills
go run main.go --log-dir ./logs
go run main.go --max-concurrent 5
go run main.go --task-timeout 10m
```

### 环境变量

| 变量 | 说明 |
|------|------|
| `AGNES_AI_API_KEY` | Agnes API 密钥（优先于数据库供应商配置） |
| `AGNES_AI_BASE_URL` | API 地址，默认 `https://apihub.agnes-ai.com/v1` |
| `TOONFLOW_PORT` | 服务端口 |
| `TOONFLOW_DB` | 数据库路径 |

### Docker

```bash
docker build -t toonflow .
docker run -p 8080:8080 -e AGNES_AI_API_KEY=your_key toonflow
```

镜像内默认端口为 **8080**（与本地 `go run` 默认 9090 不同）。

## 典型工作流

```
导入原文 → 事件分析 / AI 分集
    → 策划（骨架 / 策略 / 剧本）
    → 生成分镜 → 提取资产
    → 批量生图（按镜）
    → 按镜图生视频（可多版本）
    → 视频剪辑：载入时间线 → 生成旁白 → 合成配音 → 导出成片
```

推荐顺序：**分镜 → 提取资产 → 生图 → 生视频 → 剪辑导出**。聊天输入需明确执行意图；面板按钮走 WebSocket 工作流。

## 项目结构

```
toonflow/
├── main.go                    # 入口：配置、DB、Pipeline、WS、HTTP
├── config/                    # CLI / 环境变量配置
├── api/                       # Gin 路由与 HTTP Handler
│   ├── handler.go             # 项目、分镜、任务、设置等 REST API
│   ├── auth_handler.go        # 登录 / 会话
│   ├── workflow_handler.go    # 策划、聊天、流式回复
│   ├── video_handler.go       # 分镜视频、时间线
│   ├── narration_handler.go   # 旁白方案与 TTS 合成
│   ├── generate_handler.go    # 批量生图 HTTP（备用）
│   └── models_handler.go      # 模型连通性测试
├── adapter/                   # AI 供应商适配器
│   ├── adapter.go             # Vendor 接口与注册表
│   ├── agnes_ai.go            # Agnes-AI（文本 / 图 / 视频）
│   ├── openai_compatible.go   # OpenAI 兼容（文本 / 图）
│   ├── resolve.go             # 从 DB / 环境变量解析供应商
│   ├── speech.go / edge_tts.go # 旁白 TTS（Edge 神经语音）
│   └── image_publish.go       # 本地图上传 CDN 供 I2V 使用
├── auth/                      # 会话与用户
├── storage/                   # SQLite 初始化与模型
├── task/                      # 任务定义与并发队列
├── engine/                    # 生成流水线（parse / images / video / full）
├── service/                   # 业务逻辑
│   ├── workflow.go            # 策划类工作流
│   ├── agent_chat.go          # 聊天 Agent 与 ACTION 解析
│   ├── chat_actions.go        # 聊天动作白名单与防误触
│   ├── storyboard_store.go    # 分镜持久化
│   ├── shot_clip.go           # 单镜视频片段
│   ├── timeline.go            # 时间线编辑与 FFmpeg 导出
│   ├── narration.go           # 旁白方案、TTS、时间轴对齐
│   └── image_prompt_sanitize.go
├── skill/                     # Skill Markdown 加载
├── skills/                    # Prompt 模板（art / story / production）
├── ws/                        # WebSocket
│   ├── conn.go                # 连接管理、消息分发
│   ├── generation.go          # start_generate（流水线任务）
│   └── workflow.go            # run_workflow（策划 / 生图 / 生视频等）
├── logger/                    # 分级日志（.log / .trace / .error）
└── static/                    # 前端 SPA
    ├── index.html
    ├── css/style.css
    └── js/app.js
```

## WebSocket

连接：`ws://host:port/ws?token=<session_token>`（登录后由前端携带会话 token）。

### 客户端 Action

| action | 说明 |
|--------|------|
| `ping` | 心跳 |
| `start_generate` | 提交流水线任务（`mode`: `full` / `parse` / `images` / `video`） |
| `run_workflow` | 触发工作流步骤（见下表） |

### `run_workflow` 支持的 `workflow_action`

| workflow_action | 说明 |
|-----------------|------|
| `analyze_events` | 原文事件分析 |
| `split_episodes` | AI 分集 |
| `generate_skeleton` / `generate_strategy` / `generate_script` | 策划三件套 |
| `generate_storyboard` | 生成分镜 |
| `extract_assets` | 提取资产 |
| `generate_shot_image` / `batch_generate_shot_images` | 单镜 / 批量生图 |
| `generate_shot_video` / `batch_generate_shot_videos` | 单镜 / 批量图生视频 |
| `delete_shot_clip` | 删除视频版本 |

服务端推送 `task_update`、`workflow_done`、`chat_progress`、`chat_stream` 等步骤；任务 `done` 时前端自动刷新分镜与视频列表。

## REST API 概览

除 `GET /api/health`、`POST /api/login` 外，其余 `/api/*` 需登录（Cookie / Bearer）。

| 分类 | 主要路径 |
|------|----------|
| 认证 | `GET /api/me`, `POST /api/logout` |
| 供应商 | `GET/POST/PATCH/DELETE /api/vendors` |
| 任务 | `GET /api/tasks` |
| 项目 | `GET/POST/PUT/DELETE /api/projects` |
| 原文 / 分集 | `/api/projects/:id/source-texts`, `/episodes` |
| 策划 / 聊天 | `/api/projects/:id/agent-work`, `/chat` |
| 资产 | `/api/projects/:id/assets`, `/api/assets` |
| 分镜 | `GET /api/storyboards` |
| 分镜视频 | `/api/projects/:id/shot-clips`, `/shot-clips/:id` |
| 时间线 | `GET/PUT /api/projects/:id/timeline`, `POST .../timeline/export` |
| 旁白 | `POST /api/projects/:id/narration/plan`, `.../synthesize` |
| 设置 / 测试 | `GET/PUT /api/settings`, `POST /api/models/test/*` |
| 静态输出 | `GET /output/*`, `GET /download/*` |

## 扩展 AI 适配器

在 `adapter/` 下实现 `Vendor` 接口并在 `init()` 中 `Register`：

```go
type Vendor interface {
    VendorConfig() VendorConfig
    TextRequest(ctx interface{}, model string, params TextParams) (*TextResponse, error)
    ImageRequest(ctx interface{}, model string, params ImageParams) (*ImageResponse, error)
    VideoRequest(ctx interface{}, model string, params VideoParams) (*VideoResponse, error)
    TTSRequest(ctx interface{}, model string, params TTSParams) (*TTSResponse, error)
}
```

旁白合成默认走 `adapter.SynthesizeSpeech`（供应商 TTS 可用时优先，否则 Edge TTS）。

## License

MIT
