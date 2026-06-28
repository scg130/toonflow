# ToonFlow — 短剧 AI 生成工具

基于 Go 的轻量化短剧 AI 生成工具，对标 ToonFlow 漫画/短剧生成核心逻辑。

## 功能特性

- **剧本解析** — LLM 驱动的自动剧本拆分为多镜头分镜
- **统一画风** — 支持多种画风锁定，整剧风格一致
- **AI 绘图** — 批量 AI 生成每帧镜头画面（OpenAI 兼容接口）
- **视频合成** — 自动拼接图片帧生成 MP4 短剧
- **实时推送** — WebSocket 实时推送生成进度、日志、预览
- **可插拔适配器** — 轻松扩展新的 AI 供应商（Kling、Vidu 等）
- **Skill 系统** — Markdown 驱动的技能 prompt 模板

## 技术栈

- 后端：Go 1.25+
- 数据库：SQLite（纯 Go 驱动，无 CGO）
- WebSocket：gorilla/websocket
- 视频合成：FFmpeg
- 前端：原生 HTML + CSS + JS

## 快速开始

### 前置条件

- Go 1.25+
- FFmpeg（用于视频合成）

### 安装依赖

```bash
go mod tidy
```

### 运行

```bash
# 基本启动
go run main.go

# 自定义端口
go run main.go --port 9090

# 自定义数据库路径
go run main.go --db /path/to/db.sqlite

# 自定义输出目录
go run main.go --output-dir /path/to/output

# 自定义 skills 目录
go run main.go --skills-dir /path/to/skills
```

### 使用

1. 打开浏览器访问 `http://localhost:8080`
2. 输入剧本文本
3. 选择画风、分辨率等参数
4. 点击「开始生成」
5. 实时查看进度，完成后下载成片

### Docker 构建

```bash
docker build -t toonflow .
docker run -p 8080:8080 toonflow
```

## 项目结构

```
toonflow/
├── main.go                  # 入口：路由注册、服务启动
├── config.go                # 全局配置
├── adapter/                 # AI 供应商适配器系统
│   ├── adapter.go           # Vendor 接口、Registry
│   └── openai_compatible.go # OpenAI 兼容适配器
├── storage/                 # SQLite 持久层
│   ├── db.go
│   └── models.go
├── task/                    # 任务管理
│   ├── task.go
│   └── queue.go
├── service/                 # 业务逻辑
│   ├── script_parse.go
│   ├── ai_draw.go
│   └── video_merge.go
├── engine/                  # 流水线编排
│   └── pipeline.go
├── skill/                   # Skill prompt 系统
│   └── skill.go
├── ws/                      # WebSocket 层
│   ├── conn.go
│   └── handler.go
├── static/                  # 前端资源
│   ├── index.html
│   ├── css/style.css
│   └── js/app.js
└── skills/                  # Skill markdown 模板
    ├── art_skills/
    ├── story_skills/
    └── production_execution/
```

## API 接口

### WebSocket

- `ws://host:port/ws` — 实时进度推送

### REST

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/health` | 健康检查 |
| GET | `/api/vendors` | 列出已注册的 AI 适配器 |
| GET | `/api/tasks` | 列出所有任务 |
| GET | `/output/:path` | 访问生成的文件 |
| GET | `/download/:filename` | 下载成片 |

## 扩展新适配器

在 `adapter/` 目录下新建 `.go` 文件：

```go
package adapter

import "context"

type MyVendor struct{}

func init() { Register(&MyVendor{}) }

func (v *MyVendor) VendorConfig() VendorConfig { ... }
func (v *MyVendor) TextRequest(ctx interface{}, model string, params TextParams) (*TextResponse, error) { ... }
func (v *MyVendor) ImageRequest(ctx interface{}, model string, params ImageParams) (*ImageResponse, error) { ... }
func (v *MyVendor) VideoRequest(ctx interface{}, model string, params VideoParams) (*VideoResponse, error) { ... }
func (v *MyVendor) TTSRequest(ctx interface{}, model string, params TTSParams) (*TTSResponse, error) { ... }
```

## License

MIT
