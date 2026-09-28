[简体中文](README.md) | [English](README.en.md)

<p align="center">
  <a href="https://hequan2017.github.io/new-aigc-comfyui-minimax-h3/"><strong>🌐 Live Frontend Preview</strong></a>
  ·
  <a href="https://github.com/hequan2017/new-aigc-comfyui-minimax-h3/actions/workflows/deploy-pages.yml">Build Status</a>
</p>

# ComfyStudio · ComfyUI Multi-GPU Management Platform

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-blue)](backend/go.mod)
[![Vue](https://img.shields.io/badge/Vue-3-green)](frontend/package.json)

ComfyStudio is a **Go + Vue3** based ComfyUI multi-GPU management platform: it manages **8×NVIDIA L40** compute nodes and ships with built-in **MiniMax H3** workflows (text-to-video / image-to-video / first-last frame / reference video / character locking), automatically scheduling jobs to the idlest GPU. The frontend shows **node-level generation progress** and GPU usage in real time, with in-browser preview of results and video metadata parsing.

The platform also includes an **AI Comic-Drama Workbench**: a single pipeline that takes a drama from "creative plan → episodic script → storyboard images → scene videos → merged film". It supports **multi-episode production**, a **character asset library** (consistent characters across scenes), a **prop/scene asset library** (consistent props and environments across storyboards), **character voice binding with TTS dubbing** (Alibaba Cloud Model Studio), and **SRT subtitles**.

**Who it is for**: content teams that own a multi-GPU server and want to mass-produce MiniMax H3 videos (short dramas / comic dramas / marketing assets), as well as solo creators who want to manage the whole "scheduling + assets + scripts + dubbing" flow in one UI.

## ✨ Features

### Platform Basics

| Capability | Details |
|---|---|
| Multi-GPU scheduling | 8×L40 (configurable); jobs are auto-assigned to the GPU with the **shortest queue + most free VRAM** |
| Real-time progress | Node-level ComfyUI progress pushed over WS (`progress_state`), weighted by template nodes, capped at 99% while generating |
| Workflow templates | 5 built-in MiniMax H3 templates: t2v / i2v / first-last frame / ref2v / character locking (simplified ref2v), with placeholder rendering, multi-asset expansion and automatic node pruning |
| Instance management | Per-instance start/stop/restart (SSH calls `start-multi-gpu.sh`), one-click start/stop/restart for all (async) |
| GPU dashboard | Real-time monitoring of temperature / power / VRAM / utilization / processes for all 8 GPUs (nvidia-smi, 3s default interval) |
| Job management | List filtering / cancel / requeue / retry / cancel all / clear finished jobs |
| Job auto-recovery | Background loop reconciles stuck `running`/`queued` jobs; falls back to `output_workers/gpuN/` when ComfyUI history is lost |
| Material library | Storyboard images are auto-ingested on success, plus manual upload management |
| Result preview | In-browser preview (HTTP Range seeking) + download; MP4/image metadata parsed in pure Go, no ffprobe needed |
| Platform settings | Volcano Engine Ark (text generation / image generation) + Alibaba Cloud Model Studio (TTS) API keys and models, with connectivity tests and masked key display |
| Concurrency gate | Global video-generation concurrency `video_concurrency` (default 4) and resolution `video_resolution`, adjustable in the UI |
| Simulation mode | With `simulate: true`, jobs progress per template reference durations — full flow works without any GPU |

### AI Comic-Drama Workbench

| Capability | Details |
|---|---|
| One-click pipeline | Creative plan → script → storyboard images → scene videos → merged film; recoverable after service restart |
| Multi-episode | The creative plan targets an episode count; scripts/scenes are generated per episode and SRT subtitles download per episode |
| Character asset library | Character cards (trait/style/portrait) are auto-extracted, idempotent on re-extraction; portraits can be generated via text-to-image or uploaded; ref2v locks characters for cross-scene consistency |
| Prop/scene assets | Key props and main scenes automatically get cards + reference images (generated or uploaded); storyboard images inject references via `location`/`props` to lock appearance |
| Consistency gating | Storyboard generation waits for referenced asset references to be ready; the video stage waits for all asset references |
| Character-locked video | With a portrait, uses ref2v / ref2v_single (storyboard + protagonist portrait, 2 images to lock identity); falls back to i2v when no character exists |
| Character voice binding | Preset voices, or upload a 10–20 second voice sample registered via `qwen-voice-enrollment` for a cloned voice consistent across the whole drama |
| TTS dubbing | Alibaba Cloud Model Studio `qwen3-tts-flash` (male/female voices + per-character voice mapping) synthesizes dialogue; single-line re-dubbing supported |
| SRT subtitles | Timeline generated from real audio durations (or scene durations as placeholder before dubbing is ready); UTF-8 BOM for Windows player compatibility |
| Scene editor | Scene ordering, duration tuning, dialogue editing, per-scene image/video regeneration and cancel |
| Video merging | ffmpeg concatenates scene videos in order into the final film keeping the audio track (libx264/AAC); single-episode and merge-all supported |
| Skills page | End-to-end comic-drama methodology; includes a micro short-drama scriptwriting Skill (genre / opening hooks / paywalls / pacing / villains reference docs) |

## 🛠 Tech Stack

| Layer | Technology |
|---|---|
| Backend | Go 1.25 + Gin v1.12 + GORM v1.31 (SQLite via the glebarez pure-Go driver) + gorilla/websocket v1.5 + pkg/sftp v1.13 |
| Frontend | Vue 3.5 + Vite 6 + Pinia 2 + Vue Router 4 + axios (no UI library, dark/light theme toggle) |
| Database | SQLite (`/opt/comfyui-console/data/console.db`) |
| Inference | ComfyUI (with the built-in `nodes_minimax_h3.py` MiniMax H3 implementation, 8×NVIDIA L40 bare processes) |
| Cloud services | Volcano Engine Ark (deepseek-v4 text / doubao-seedream-5.0 image), Alibaba Cloud Model Studio (qwen3-tts-flash / qwen-voice-enrollment) |
| Deployment | Single binary (frontend embedded), Docker or bare process |

## 🚀 Quick Start

### Deployment Modes

| Mode | Description | Use case |
|---|---|---|
| console container + bare ComfyUI processes (default) | console runs in Docker (:18000); ComfyUI instances are managed by the host's `start-multi-gpu.sh`; console operates via SSH/SFTP | Production (recommended) |
| All containers | `comfy.mode: docker`; console runs `docker compose` on the host over SSH to manage `comfyui-gpu{N}` containers (CDI GPU binding) | Container isolation |
| Local mode | `comfy.mode: local`, leave `remote.host` empty | Single-machine dev/test |

**Requirements**: Linux + NVIDIA GPUs (8 by default, configurable), Docker + compose v2, conda (miniconda3), MiniMax H3 model weights.

### 1. Deploy ComfyUI and Model Weights

```bash
git clone https://github.com/hequan2017/new-aigc-comfyui-minimax-h3.git
cd new-aigc-comfyui-minimax-h3

conda create -n comfyenv python=3.11 -y          # deps are installed incrementally by deploy.sh
mkdir -p /opt/comfyUI && cp -r comfyui/* /opt/comfyUI/
mkdir -p /opt/comfyUI/models/{diffusion_models,text_encoders,vae}
scp shell/start-multi-gpu.sh root@<server-ip>:/opt/comfyUI/start-multi-gpu.sh   # edit paths/GPU count at the top of the script
```

MiniMax H3 needs **5 weight files** (exact filenames; fp8 weights are quantized automatically on load):

| Weight file | Directory | Purpose |
|---|---|---|
| `minimax_h3_fl2va_bf16.safetensors` | `models/diffusion_models/` | t2v / i2v / first-last frame DiT |
| `minimax_h3_ref2va_bf16.safetensors` | `models/diffusion_models/` | Reference video (ref2v) DiT |
| `qwen3vl_32b_minimax_h3_bf16.safetensors` | `models/text_encoders/` | Qwen3-VL-32B text/vision encoder |
| `minimax_h3_video_vae_fp16.safetensors` | `models/vae/` | Video VAE |
| `minimax_h3_audio_vae_fp32.safetensors` | `models/vae/` | Audio VAE (32kHz) |

> H3 is a joint video+audio generation model — one sampling pass produces both the video and its audio track; frame counts snap to the `17k+5` grid (124–362 frames ≈ 5–15 seconds at 24fps); each fp8-quantized instance needs roughly 22–24GB of VRAM.

### 2. Create the Config

```bash
cp backend/config.yaml.example backend/config.yaml
```

```yaml
remote:
  host: "<compute-node-ip>"   # leave empty for local mode
  port: 22
  user: root
  private_key: "/opt/comfyui-console/ssh_key"   # private key takes priority over password
comfy:
  comfy_dir: /opt/comfyUI   # must match the host directory
simulate: false
```

Common options (see `backend/config.yaml.example` for everything):

| Option | Default | Description |
|---|---|---|
| `server.addr` | `0.0.0.0:18000` | Platform listen address |
| `comfy.gpu_count` | `8` | Number of GPU instances |
| `comfy.base_port` | `8188` | First instance port (increments per GPU, 8188–8195) |
| `comfy.reserve_vram` | `6` | Reserved VRAM per instance (GB) |
| `comfy.mode` | `ssh` | `ssh` / `docker` / `local` |
| `storage.db_path` | `/opt/comfyui-console/data/console.db` | SQLite path |
| `gpu.monitor_interval_seconds` | `3` | GPU monitor interval |
| `simulate` | `false` | Simulation mode (full flow without GPUs) |

### 3. One-Click Deploy (Docker)

```bash
bash deploy.sh               # git pull → conda deps → build image → start → health check
bash deploy.sh --no-gpu      # when there is no GPU / no NVIDIA toolkit
```

When deployment finishes, open `http://<server-ip>:18000` in a browser and verify:

```bash
curl http://127.0.0.1:18000/api/health
cd /opt/comfyUI && bash start-multi-gpu.sh status
```

**Live preview (GitHub Pages)**: pushing to `main` triggers Actions to build the frontend and publish it to [https://hequan2017.github.io/new-aigc-comfyui-minimax-h3/](https://hequan2017.github.io/new-aigc-comfyui-minimax-h3/) with no repo secrets required; Pages is a static preview (hash routing, no backend) — deploy locally for full functionality.

**Operations**: to upgrade, re-run `bash deploy.sh` (the image is force-rebuilt every time); logs via `docker logs -f console`; back up `/opt/comfyui-console/data/` and you are done.

## 📁 Directory Layout

```
├── backend/                  # Go backend (single binary, frontend embedded)
│   ├── config.yaml.example   # config template
│   └── internal/
│       ├── api/router.go     # routes
│       ├── models/           # Task/Template/Project/Scene/Character/Asset/Material etc.
│       └── service/          # job scheduling / drama pipeline / Volc Ark / Aliyun TTS / SSH / WS / GPU monitor
│           └── templates/    # 5 workflow template JSON files (embed)
├── comfyui/                  # ComfyUI fork (with the MiniMax H3 model implementation)
├── frontend/                 # Vue3 frontend (Dashboard/Tasks/Instances/Projects/Materials/Skills/Settings)
├── shell/start-multi-gpu.sh  # multi-GPU ComfyUI start/stop script (deploy to the host)
├── docker-compose.yml        # console container orchestration
├── Dockerfile                # multi-stage build (frontend embed + Go single binary)
└── deploy.sh                 # one-click deploy
```

## 📸 Screenshots

<p align="center">
  <img src="docs/screenshots/projects.png" alt="Projects" width="49%"/>
  <img src="docs/screenshots/project-detail.png" alt="Project detail" width="49%"/>
  <br/>
  <img src="docs/screenshots/project-editor.png" alt="Project editor" width="49%"/>
  <img src="docs/screenshots/tasks.png" alt="Tasks" width="49%"/>
  <br/>
  <img src="docs/screenshots/instances.png" alt="Instances" width="49%"/>
  <img src="docs/screenshots/settings.png" alt="Settings" width="49%"/>
  <br/>
  <img src="docs/screenshots/dashboard.png" alt="Dashboard" width="49%"/>
</p>

## 🔗 Related Projects

- [minimax-h3-manager](https://github.com/hequan2017/minimax-h3-manager) — the same author's MiniMax-H3 unified inference service: vllm-omni dual backends (FL2VA / Ref2VA) + FastAPI gateway + Chinese web console, exposing H3 generation through a standard Video Generation API. Complementary to this platform's ComfyUI inference path.

## 📄 License

[MIT License](LICENSE) © [hequan2017](https://github.com/hequan2017)
