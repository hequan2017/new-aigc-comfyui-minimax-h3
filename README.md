# ComfyStudio · ComfyUI 多卡管理控制平台

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-blue)](backend/go.mod)
[![Vue](https://img.shields.io/badge/Vue-3-green)](frontend/package.json)

ComfyStudio 是一个基于 **Go + Vue3** 的 ComfyUI 多卡管理平台：管理 **8×NVIDIA L40** 算力节点，内置 **MiniMax H3** 工作流（文生视频 / 图生视频 / 首尾帧 / 参考视频），自动把任务调度到最空闲的 GPU，前端实时展示**节点级生成进度**与显卡占用，生成结果在线预览并解析视频元数据。

平台还内置 **AI 漫剧工作台**：一条流水线完成「剧本 → 分镜画面 → 场景视频 → 合并成片」的漫剧制作，支持**角色资产库**（跨场景人物一致）、**ref2v 角色参考图锁角色**、**对白配音（TTS）与 SRT 字幕**。

---

## 目录

- [核心特性](#核心特性)
- [系统架构](#系统架构)
- [功能清单](#功能清单)
- [技术栈](#技术栈)
- [部署指南](#部署指南)
- [配置说明](#配置说明)
- [项目结构](#项目结构)
- [API 摘要](#api-摘要)
- [测试验证](#测试验证)
- [已知问题](#已知问题)
- [开源协议](#开源协议)

---

## 核心特性

| 能力 | 说明 |
|---|---|
| 多卡调度 | 8×L40，任务自动分配到**队列最短 + 显存最闲**的 GPU |
| 实时进度 | WS 推送 ComfyUI 0.30+ 节点级进度（`progress_state`），生成中封顶 99% |
| MiniMax H3 | 4 个工作流模板：文生视频 / 图生视频 / 首尾帧 / 参考视频 |
| 任务恢复 | 平台/实例重启后自动 reconcile 卡死任务，history 丢失时按任务 ID 兜底恢复结果 |
| 远程算力 | SSH/SFTP 管理远程 ComfyUI 节点（实例启停、素材上传、结果读取） |
| 视频元数据 | 纯手写解析 MP4/图片（分辨率/时长/大小/编码），不依赖 ffprobe |
| 漫剧工作台 | 剧本（火山文生文）→ 分镜（火山文生图）→ 视频（本地 L40）→ 合并成片 |
| 角色资产库 | 角色卡（trait/style/标准像）自动抽取，ref2v 锁角色保证跨场景一致 |
| TTS 配音 | 火山 Ark `/audio/speech` 合成对白，直出 SRT 字幕（UTF-8 BOM） |
| 模拟模式 | `simulate: true` 时按模板参考耗时模拟进度，无需 GPU 即可体验全流程 |

---

## 系统架构

```mermaid
graph TB
    subgraph PUBLIC[浏览器端]
        B[Vue3 前端<br/>苹果风格 UI]
    end

    subgraph DC[Docker 容器]
        C[console 后端<br/>Go + Gin · :18000]
        S[(SQLite<br/>console.db)]
    end

    subgraph GPUNODE[算力宿主机 · 8×L40]
        F[ComfyUI ×8<br/>GPU 0~7 · 端口 8188~8195]
        M[ffmpeg 合并<br/>成片 concat + 音轨]
    end

    V[火山引擎 Ark<br/>文生文 · 文生图 · TTS]

    B -- "HTTP / WS 实时进度" --> C
    C -- "GORM SQL 读写" --> S
    C -- "SSH/SFTP 管理 + HTTP/WS 提交" --> F
    F -- "节点级进度 WS 回传" --> C
    C -- "SSH 触发视频合并" --> M
    C -- "HTTPS 文生文/图/TTS" --> V
```

### 组件说明

| 组件 | 类型 | 职责 |
|---|---|---|
| 浏览器前端 | Vue3 + Vite | Dashboard / 任务创建 / 任务详情 / 实例管理 / GPU 看板 / 漫剧工作台 / 平台设置，WS 实时刷新 |
| console 后端 | Go + Gin | 任务调度、模板渲染、ComfyUI 通信、GPU 监控、实例管理、漫剧流水线、平台设置（单二进制，前端 embed） |
| SQLite | GORM | 任务 / 模板 / 实例 / 项目 / 场景 / 角色 / 设置 持久化 |
| ComfyUI ×8 | 宿主机裸进程 | MiniMax H3 推理（t2v / i2v / 首尾帧 / ref2v），每卡一实例，端口 8188~8195 |
| ffmpeg | 远程命令 | 按场景顺序 concat 拼接成片并保留音轨（libx264/AAC） |
| 火山引擎 Ark | 云 API | 文生文（剧本生成 / 分镜提示词）、文生图（Seedream 5.0）、TTS 对白配音 |

### 任务执行数据流

```mermaid
sequenceDiagram
    participant U as 用户
    participant C as console
    participant S as SQLite
    participant F as ComfyUI GPU0~7
    participant V as 火山引擎 Ark

    U->>C: 创建任务（模板 + 素材 + 参数）
    C->>S: 写入任务 (status=pending)
    C->>F: 并发探测 8 实例负载
    C->>C: 选队列最短 + 显存最闲
    C->>F: 渲染工作流并提交 /prompt
    F->>F: 执行生成
    F-->>C: WS 节点级进度 progress_state
    C-->>U: WS 推送 task_update
    F-->>C: execution_success → GET /history 提取结果
    C->>S: 更新任务状态与结果

    Note over C,V: 漫剧流水线（可选）：<br/>C->>V 文生文剧本 → 文生图分镜 → 本地视频 → ffmpeg 合并
```

---

## 功能清单

### 平台基础

- [x] **实例管理**：启动/停止/重启单实例（SSH 调用 `start-multi-gpu.sh`）、一键启停全部、一键重启全部（异步执行）
- [x] **GPU 看板**：8 卡温度/功耗/显存/利用率/进程实时监控
- [x] **工作流模板**：4 个 MiniMax H3 模板（t2v / i2v / 首尾帧 / 参考视频），占位符渲染 + 复数素材展开 + 自动节点裁剪
- [x] **任务创建**：模板选择 → 素材上传（图片/视频/音频）→ 参数配置（分辨率预设/时长/步数/种子/CFG/帧率）
- [x] **自动调度**：并发探测实例负载，提交到最空闲 GPU（队列短优先，显存大次之）
- [x] **实时进度**：WS 推送节点级进度，生成中封顶 99%
- [x] **结果展示**：在线预览（HTTP Range 拖动）+ 下载，元数据纯手写解析
- [x] **任务管理**：列表筛选 / 取消 / 重试 / 详情回看 / 清空已结束任务
- [x] **任务自动恢复**：后台循环（30s）扫描 `running`/`queued` 卡死任务并 reconcile；history 丢失时从 `output_workers/gpuN/` 兜底恢复
- [x] **平台设置**：火山引擎 Ark API Key / 模型 ID / 画面尺寸，支持测试连通性（API Key 打码回显）

### AI 漫剧工作台

- [x] **一键流水线**：创建项目 → 生成创作方案（自动抽取角色卡）→ 剧本 → 分镜画面 → 场景视频 → 合并成片，服务重启可恢复
- [x] **项目状态机**：draft → script_done → producing → ready → finished
- [x] **场景状态机**：待画面 → 画面生成中 → 画面就绪 → 视频排队/生成中 → 视频就绪
- [x] **角色资产库**：角色卡（trait/style/标准像），重复抽取幂等、不覆盖手动编辑；文生图注入权威设定纠偏描述漂移
- [x] **角色标准像**：基于 trait 文生图生成，角色面板支持编辑/重新生成/出场统计
- [x] **画幅选择**：横屏 16:9 / 竖屏 9:16 / 方形 1:1，画面按画幅比例文生图、视频按画幅分辨率
- [x] **ref2v 锁角色**：有标准像时切 ref2v（`ref_images=[首帧+角色标准像]`），无角色自动回退 i2v
- [x] **视频失败重试**：场景视频任务失败自动重试（最多 2 次）
- [x] **视频合并**：按场景顺序 concat 拼接并保留一致音轨（libx264/AAC）
- [x] **对白配音**：剧本自动拆分场景对白（Dialogue 模型），火山 Ark 合成 mp3，在线试听
- [x] **SRT 字幕**：按场景时长均分时间轴，按集下载（UTF-8 BOM 兼容 Windows 播放器）
- [x] **文生图并发控制**：一键生成画面限流 3 并发，避免火山 API 限流

---

## 技术栈

| 端 | 技术 |
|---|---|
| 后端 | Go 1.25 + Gin + GORM(SQLite) + gorilla/websocket + pkg/sftp |
| 前端 | Vue3 + Vite + Pinia + Vue Router（苹果风格设计系统，无 UI 库，黑白主题切换） |
| 数据库 | SQLite（`/opt/comfyui-console/data/console.db`） |
| 推理 | ComfyUI（MiniMax H3，8×NVIDIA L40 裸进程） |
| 云服务 | 火山引擎 Ark（文生文 / 文生图 Seedream 5.0 / TTS） |
| 部署 | 单二进制（前端 embed），Docker / 裸进程两种方式 |

---

## 部署指南

### 部署架构（两种形态）

| 形态 | 说明 | 适用 |
|---|---|---|
| **console 容器 + ComfyUI 裸进程**（默认） | console 跑 Docker 容器（前端 embed + Go 单二进制 + SQLite），8 个 ComfyUI 实例为**宿主机裸进程**（`start-multi-gpu.sh` 管理），console 经 **SSH** 启停实例、经 **SFTP** 读写素材/结果 | 生产（推荐，本仓库默认架构） |
| **全容器（compose 管理 ComfyUI）** | `comfy.mode: docker`，console 经 SSH 在宿主执行 `docker compose` 管理 `comfyui-gpu{N}` 容器（CDI 绑卡，共享挂载 models） | 容器化隔离需求 |
| **本地模式** | `comfy.mode: local`，ComfyUI 与本机 console 同机裸进程，`remote.host` 留空 | 单机开发测试 |

### 组件清单

| 组件 | 作用 | 部署位置 | 获取方式 |
|---|---|---|---|
| **console**（Go 后端 + Vue3 前端 embed） | 任务调度 / GPU 监控 / 实例管理 / 漫剧流水线 / 设置 | Docker 容器 `console`（:18000） | 本仓库构建（Dockerfile） |
| **SQLite** | 任务 / 模板 / 项目 / 设置持久化 | `data/console.db`（volume 挂载） | console 启动时自动迁移建表 |
| **ComfyUI ×8** | MiniMax H3 推理（t2v/i2v/首尾帧/ref2v），每卡一实例 | 宿主机 `/opt/comfyUI`（裸进程，端口 8188~8195） | 本仓库 `comfyui/` 目录（含 MiniMax H3 实现） |
| **conda 环境 `comfyenv`** | ComfyUI 运行依赖（torch 等，镜像内不装，复用宿主） | 宿主机 `/opt/miniconda3/envs/comfyenv` | `deploy.sh` 自动增量 `pip install -r comfyui/requirements.txt`（幂等） |
| **MiniMax H3 模型权重** | DiT 模型（t2v / i2v / ref2v + VAE/文本编码器） | 宿主机 `/opt/comfyUI/models/` 下对应子目录 | 手动下载放置（见下方「模型放置」） |
| **start-multi-gpu.sh** | 宿主机多卡实例启停脚本（start/stop/restart/status） | 宿主机 `/opt/comfyUI/start-multi-gpu.sh` | 仓库外维护，console 经 SSH 调用 |
| **SSH 凭证** | console → 宿主机 免密管理（私钥优先于密码） | `config.yaml` 的 `remote` 段 + `/opt/comfyui-console/ssh_key` | 自行生成/配置 |
| **火山引擎 Ark API Key** | 文生文（剧本）/ 文生图（分镜）/ TTS（配音） | 平台设置页（存 SQLite，打码回显） | 火山引擎控制台申请 |
| **NVIDIA 驱动 + nvidia-container-toolkit** | GPU 监控（nvidia-smi）+ 容器 GPU 访问（CDI） | 宿主机 | 官方驱动 + `nvidia-ctk cdi generate` |
| **Docker + compose v2** | console 容器运行 | 宿主机 | 官方安装脚本 |

### 环境要求

- **算力服务器**：Linux + NVIDIA GPU（`comfy.gpu_count` 可调，默认 8）
- **Docker** + docker compose v2（仅 console 容器需要）
- **conda**（miniconda3）与 ComfyUI 依赖环境 `comfyenv`
- **MiniMax H3 模型权重**（必须，否则任务无法推理）
- **网络**：可访问 Docker Hub 镜像加速（脚本默认 daocloud / npmmirror / goproxy.cn 中国源）

### 部署步骤

#### 1. 下载代码

```bash
git clone https://github.com/hequan2017/new-aigc-comfyui-minimax-h3.git
cd new-aigc-comfyui-minimax-h3
```

#### 2. 准备 conda 环境

```bash
# 创建 conda 环境（如已存在可跳过，deploy.sh 会自动增量安装缺失依赖）
conda create -n comfyenv python=3.11 -y
```

依赖安装由 `deploy.sh` 自动完成（`pip install -r comfyui/requirements.txt -i https://pypi.tuna.tsinghua.edu.cn/simple`）。

#### 3. 放置 ComfyUI 与模型

将 `comfyui/` 目录部署到 `/opt/comfyUI`，并按 MiniMax H3 工作流要求放置模型权重：

```
/opt/comfyUI/models/
├── diffusion_models/      # MiniMax H3 DiT 权重（t2v / i2v / ref2v）
├── vae/                   # 视频 VAE
├── text_encoders/         # 文本编码器（H3 tokenizer 等）
└── checkpoints/           # （如需）
```

> 模型文件较大（数十 GB 级），部署到 `/data1` 或大容量盘后调整 `config.yaml` 的 `comfy.comfy_dir`。

#### 4. 准备多卡启动脚本

宿主机 `/opt/comfyUI/` 下准备 `start-multi-gpu.sh`（管理 8 个实例：每卡一个裸进程，端口 8188~8195，`--reserve-vram 6`，仅 GPU0 启用 Manager）。console 的实例管理/一键启停功能通过 SSH 调用它。

#### 5. 生成配置

```bash
cp backend/config.yaml.example backend/config.yaml
```

编辑 `backend/config.yaml`：

```yaml
remote:
  host: "<算力节点IP>"        # SSH 地址（remote.host 为空时按本地模式运行）
  port: 22
  user: root
  password: ""              # 推荐改用私钥
  private_key: "/opt/comfyui-console/ssh_key"
comfy:
  comfy_dir: /opt/comfyUI   # 与宿主机实际目录一致
simulate: false
```

#### 6. 一键部署（Docker 容器）

```bash
bash deploy.sh               # git pull → conda 依赖 → 构建镜像 → 启动 → 健康检查
bash deploy.sh --no-gpu      # 无 GPU / 未装 toolkit 时
bash deploy.sh --rebuild     # 强制重建镜像（默认每次发版已强制重建）
```

`deploy.sh` 自动完成：

1. `git pull` 更新代码（更新后自动重新加载脚本继续）
2. 清理旧部署（旧 console 进程 / 残留容器）
3. 检查 Docker / CDI / GPU 环境
4. 准备宿主机目录与 `config.yaml`（首次自动从模板复制）
5. conda 环境增量安装 ComfyUI 依赖
6. 构建 `comfyui:latest` 与 `comfyui-console:latest` 镜像（构建后 `docker save` 备份到 `/opt/docker-images/`）
7. `docker compose up -d` 启动 console
8. 健康检查：平台 `/api/health` + 8 个 ComfyUI 实例 `/system_stats`

部署完成后浏览器访问 `http://<服务器IP>:18000`。

#### 7. 验证部署

```bash
# 平台健康
curl http://127.0.0.1:18000/api/health

# ComfyUI 实例（宿主机裸进程）
cd /opt/comfyUI && bash start-multi-gpu.sh status

# 浏览器
# 总览页确认 8 卡 GPU 看板在线 → 创建任务 → 观察节点级进度 → 预览生成视频
```

### 传统二进制部署（备用，无 Docker）

```bash
# ① 本地交叉编译（Linux 开发机）
cd backend
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o console-linux-amd64 .

# ② 上传并启动（backend/deploy.sh 亦可一键完成）
scp console-linux-amd64 config.yaml root@<server>:/opt/comfyui-console/
ssh root@<server> "cd /opt/comfyui-console && bash -c 'nohup ./console-linux-amd64 > console.log 2>&1 &'"
```

### 模拟模式（无 GPU 体验）

将 `config.yaml` 的 `simulate` 设为 `true`，任务按模板参考耗时模拟进度推进直至成功，不连接 ComfyUI——可用于本地开发验证前端全流程。

### 运维与升级

| 操作 | 命令 |
|---|---|
| 升级发版 | 服务器仓库 `bash deploy.sh`（每次强制重建镜像，前端/后端代码总是最新） |
| 查看日志 | `docker logs -f console` |
| 重启平台 | `docker restart console` |
| ComfyUI 实例 | `cd /opt/comfyUI && bash start-multi-gpu.sh {status\|restart\|stop}` |
| 数据备份 | 备份 `/opt/comfyui-console/data/`（SQLite + 上传素材）即可 |
| 镜像缓存 | 构建产物自动 `docker save` 到 `/opt/docker-images/`，下次部署秒级 load |

---

## 配置说明

`backend/config.yaml.example` 为配置模板，复制为 `config.yaml` 后填写真实值（**切勿提交含密钥的 config.yaml**）：

| 配置项 | 默认值 | 说明 |
|---|---|---|
| `server.addr` | `0.0.0.0:18000` | 平台监听地址 |
| `comfy.comfy_dir` | `/opt/comfyUI` | ComfyUI 目录 |
| `comfy.base_port` | `8188` | 实例起始端口（每 GPU 递增 1） |
| `comfy.gpu_count` | `8` | GPU 实例数量 |
| `comfy.reserve_vram` | `6` | 每实例预留显存（GB） |
| `comfy.force_fp16` | `true` | 强制 FP16 |
| `comfy.enable_manager` | `true` | 仅 GPU0 启用 ComfyUI Manager |
| `comfy.mode` | `ssh` | 调度方式：`ssh`（算力节点裸进程）/ `docker`（compose 容器）/ `local`（本机裸进程） |
| `comfy.container_prefix` | `comfyui-gpu` | docker 模式容器名前缀 |
| `comfy.network` | `comfyui-console_default` | docker 模式容器网络 |
| `storage.db_path` | `/opt/comfyui-console/data/console.db` | SQLite 数据库路径 |
| `storage.data_dir` | `/opt/comfyui-console/data` | 数据目录 |
| `gpu.nvidia_smi` | `/usr/bin/nvidia-smi` | nvidia-smi 路径 |
| `gpu.monitor_interval_seconds` | `3` | GPU 监控采集间隔 |
| `remote.host` | 空 | SSH 算力节点地址（为空则本地模式） |
| `remote.port` | `22` | SSH 端口 |
| `remote.user` | `root` | SSH 用户 |
| `remote.password` | 空 | SSH 密码（推荐改用私钥） |
| `remote.private_key` | 空 | SSH 私钥路径（优先于 password） |
| `simulate` | `false` | 模拟模式（无 GPU 验证全流程） |

---

## 项目结构

```
├── backend/                       # Go 后端
│   ├── main.go                    # 入口：config → db → service → router
│   ├── config.yaml.example        # 配置模板（复制为 config.yaml 填写真实值）
│   ├── internal/
│   │   ├── api/router.go          # 路由
│   │   ├── config/                # 配置加载
│   │   ├── database/              # SQLite 初始化 + 自动迁移
│   │   ├── models/                # 数据模型 (Task/Template/Instance/UploadFile/Event)
│   │   ├── service/
│   │   │   ├── handlers.go        # API Handlers（ClearTasks / Output / MediaInfo / WS）
│   │   │   ├── task_service.go    # 任务创建/调度/渲染/WS进度/simulate
│   │   │   ├── project_service.go # 漫剧项目：剧本/分镜/视频/合并 + 状态轮询
│   │   │   ├── project_handlers.go# 漫剧项目与平台设置 Handlers
│   │   │   ├── volc_engine.go     # 火山引擎 Ark 客户端（文生文/文生图/音频/设置）
│   │   │   ├── comfy_client.go    # ComfyUI HTTP 客户端 (prompt/queue/history)
│   │   │   ├── ws_client.go       # ComfyUI WS 监听 + 前端推送 Hub
│   │   │   ├── instance_manager.go# 实例启停（SSH 调 start-multi-gpu.sh）
│   │   │   ├── gpu_monitor.go     # nvidia-smi 采集
│   │   │   ├── remote.go          # SSH/SFTP 远程执行
│   │   │   ├── media_probe.go     # MP4/图片 元数据解析
│   │   │   ├── template_seed.go   # 系统模板种子化 + 素材上传管理
│   │   │   └── templates/         # 4 个工作流模板 JSON (embed)
│   │   └── static/dist            # 前端构建产物 (embed)
│   └── deploy.sh                  # 传统二进制部署脚本
├── comfyui/                       # ComfyUI fork 代码（含 MiniMax H3 模型实现）
├── frontend/                      # Vue3 前端
│   └── src/
│       ├── views/                 # Dashboard/Tasks/Instances/Projects/Settings 等
│       ├── styles/main.css        # 苹果风格设计系统（黑白主题）
│       ├── api/index.js           # API + WS 客户端
│       └── stores/app.js
├── Dockerfile                     # console 镜像（前端 embed + Go 单二进制）
├── docker-compose.yml             # console 容器（comfyui 为宿主机裸进程）
└── deploy.sh                      # 一键部署（git pull → conda → 镜像 → 启动 → 健康检查）
```

---

## API 摘要

### 平台基础

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/health` | 健康检查 |
| GET | `/api/instances` | 实例列表（状态/队列/显存，自动探测真实状态） |
| POST | `/api/instances/:id/start · stop · restart` | 实例启停（id=GPU序号，SSH 调 start-multi-gpu.sh） |
| POST | `/api/instances/start-all · stop-all · restart-all` | 一键启停 / 重启全部（异步） |
| GET | `/api/gpus` | GPU 实时状态 |
| GET | `/api/templates` | 工作流模板（4 个） |
| GET | `/api/tasks · /api/tasks/:id` | 任务列表（分页/筛选）/ 详情 |
| POST | `/api/tasks` | 创建任务（提交后自动调度执行） |
| DELETE | `/api/tasks` | 清空已结束任务（存在活动任务时拒绝） |
| POST | `/api/tasks/:id/cancel · rerun` | 取消 / 重试 |
| POST | `/api/upload` | 素材上传 (multipart: file/type/task_id) |
| GET | `/api/output/:gpu/*path` | 结果文件（视频/音频，支持 Range/MIME/下载） |
| GET | `/api/media/:gpu/*path` | 输出文件元数据（分辨率/时长/大小/编码） |
| GET | `/api/input/:taskid/*path` | ComfyUI input 目录文件（分镜画面预览） |
| WS | `/api/ws` | 实时推送（实例/GPU快照、任务进度） |

### 平台设置（火山引擎 Ark）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/settings` | 读取设置（API Key 打码） |
| PUT | `/api/settings` | 保存设置（API Key/接口地址/模型/画面尺寸） |
| POST | `/api/settings/test-text · test-image` | 测试文生文 / 文生图连通性 |

### 漫剧项目

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST | `/api/projects` | 项目列表 / 新建项目 |
| GET/PUT/DELETE | `/api/projects/:id` | 详情（含场景与合并列表）/ 编辑 / 删除 |
| POST | `/api/projects/:id/generate` | 一键持久化流水线（剧本→画面→视频→合并） |
| POST | `/api/projects/:id/script` | 生成剧本（文生文，自动拆分场景） |
| POST | `/api/projects/:id/images` | 一键生成全部画面（文生图，限流 3 并发） |
| POST | `/api/projects/:id/videos` | 一键生成全部视频（本地 L40 i2v） |
| PATCH | `/api/projects/:id/scenes/:sid` | 编辑场景（改画面提示词自动重置下游产物） |
| POST | `/api/projects/:id/scenes/:sid/image` | 生成单场景画面 |
| POST | `/api/projects/:id/scenes/:sid/video` | 生成单场景视频 |
| POST | `/api/projects/:id/merge` | 合并选中场景为成片（ffmpeg） |
| GET | `/api/projects/:id/merges` | 合并任务列表 |

### 角色资产（Character Bible）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/projects/:id/characters` | 角色列表（含出场统计） |
| POST | `/api/projects/:id/characters` | 新建角色（手动，source=manual） |
| PUT | `/api/projects/:id/characters/:cid` | 编辑角色（name/role/trait/style） |
| DELETE | `/api/projects/:id/characters/:cid` | 删除角色 |
| POST | `/api/projects/:id/characters/:cid/portrait` | 生成/重新生成角色标准像 |
| POST | `/api/projects/:id/characters/portraits` | 一键生成全部缺标准像角色 |

### 对白配音与字幕

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/projects/:id/scenes/:sid/dialogues` | 场景对白列表 |
| POST | `/api/projects/:id/scenes/:sid/dub` | 合成场景对白配音（TTS） |
| POST | `/api/projects/:id/dub` | 一键合成项目全部对白配音 |
| GET | `/api/projects/:id/srt?episode_n=N` | 下载该集 SRT 字幕 |

---

## 测试验证

**单元测试**（`backend/internal/service/task_service_test.go`，不依赖 GPU/DB）：

- 4 个模板 `RenderWorkflow` 渲染后无残留占位符 ✅
- 复数素材展开为索引占位符（`ref_image_0`/`ref_image_1`）✅
- 必填素材缺失校验 ✅
- 实例调度排序（队列短优先 → 显存大优先）✅

```bash
cd backend && go test ./internal/service/
```

**端到端验证**（真实 8×L40 + ComfyUI）：

- 4 个模板任务全部创建成功，工作流渲染后成功提交至 ComfyUI，并 success 生成 MP4 ✅
- 自动调度到不同 GPU 并行执行，节点级进度正常推进 ✅
- 生成 MP4（1280×704 / h264 / ~5.2s），`/api/media` 返回正确元数据 ✅
- 一键 `start-all` / `stop-all` / `restart-all`：8 实例启停正常 ✅

---

## 已知问题

- ComfyUI 0.30+ 的 WS 事件为 `progress_state`/`execution_success` 格式（非旧版 `progress`/`executing.prompt`）。
- `SaveVideo.codec` 为 DynamicCombo 类型，API 提交须用字符串 `"auto"`（不可用对象格式）。
- 素材上传后存于 ComfyUI 共享 `input/<task_id>/` 目录（8 实例共用 input）。
- 平台/实例重启后，`running`/`queued` 任务由后台恢复循环自动 reconcile；`pending` 任务需手动重试。
- 模拟模式跳过渲染与 ComfyUI 提交，仅验证前端进度/状态机全流程。

---

## 开源协议

[MIT License](LICENSE) © [hequan2017](https://github.com/hequan2017)
