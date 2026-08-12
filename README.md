# ComfyStudio · ComfyUI 多卡管理控制平台

基于 Go + Vue3 的 ComfyUI 视频生成管理平台，管理 8×NVIDIA L40，支持 MiniMax H3 工作流（文生视频 / 图生视频 / 首尾帧 / 参考视频），任务自动分配到最空闲 GPU，前端实时展示节点级进度与显卡占用，生成结果在线预览并解析视频元数据（分辨率/时长/大小/编码）。

内置 **AI 漫剧工作台**：剧本（火山引擎文生文）→ 分镜画面（火山引擎文生图）→ 视频（本地 L40 图生视频）→ 合并成片，一条流水线完成漫剧制作。支持 **角色资产库（Character Bible，跨场景人物一致）**、**画幅选择（横屏 16:9 / 竖屏 9:16 / 方形 1:1）**、**ref2v 角色参考图锁角色**、**对白配音（TTS）与 SRT 字幕**、全局 Toast 通知。平台设置中可配置火山引擎 Ark 的 API Key 与模型 ID。

## 技术栈

| 端 | 技术 |
|---|---|
| 后端 | Go 1.25 + Gin + GORM(SQLite) + gorilla/websocket |
| 前端 | Vue3 + Vite + Pinia + Vue Router（苹果风格设计系统，无 UI 库，支持黑白主题切换） |
| 数据库 | SQLite（`/opt/comfyui-console/data/console.db`） |
| 部署 | 单二进制（前端 + 模板 JSON embed 进 Go 程序），Docker 运行 |

## 架构

```
┌─────────────── 部署服务器 (8×L40) ─────────────────┐
│ ┌─────────────┐   HTTP/WS    ┌──────────────────────┐                       │
│ │  Vue3 前端   │ ───────────▶ │  Go 后端 console     │                       │
│ │ 苹果风格 UI  │ ◀─────────── │  (:18000, Docker 容器)│                       │
│ │ 漫剧工作台   │   实时进度    │ 任务调度/实例管理/GPU监控 │                      │
│ └─────────────┘              │ 漫剧项目/合并/设置/TTS │                       │
│  Docker 容器 console          └──────┬───────────────┘                       │
│  代码 /opt/comfyui-console              │ SSH/SFTP (remote)                        │
│  数据 /opt/comfyui-console        ▼                                        │
│  ┌────────────────────────────────────────────────────────────────────┐    │
│  │ ComfyUI 宿主机裸进程 comfyui-gpu0~7（端口 8188~8195 ↔ GPU 0~7）     │    │
│  │  /opt/comfyUI · start-multi-gpu.sh 启停 · 复用宿主 conda          │    │
│  │  comfyenv · 独立 user/temp/output_workers/gpuN 目录                │    │
│  └────────────────────────────────────────────────────────────────────┘    │
│                        ▲                                                     │
│        HTTPS 火山引擎 Ark（文生文/文生图/音频）云 API 在线模型                 │
└──────────────────────────────────────────────────────────────────────────────┘
```

- **部署服务器**：单机部署——`console`（前端 + Go 后端 + SQLite）跑在 Docker 容器，8 个 ComfyUI 实例为宿主机裸进程，由 console 经 SSH 密钥（`config.yaml` 的 `remote` 段）启停与读写，不再使用 compose 管理 comfyui 容器。
- **代码仓库**：`/opt/comfyui-console`（git 仓库，`deploy.sh` 会自动 `git pull`）；**数据/配置**：`/opt/comfyui-console/`；**ComfyUI**：`/opt/comfyUI`（模型、input、output）；**conda 环境**：`/opt/miniconda3/envs/comfyenv`。
- **ComfyUI 实例**：由 `/opt/comfyUI/start-multi-gpu.sh` 管理（每卡一个实例，端口 8188~8195，仅 GPU0 启用 Manager），console 通过 SSH 调用其 start/stop/restart。

## 创建 → 展示全链路数据流

```
① 选模板           ② 上传素材              ③ 配置参数
   GET /templates     POST /upload            分辨率预设/时长/步数/种子/CFG/帧率
      │              (SFTP→106 input/<tid>/)        │
      ▼                                            ▼
   POST /api/tasks ──▶ TaskService.CreateTask
                        ├─ normalizeTemplateFiles  校验必填素材 + 复数素材展开为索引占位符
                        ├─ 写入 SQLite (status=pending)
                        └─ go Execute() 异步：
                           ├─ pickInstance         并发探测 8 实例，选队列最短 + 显存最闲
                           ├─ RenderWorkflow       标量占位符替换 + 时长→帧数(对齐 17k+5) + 文件占位符/节点裁剪
                           ├─ SubmitPrompt         POST /prompt → ComfyUI 返回 prompt_id
                           ├─ status=queued → running
                           └─ listenWS             连 ComfyUI WS 跟踪任务
                                ├─ progress_state  节点级进度 (生成中封顶 99%)
                                ├─ execution_success → finishTask
                                │     └─ GET /history → 提取 videos/audio 结果文件
                                └─ 每 10s reconcile  队列兜底检查

④ 实时进度                  ⑤ 结果展示
   WS /api/ws                  GET /api/output/:gpu/*path   视频 Range 播放/下载
   task_update 推送             GET /api/media/:gpu/*path    元数据(分辨率/时长/大小/编码)
   (status/progress/node)       纯手写 MP4/图片 头部解析，不依赖 ffprobe
```

## 工作流模板（4 个）

模板源码位于 **`backend/internal/service/templates/*.json`**，通过 `//go:embed templates/*.json` 编入二进制，服务启动时由 `InitSystemTemplates` 种子化进 SQLite `templates` 表（已存在则更新）。前端通过 `GET /api/templates` 获取。

| 模板 | code | 文件 | 必填素材 | 节点数 | 典型耗时 |
|---|---|---|---|---|---|
| 文生视频 | `minimax_h3_t2v` | `minimax_h3_t2v.json` | 无 | 11 | ~90s |
| 图生视频 | `minimax_h3_i2v` | `minimax_h3_i2v.json` | 首帧图 | 13 | ~60s |
| 首尾帧视频 | `minimax_h3_first_last` | `minimax_h3_first_last.json` | 首帧 + 尾帧图 | 13 | ~60s |
| 参考视频 | `minimax_h3_ref2v` | `minimax_h3_ref2v.json` | 提示词（参考图/视频/音频可选） | 26 | ~120s |

**占位符约定**：`{{param}}` 标量参数；`{{img:key}}`/`{{vid:key}}`/`{{aud:key}}` 文件占位符（未提供则该节点自动裁剪，复数素材展开为 `key_0`/`key_1`…）。`length`（帧数）由 `duration × fps` 自动计算并对齐 `17k+5` 网格，`task_id` 自动注入。

**分辨率预设**（创建页）：480P = 832×480 · 720P = 1280×704 · 1080P = 1920×1088。

## 功能

- [x] 实例管理：启动/停止/重启单实例（SSH 调用 start-multi-gpu.sh）、一键启停全部、一键重启全部（异步执行）
- [x] GPU 看板：8 卡温度/功耗/显存/利用率/进程实时监控
- [x] 工作流模板：4 个 MiniMax H3 模板（t2v / i2v / 首尾帧 / 参考视频）
- [x] 任务创建：模板选择 → 素材上传（图片/视频/音频）→ 参数配置（分辨率预设/时长/步数/种子/CFG/帧率）
- [x] 自动调度：并发探测实例负载，任务提交到最空闲 GPU（队列短优先，显存大次之）
- [x] 实时进度：WS 推送 ComfyUI 0.30+ 节点级进度（`progress_state`/`execution_success`），生成中封顶 99%
- [x] 结果展示：生成视频在线预览（HTTP Range 拖动）+ 下载
- [x] 视频元数据：纯手写解析 MP4/图片（分辨率/时长/大小/编码），不依赖 ffprobe
- [x] 任务管理：列表筛选 / 取消 / 重试 / 详情参数去重回看 / 清空已结束任务；TaskDetail 首帧/参考图缩略图、视频置顶全宽展示
- [x] 任务自动恢复：平台/实例重启后后台循环（30s）扫描 `running`/`queued` 卡死任务并 reconcile 恢复；history 丢失时从 `output_workers/gpuN/` 按任务 ID 兜底恢复结果
- [x] 远程算力：SSH/SFTP 管理远程 ComfyUI 节点（实例启停、素材上传、结果读取）
- [x] 模拟模式：`simulate: true` 时按模板参考耗时模拟进度，无需 GPU 即可验证前端全流程
- [x] 平台设置：配置火山引擎 Ark API Key / 文生文模型 / 文生图模型 / 画面尺寸，支持测试连通性
- [x] 漫剧项目：创建项目后自动启动持久化流水线，依次完成剧本、分镜画面、场景视频和成片合并；服务重启后可恢复
- [x] 视觉一致性与时长：剧本同时生成角色/画风视觉基准，逐场景按对白与动作生成 3~8 秒动态时长
- [x] 场景状态机：待画面 → 画面生成中 → 画面就绪 → 视频排队/生成中 → 视频就绪，后台 3s 轮询回写，WS 即时刷新
- [x] 场景/项目编辑：修改场景文案与画面提示词（自动重置下游产物）、编辑项目信息与创意
- [x] 文生图并发控制：一键生成画面限流并发（3 路），避免火山 API 触发限流
- [x] 视频失败自动重试：场景视频任务失败自动重试（最多 2 次），无需人工干预
- [x] 项目状态机：draft → script_done → producing → ready（全部视频就绪）→ finished（合并成片完成）
- [x] 视频合并：按场景顺序 concat 拼接并保留一致音轨（远程 ffmpeg，libx264/AAC），输出到 output_workers/gpu0/merged/ 在线预览下载
- [x] 角色资产库（Character Bible）：项目级角色卡（trait/style/标准像），创作方案生成时自动抽取为独立资产（重复抽取幂等，不覆盖手动编辑）；场景绑定出场角色，文生图注入权威设定纠偏描述漂移
- [x] 角色标准像：基于角色 trait 文生图生成标准参考像（限流并发），角色面板支持编辑/生成/看出场统计
- [x] 画幅选择：项目级画幅（横屏 16:9 / 竖屏 9:16 / 方形 1:1），场景视频按画幅分辨率（横 1280×704 / 竖 704×1280 / 方 1024×1024）、分镜画面按画幅比例文生图（满足 seedream 5.0 像素约束，避免首帧变形）
- [x] ref2v 角色参考锁角色：有出场角色标准像时，场景视频从 i2v 切换 ref2v，`ref_images=[首帧+各角色标准像]`，prompt 注入 `<Picture N>` 引用保证跨场景人物一致；无角色时自动回退 i2v（更快）
- [x] 画面注入只取角色 trait：文生图注入角色权威设定时不注入 style，避免多套服装致文生图困惑
- [x] 全局 Toast 通知：替代浏览器 alert，右上角堆叠、自动消失、可点击关闭
- [x] 对白配音（TTS）：剧本自动拆分场景对白（Dialogue 模型），火山 Ark `/audio/speech` 合成 mp3；场景/项目级一键配音，对白音频在线试听
- [x] SRT 字幕：对白文本直出 SRT（按场景时长均分时间轴），按集下载，含 UTF-8 BOM 兼容 Windows 播放器
- [x] UI 体验：黑白主题切换、项目头部 sticky 浮顶（选集后生成按钮随滚动可见）、按钮命名消除歧义（明确集数/范围）

## AI 漫剧工作台流程

```
① 新建项目（创意/题材/画风/画幅）     ② 生成创作方案 → 自动抽取角色卡
   POST /api/projects                  方案 characters(trait/style) → Character 实体
   画幅 16:9 / 9:16 / 1:1              角色标准像（火山文生图，限流并发）
        │                                    │
        ▼                                    ▼
③ 生成分镜画面（按画幅比例 文生图）    ④ 生成场景视频（ref2v 锁角色 / i2v）
   POST /api/projects/:id/images       POST /api/projects/:id/videos
   注入出场角色权威设定(trait)纠偏      有标准像→ref2v(ref_images=首帧+标准像，
   横2560×1440 / 竖1440×2560 / 方1920²   prompt <Picture N> 引用)；无→i2v(仅首帧)
        │                                    │
        ▼                                    ▼
⑤ 合并成片（远程 ffmpeg concat）
   POST /api/projects/:id/merge
   按场景顺序拼接并保留音轨 → 成片在线预览/下载
```

## 项目结构

```
├── backend/                      # Go 后端
│   ├── main.go                   # 入口：config → db → service → router
│   ├── config.yaml.example        # 配置模板（复制为 config.yaml 填写真实值）
│   ├── internal/
│   │   ├── api/router.go         # 路由
│   │   ├── config/               # 配置加载
│   │   ├── database/             # SQLite 初始化 + 自动迁移
│   │   ├── models/               # 数据模型 (Task/Template/Instance/UploadFile/Event)
│   │   ├── service/
│   │   │   ├── handlers.go          # API Handlers（含 ClearTasks / Output / MediaInfo / WS）
│   │   │   ├── task_service.go      # 任务创建/调度/渲染/WS进度/simulate
│   │   │   ├── project_service.go   # 漫剧项目：剧本/分镜/视频/合并 + 状态轮询
│   │   │   ├── project_handlers.go  # 漫剧项目与平台设置 Handlers
│   │   │   ├── volc_engine.go       # 火山引擎 Ark 客户端（文生文/文生图/音频/设置）
│   │   │   ├── comfy_client.go      # ComfyUI HTTP 客户端 (prompt/queue/history)
│   │   │   ├── ws_client.go         # ComfyUI WS 监听 + 前端推送 Hub
│   │   │   ├── instance_manager.go  # 实例启停（SSH 调 start-multi-gpu.sh 管理裸进程）
│   │   │   ├── gpu_monitor.go       # nvidia-smi 采集
│   │   │   ├── remote.go            # SSH/SFTP 远程执行 (Run/WriteFile/OpenSeek/Size，remote.host 留空时不使用)
│   │   │   ├── media_probe.go       # MP4/图片 元数据解析 (ProbeMedia)
│   │   │   ├── template_seed.go     # 系统模板种子化 + 素材上传管理
│   │   │   ├── task_service_test.go # 模板渲染/调度排序单元测试
│   │   │   ├── project_service_test.go # 剧本JSON解析/结果提取/图片类型 单元测试
│   │   │   └── templates/           # 4 个工作流模板 JSON (embed)
│   │   └── static/dist          # 前端构建产物 (embed)
│   └── deploy.sh                # 传统二进制部署脚本
├── comfyui/                     # ComfyUI fork 代码（源目录，构建进镜像或复制到宿主）
│   ├── Dockerfile               # ComfyUI 镜像（复用宿主 conda 环境）
│   └── comfy/ldm/minimax/       # MiniMax H3 DiT 模型（t2v/i2v/首尾帧/参考视频）
├── frontend/                     # Vue3 前端
│   └── src/
│       ├── views/                # Dashboard/CreateTask/Tasks/TaskDetail/Instances
│       │                         # + Projects/ProjectDetail(漫剧工作台)/Settings(平台设置)
│       ├── styles/main.css       # 苹果风格设计系统（含黑白主题变量）
│       ├── api/index.js          # API + WS 客户端
│       └── stores/app.js
├── Dockerfile                    # console 镜像（前端 embed + Go 单二进制）
├── docker-compose.yml            # console 容器（comfyui 为宿主机裸进程，SSH 管理）
└── deploy.sh                     # 一键部署（git pull → conda 依赖 → 镜像 → 启动 → 健康检查）
```

## 部署

> **部署服务器**: `<服务器IP>`（8×L40，console 容器 + 8×ComfyUI 宿主机裸进程）
> · 代码目录 `/opt/comfyui-console`（git 仓库，docker build context）
> · 数据/配置目录 `/opt/comfyui-console/`（volume 挂载）
> · ComfyUI 目录 `/opt/comfyUI/`（`start-multi-gpu.sh` 管理 8 实例）· conda 环境 `/opt/miniconda3/envs/comfyenv`

### Docker 一键部署（推荐，全链路中国源）

**1. 前置准备（仅首次）**

```bash
# 安装 Docker（国内镜像脚本，可自动配置加速）
curl -fsSL https://get.docker.com | sh

# 安装 nvidia-container-toolkit（如需容器内 GPU，当前架构 ComfyUI 跑在宿主机，可跳过）
curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg
curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' | tee /etc/apt/sources.list.d/nvidia-container-toolkit.list
apt-get update && apt-get install -y nvidia-container-toolkit
nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml
```

**2. 一键部署 / 更新**

```bash
# SSH 登录服务器（root），进入代码仓库后执行（需 root）
ssh root@<服务器IP>
cd /opt/comfyui-console
bash deploy.sh            # 标准部署（git pull 更新代码 → 构建/加载镜像 → 启动 console → 健康检查）
bash deploy.sh --no-gpu   # 无 GPU / 未装 toolkit 时
bash deploy.sh --rebuild  # 强制重建镜像（默认镜像已存在则跳过构建）
```

脚本自动完成：`git pull` 更新代码 → conda 依赖增量安装 → 构建/加载 `comfyui-console:latest`（前端 embed + Go 单二进制，构建后 `docker save` 备份，下次秒级 load）→ 启动 console 容器 → 健康检查（平台 `/api/health`）。

**3. 验证与运维**

```bash
# 验证：浏览器访问 http://<服务器IP>:18000

# ComfyUI 实例（宿主机裸进程）
cd /opt/comfyUI && bash start-multi-gpu.sh status   # 查看 8 实例状态
bash start-multi-gpu.sh restart                        # 重启全部实例

# console 日志
docker logs -f console
docker restart console
```

**目录约定**：

| 宿主机路径 | 用途 |
|---|---|
| `/opt/comfyui-console/` | 代码仓库（git clone / docker build context / deploy.sh） |
| `/opt/comfyui-console/config.yaml` | 平台配置（含 remote SSH 段；可修改后重启容器） |
| `/opt/comfyui-console/data/` | SQLite 数据库、上传素材（持久化） |
| `/opt/comfyui-console/ssh_key` | SSH 私钥（console 容器内管理宿主机 ComfyUI 裸进程） |
| `/opt/comfyUI/` | ComfyUI 代码/模型/素材目录（`start-multi-gpu.sh` 管理 8 实例） |
| `/opt/miniconda3/` | conda python 环境（`envs/comfyenv/bin/python`，ComfyUI 复用） |
| `/opt/docker-images/` | console 镜像备份（`comfyui-console.tar.gz`，免重复构建） |

### 传统二进制部署（备用）

```bash
# ① 交叉编译（Linux 开发机）
cd backend
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o console-linux-amd64 .

# ② 上传并启动
scp console-linux-amd64 config.yaml root@<server>:/opt/comfyui-console/
ssh root@<server> "cd /opt/comfyui-console && bash -c 'nohup ./console-linux-amd64 > console.log 2>&1 &'"
```

## API 摘要

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /api/health | 健康检查 |
| GET | /api/instances | 实例列表（状态/队列/显存，自动探测真实状态） |
| POST | /api/instances/:id/start · stop · restart | 实例启停（id=GPU序号，SSH 调 start-multi-gpu.sh） |
| POST | /api/instances/start-all · stop-all · restart-all | 一键启停 / 重启全部（异步） |
| GET | /api/gpus | GPU 实时状态 |
| GET | /api/templates | 工作流模板（4 个） |
| GET | /api/tasks · /api/tasks/:id | 任务列表（分页/筛选） / 详情 |
| POST | /api/tasks | 创建任务（提交后自动调度执行） |
| DELETE | /api/tasks | 清空已结束任务（存在活动任务时拒绝） |
| POST | /api/tasks/:id/cancel · rerun | 取消 / 重试 |
| POST | /api/upload | 素材上传 (multipart: file/type/task_id) |
| GET | /api/output/:gpu/*path | 结果文件（视频/音频，支持 Range/MIME/下载） |
| GET | /api/media/:gpu/*path | 输出文件元数据（分辨率/时长/大小/编码） |
| GET | /api/input/:taskid/*path | ComfyUI input 目录文件（分镜画面预览） |
| WS | /api/ws | 实时推送（实例/GPU快照、任务进度） |

**平台设置（火山引擎 Ark）**

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /api/settings | 读取设置（API Key 打码） |
| PUT | /api/settings | 保存设置（API Key/接口地址/文生文模型/文生图模型/画面尺寸） |
| POST | /api/settings/test-text · test-image | 测试文生文 / 文生图连通性 |

**漫剧项目**

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST | /api/projects | 项目列表（含场景统计）/ 新建项目 |
| GET/PUT/DELETE | /api/projects/:id | 项目详情（含场景与合并列表）/ 编辑 / 删除 |
| POST | /api/projects/:id/generate | 启动剧本→画面→视频→合并的一键持久化流水线 |
| POST | /api/projects/:id/script | 生成剧本（文生文，自动拆分场景） |
| POST | /api/projects/:id/images | 一键生成全部画面（文生图，限流 3 并发） |
| POST | /api/projects/:id/videos | 一键生成全部视频（本地 L40 i2v） |
| PATCH | /api/projects/:id/scenes/:sid | 编辑场景（改画面提示词自动重置画面/视频） |
| POST | /api/projects/:id/scenes/:sid/image | 生成单场景画面 |
| POST | /api/projects/:id/scenes/:sid/video | 生成单场景视频 |
| POST | /api/projects/:id/merge | 合并选中场景为成片（ffmpeg） |
| GET | /api/projects/:id/merges | 合并任务列表 |

**角色资产（Character Bible）**

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /api/projects/:id/characters | 角色列表（含出场统计） |
| POST | /api/projects/:id/characters | 新建角色（手动，source=manual） |
| PUT | /api/projects/:id/characters/:cid | 编辑角色（name/role/trait/style） |
| DELETE | /api/projects/:id/characters/:cid | 删除角色 |
| POST | /api/projects/:id/characters/:cid/portrait | 生成/重新生成角色标准像 |
| POST | /api/projects/:id/characters/portraits | 一键生成全部缺标准像角色 |

**对白配音与字幕**

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /api/projects/:id/scenes/:sid/dialogues | 场景对白列表 |
| POST | /api/projects/:id/scenes/:sid/dub | 合成场景对白配音（TTS） |
| POST | /api/projects/:id/dub | 一键合成项目全部对白配音 |
| GET | /api/projects/:id/srt?episode_n=N | 下载该集 SRT 字幕 |

## 测试验证

**单元测试**（`backend/internal/service/task_service_test.go`，不依赖 GPU/DB）：
- 4 个模板 `RenderWorkflow` 渲染后无残留占位符 ✅
- 复数素材展开为索引占位符（`ref_image_0`/`ref_image_1`）✅
- 必填素材缺失校验 ✅
- 实例调度排序（队列短优先 → 显存大优先）✅

**端到端验证**（部署服务器，真实 8×L40 + ComfyUI）：
- 4 个模板任务全部创建成功，工作流渲染后成功提交至 ComfyUI，并 success 生成 MP4 ✅
- 自动调度到不同 GPU 并行执行，节点级进度（`progress_state`）正常推进 ✅
- 生成 MP4（1280×704 / h264 / ~5.2s），`/api/media` 返回正确元数据 ✅
- 一键 `start-all` / `stop-all` / `restart-all`：8 实例启停正常 ✅

## 已知问题 / 说明

- ComfyUI 0.30+ 的 WS 事件为 `progress_state`/`execution_success` 格式（非旧版 `progress`/`executing.prompt`）。
- `SaveVideo.codec` 为 DynamicCombo 类型，API 提交须用字符串 `"auto"`（不可用对象格式）。
- 素材上传后存于 ComfyUI 共享 `input/<task_id>/` 目录（8 实例共用 input）。
- 平台/实例重启后，遗留的 `running`/`queued` 任务由后台恢复循环（`StartRecovery`，30s 轮询）自动 reconcile；`pending` 任务需手动重试（未进入执行）。
- 模拟模式（`simulate: true`）跳过渲染与 ComfyUI 提交，仅验证前端进度/状态机全流程。
- `config.yaml` 的 `remote` 段配置 SSH 宿主信息（console 容器经 SSH 私钥免密登录宿主管理 ComfyUI 裸进程），模板见 `backend/config.yaml.example`。

## 故障排查记录

### 分镜画面人物不一致（2026-08-07 修复）

**现象**：漫剧工作台中已配置角色标准像（人物图），生成分镜画面时同一角色在多个分镜中外貌变化明显、无法锁定。

**根因**：方舟 Ark `images/generations`（Seedream 5.0）的参考图字段是 `image`（单张为字符串、多张为字符串数组），而非 `reference_image`。此前代码误用 `reference_image` + `subject`/`type`/`image_url`/`reference_region` 结构——这些字段在方舟 API 中并不存在，整段参考图被当作未知字段忽略，**分镜画面静默退化为纯文生图**，角色标准像从未真正参与生成。因 HTTP 200 正常返回、错误日志不触发，导致此前多次「锁人物」改动均未触及根因。

> `subject` / `reference_region` 实为「智能视觉服务(CV)」/「图生图 3.0 DreamO」的参数，方舟 `images/generations` 不支持。

**修复**：
- `backend/internal/service/volc_engine.go`：参考图改用正确的 `image` 字段（`ImageRef` 仅保留 URL；单张传 string、多张传 `[]string`），删除无效的 subject/type/reference_region 结构与 subject→reference 降级逻辑。
- `backend/internal/service/project_service.go`：
  - `buildSceneImagePrompt` 修复画风被重复注入 3 次的 bug（原 `styleDescriptor` 经 `projectVisualContext` 反复调用），改为单次 DB 查询内联。
  - `characterContextForScene` 注入完整外貌 `trait + style`（强化人物一致性）。
  - `buildSceneVideoSpec` 恢复 `ref2v` 分支（有标准像时首帧 + 各角色标准像作参考图锁人物），对齐项目设计意图（`minimax_h3_ref2v.json` 模板）。
  - 图生图 prompt 改用官方教程「图N」引用风格（如「图1 是林夏，图2 是陆川」）。

**验证**：`go test ./internal/service/` 全绿（含 `TestBuildSceneImagePrompt` / `TestCharacterContextForScene` / `TestBuildSceneVideoSpec`）。

**参考**：
- [方舟 图片生成 API（Seedream 5.0）](https://www.volcengine.com/docs/82379/1541523)
- [方舟 图片生成教程（参考图生图 / 多图融合）](https://www.volcengine.com/docs/82379/1824121)

## 开源协议

[MIT License](LICENSE) © [hequan2017](https://github.com/hequan2017)
