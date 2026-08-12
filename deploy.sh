#!/bin/bash
# ============================================================
# ComfyUI Console 一键 Docker 部署（全链路中国源）
#   适用: 算力服务器 (8×NVIDIA L40)
#
#   用法:
#     bash deploy.sh            # 标准部署 (console + 8×comfyui 容器)
#     bash deploy.sh --no-gpu   # 无 GPU 环境（跳过 GPU 设备声明）
#     bash deploy.sh --rebuild  # 显式重建参数（默认每次发版已强制重建镜像）
#
#   自动完成:
#     1. 检查 Docker / NVIDIA Container Toolkit (CDI)
#     2. 准备 conda 依赖（复用宿主 comfyenv 环境，增量 pip install）
#     3. 构建 comfyui:latest (轻量镜像, 复用宿主 conda 运行环境) 与 comfyui-console:latest
#        （每次发版强制重建镜像，保证代码最新；构建成功后 docker save 备份，免去重复打包）
#     3. 校验数据目录 (models 485G 等) 与 config.yaml
#     4. 启动 compose: console + comfyui-gpu0~7 (CDI 绑卡, 共享挂载 models)
#     5. 健康检查: 平台 API + ComfyUI 实例 /system_stats
# ============================================================

# 兼容 `sh deploy.sh` 调用
if [ -z "${BASH_VERSION:-}" ]; then
  exec bash "$0" "$@"
fi

set -euo pipefail

PORT=18000
CONSOLE_DIR=/opt/comfyui-console
DATA_DIR=${CONSOLE_DIR}/data
CONFIG_FILE=${CONSOLE_DIR}/config.yaml
COMFY_DIR=/opt/comfyUI
IMAGE_DIR=/opt/docker-images
COMFY_IMAGE_TAR=${IMAGE_DIR}/comfyui.tar.gz
CONSOLE_IMAGE_TAR=${IMAGE_DIR}/comfyui-console.tar.gz

USE_GPU=1
REBUILD=0
for arg in "$@"; do
  case "$arg" in
    --no-gpu) USE_GPU=0 ;;
    --rebuild) REBUILD=1 ;;
    *) die "未知参数: $arg" ;;
  esac
done

info() { echo -e "\033[1;34m==> $*\033[0m"; }
ok()   { echo -e "\033[1;32m[OK] $*\033[0m"; }
warn() { echo -e "\033[1;33m[!] $*\033[0m"; }
die()  { echo -e "\033[1;31m[ERR] $*\033[0m" >&2; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ---------- 0. 更新项目代码 ----------
info "更新项目代码 (git pull)..."
if [ -d "${SCRIPT_DIR}/.git" ]; then
  if git -C "$SCRIPT_DIR" pull origin master 2>&1 | sed 's/^/  /'; then
    # pull 可能更新了 deploy.sh 自身，重新加载新脚本继续部署（DEPLOY_RELOADED 防死循环）
    if [ "${DEPLOY_RELOADED:-}" != "1" ]; then
      info "代码已更新，重新加载 deploy.sh 继续部署..."
      export DEPLOY_RELOADED=1
      exec bash "$0" "$@"
    fi
  else
    die "git pull 失败，请检查网络/凭证"
  fi
else
  warn "未找到 git 仓库（${SCRIPT_DIR}），跳过代码更新"
fi

# ---------- 0.1 清理旧部署（裸进程模式） ----------
if pgrep -f "console-linux-amd64" >/dev/null 2>&1; then
  info "停止旧版裸进程 console..."
  pkill -f "console-linux-amd64" || true
  sleep 2
fi
# ComfyUI 实例为宿主机裸进程（start-multi-gpu.sh 管理），发版不中断，部署完成后校验/启动
# 清理残留的旧版本容器（compose 升级时原名可能冲突）
docker rm -f console comfyui-gpu0 comfyui-gpu1 comfyui-gpu2 comfyui-gpu3 \
  comfyui-gpu4 comfyui-gpu5 comfyui-gpu6 comfyui-gpu7 2>/dev/null || true

# ---------- 1. 环境检查 ----------
[ "$(id -u)" = "0" ] || die "请用 root 运行"

info "检查 Docker..."
command -v docker >/dev/null 2>&1 || die "未安装 Docker，请先执行: curl -fsSL https://get.docker.com | sh"
docker compose version >/dev/null 2>&1 || die "缺少 docker compose 插件（v2）"

# ---------- 2. GPU / nvidia-container-toolkit 检查 ----------
if [ "$USE_GPU" = "1" ]; then
  info "检查 GPU / nvidia-container-toolkit (CDI)..."
  command -v nvidia-smi >/dev/null 2>&1 || die "未找到 nvidia-smi，请确认 NVIDIA 驱动已安装，或使用 --no-gpu"
  if docker info 2>/dev/null | grep -q "nvidia.com/gpu"; then
    ok "CDI GPU 设备就绪"
  elif docker info 2>/dev/null | grep -qi nvidia || command -v nvidia-ctk >/dev/null 2>&1; then
    warn "未检测到 CDI 设备(nvidia.com/gpu)，尝试生成..."
    nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml 2>/dev/null \
      && ok "已生成 CDI 配置" \
      || warn "CDI 生成失败，GPU 分配可能不可用"
  else
    warn "未检测到 nvidia-container-toolkit，容器将无法访问 GPU"
    echo
    echo "  安装方法（Ubuntu/Debian）:"
    echo "    curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg"
    echo "    curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' | tee /etc/apt/sources.list.d/nvidia-container-toolkit.list"
    echo "    apt-get update && apt-get install -y nvidia-container-toolkit"
    echo "    nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml && systemctl restart docker"
    echo
    read -rp "是否跳过 GPU 支持继续部署? [y/N] " ans
    case "$ans" in
      y|Y|yes) USE_GPU=0 ;;
      *) exit 1 ;;
    esac
  fi
fi

# ---------- 3. 准备宿主机目录 / 配置 ----------
info "准备宿主机目录..."
mkdir -p "$DATA_DIR"
if [ ! -f "$CONFIG_FILE" ]; then
  cp "${SCRIPT_DIR}/backend/config.yaml.example" "$CONFIG_FILE"
  ok "已生成默认配置: ${CONFIG_FILE}（可按需修改后重启容器）"
fi
[ -d "$COMFY_DIR/models" ] || die "未找到 ${COMFY_DIR}/models（模型目录，必须存在）"
ok "模型目录: ${COMFY_DIR}/models ($(du -sh "$COMFY_DIR/models" 2>/dev/null | cut -f1))"

# ---------- 3.5 准备 conda 依赖（复用宿主环境，增量安装缺失包） ----------
CONDA_PY=/opt/miniconda3/envs/comfyenv/bin/python
if [ ! -x "$CONDA_PY" ]; then
  warn "未找到 conda 环境 ${CONDA_PY}，请先部署依赖环境"
  die "缺少 ComfyUI conda 环境，无法部署"
fi
info "增量安装 comfyui 依赖到 conda 环境（已存在包自动跳过）..."
"$CONDA_PY" -m pip install -r "${SCRIPT_DIR}/comfyui/requirements.txt" \
  -i https://pypi.tuna.tsinghua.edu.cn/simple
ok "conda 依赖就绪"

# ---------- 4. 构建镜像（每次发版强制重建，保证前端/后端代码总是最新） ----------
cd "$SCRIPT_DIR"
mkdir -p "$IMAGE_DIR"
export DOCKER_BUILDKIT=1

build_comfyui() {
  info "构建镜像 comfyui:latest（轻量镜像，复用宿主 conda，1-2 分钟）..."
  docker build -f comfyui/Dockerfile -t comfyui:latest .
  info "保存镜像备份 → ${COMFY_IMAGE_TAR}（下次部署秒级 load）..."
  docker save comfyui:latest | gzip > "$COMFY_IMAGE_TAR"
  ok "comfyui 镜像备份完成"
}

build_console() {
  info "构建镜像 comfyui-console:latest（前端 embed + Go 单二进制）..."
  docker build -f Dockerfile -t comfyui-console:latest .
  info "保存镜像备份 → ${CONSOLE_IMAGE_TAR}..."
  docker save comfyui-console:latest | gzip > "$CONSOLE_IMAGE_TAR"
  ok "console 镜像备份完成"
}

if [ "$REBUILD" = "1" ]; then
  warn "显式 --rebuild（当前版本默认每次发版均强制重建镜像）"
fi
build_comfyui
build_console

# ---------- 5. 启动 ----------
info "启动 compose（console + 8×comfyui 容器）..."
if [ "$USE_GPU" = "1" ]; then
  docker compose -f docker-compose.yml up -d
else
  docker compose -f docker-compose.yml -f docker-compose.nogpu.yml up -d
fi

# ---------- 6. 健康检查 ----------
info "等待平台就绪..."
for i in $(seq 1 30); do
  if curl -fsS -m 3 "http://127.0.0.1:${PORT}/api/health" 2>/dev/null | grep -q '"ok"'; then
    ok "平台就绪: http://<服务器IP>:${PORT}"
    break
  fi
  sleep 2
  [ "$i" = "30" ] && die "平台启动超时，请执行: docker compose logs console 排查"
done

# 确保 ComfyUI 实例（宿主机裸进程，start-multi-gpu.sh 管理）运行
info "检查 ComfyUI 实例（宿主机裸进程）..."
if pgrep -f "main.py.*--port 81" >/dev/null 2>&1; then
  ok "ComfyUI 实例已在运行"
else
  info "启动 ComfyUI 实例（start-multi-gpu.sh start）..."
  bash "${COMFY_DIR}/start-multi-gpu.sh" start \
    || warn "实例启动失败，请手动执行: cd ${COMFY_DIR} && bash start-multi-gpu.sh start"
fi

info "等待 ComfyUI 实例就绪..."
for gpu in $(seq 0 7); do
  port=$((8188 + gpu))
  for i in $(seq 1 36); do
    if curl -fsS -m 3 "http://127.0.0.1:${port}/system_stats" >/dev/null 2>&1; then
      ok "comfyui-gpu${gpu} (端口 ${port}) 就绪"
      break
    fi
    sleep 5
    [ "$i" = "36" ] && warn "comfyui-gpu${gpu} 启动超时，请查看: tail ${COMFY_DIR}/logs/comfyui-gpu${gpu}.log"
  done
done

ok "部署完成: http://<服务器IP>:${PORT}"
ok "查看日志: docker compose logs -f"

# ---------- 7. 维护发版记录（release.json：编号+1、时间、功能说明） ----------
info "维护发版记录 release.json..."
RELEASE_FILE=${DATA_DIR}/release.json
PREV_NUM=0
if [ -f "$RELEASE_FILE" ]; then
  PREV_NUM=$(sed -n 's/.*"number"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p' "$RELEASE_FILE" | head -1)
fi
[ -n "$PREV_NUM" ] || PREV_NUM=0
NEW_NUM=$((PREV_NUM + 1))
NOW=$(date '+%Y-%m-%d %H:%M:%S')
CUR_COMMIT=$(git -C "$SCRIPT_DIR" rev-parse --short HEAD 2>/dev/null || echo "")
PREV_COMMIT=$(cat "${DATA_DIR}/release.prev_commit" 2>/dev/null || echo "")
if [ -n "$PREV_COMMIT" ] && [ -n "$CUR_COMMIT" ] && [ "$PREV_COMMIT" != "$CUR_COMMIT" ]; then
  # 本次发版功能 = 自上次发版 commit 以来的提交标题
  FEATURES=$(git -C "$SCRIPT_DIR" log --pretty=format:%s "$PREV_COMMIT"..HEAD 2>/dev/null | sed '/^$/d' || true)
else
  # 首次发版：取最近 10 条提交作为功能说明
  FEATURES=$(git -C "$SCRIPT_DIR" log --pretty=format:%s -10 HEAD 2>/dev/null | sed '/^$/d' || true)
fi
if [ -z "$FEATURES" ]; then
  FEATURES="平台代码更新"
fi
# 经环境变量传入 python（避免 heredoc 变量展开问题）
export RELEASE_NUM="$NEW_NUM" RELEASE_NOW="$NOW" RELEASE_COMMIT="$CUR_COMMIT" RELEASE_FEATURES="$FEATURES" RELEASE_FILE="$RELEASE_FILE"
"$CONDA_PY" - <<'PYEOF'
import json, os
features = [f.strip() for f in os.environ.get("RELEASE_FEATURES", "").splitlines() if f.strip()]
with open(os.environ["RELEASE_FILE"], "w", encoding="utf-8") as f:
    json.dump({
        "number": int(os.environ["RELEASE_NUM"]),
        "time": os.environ["RELEASE_NOW"],
        "commit": os.environ["RELEASE_COMMIT"],
        "features": features,
    }, f, ensure_ascii=False, indent=2)
PYEOF
if [ -n "$CUR_COMMIT" ]; then
  echo "$CUR_COMMIT" > "${DATA_DIR}/release.prev_commit"
fi
ok "发版记录 #${NEW_NUM} 已写入 ${RELEASE_FILE}"

ok "全部完成"
