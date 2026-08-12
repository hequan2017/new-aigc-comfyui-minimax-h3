#!/bin/bash
# =============================================================================
# ComfyUI 多卡多实例启动脚本 (8 × NVIDIA L40)
# 设计：每张 GPU 一个独立 ComfyUI 实例
#   - 端口 8188 ~ 8195 对应 GPU 0 ~ 7
#   - 每实例独立 user/temp/output 目录 + 独立 sqlite 数据库（避免多进程争用）
#   - 仅 GPU 0 启用 ComfyUI-Manager（--enable-manager），避免多进程同时改插件
# 搭配 submit.py 实现"任务自动分配到空闲显卡"。
# 用法: bash start-multi-gpu.sh {start|stop|restart|status}
# 注意: 部署到生产前按实际环境修改下方 COMFY_DIR / CONDA_SH / GPU_COUNT
# =============================================================================
set -u

COMFY_DIR=/opt/comfyUI
CONDA_SH=/opt/miniconda3/etc/profile.d/conda.sh
CONDA_ENV=comfyenv
BASE_PORT=8188
GPU_COUNT=8
RESERVE_VRAM=6
LOG_DIR="$COMFY_DIR/logs"

# 端口范围对应的正则（8188~8195），用于精确 stop/pgrep
PORT_RE='818[89]|819[0-5]'

# ----------------------------------------------------------------------------
# 确保进入 comfyenv（已激活则跳过，避免重复 source）
# ----------------------------------------------------------------------------
ensure_conda() {
    local expect="/opt/miniconda3/envs/${CONDA_ENV}/bin/python"
    if [ -n "${CONDA_PREFIX:-}" ] && [ "$(command -v python 2>/dev/null)" = "$expect" ]; then
        return 0
    fi
    if [ ! -f "$CONDA_SH" ]; then
        echo "[ERROR] 找不到 conda: $CONDA_SH"; exit 1
    fi
    # shellcheck disable=SC1090
    source "$CONDA_SH"
    conda activate "$CONDA_ENV"
}

port_of() { echo $((BASE_PORT + $1)); }

# 指定端口实例是否在运行
is_running() { pgrep -f -- "main.py.*--port $1 " >/dev/null 2>&1; }

# ----------------------------------------------------------------------------
# 启动单个 GPU 实例
# ----------------------------------------------------------------------------
start_one() {
    local gpu=$1 port user_dir temp_dir output_dir manager_args
    port=$(port_of "$gpu")
    user_dir="$COMFY_DIR/user_workers/gpu${gpu}"
    temp_dir="$COMFY_DIR/temp_workers/gpu${gpu}"
    output_dir="$COMFY_DIR/output_workers/gpu${gpu}"
    mkdir -p "$user_dir" "$temp_dir" "$output_dir" "$LOG_DIR"

    if is_running "$port"; then
        echo "[SKIP]  GPU ${gpu} 已在运行 (端口 ${port})"
        return 0
    fi

    manager_args=""
    if [ "$gpu" -eq 0 ]; then
        manager_args="--enable-manager"
    fi

    nohup env CUDA_VISIBLE_DEVICES="$gpu" \
        python main.py \
        --listen 0.0.0.0 \
        --port "$port" \
        --reserve-vram "$RESERVE_VRAM" \
        --force-fp16 \
        --user-directory "$user_dir" \
        --temp-directory "$temp_dir" \
        --output-directory "$output_dir" \
        --database-url "sqlite:///$user_dir/comfyui.db" \
        $manager_args \
        > "$LOG_DIR/comfyui-gpu${gpu}.log" 2>&1 &

    echo "[START] GPU ${gpu} -> 端口 ${port} (pid $!)"
}

start_all() {
    ensure_conda
    cd "$COMFY_DIR" || { echo "[ERROR] ComfyUI 目录不存在: $COMFY_DIR"; exit 1; }
    echo "[INFO]  python: $(command -v python) ($(python --version 2>&1))"
    local gpu
    for gpu in $(seq 0 $((GPU_COUNT - 1))); do
        start_one "$gpu"
        sleep 2   # 错开启动，平滑显存/内存峰值
    done
    echo "[DONE]  已启动 ${GPU_COUNT} 个实例，端口 ${BASE_PORT}-$((BASE_PORT + GPU_COUNT - 1))"
    echo "[INFO]  日志: ${LOG_DIR}/comfyui-gpu*.log"
}

stop_all() {
    if pgrep -f -- "main.py.*--port (${PORT_RE}) " >/dev/null 2>&1; then
        echo "[STOP]  停止多实例 ComfyUI (端口 ${BASE_PORT}-$((BASE_PORT + GPU_COUNT - 1))) ..."
        pkill -f -- "main.py.*--port (${PORT_RE}) "
        sleep 5
        if pgrep -f -- "main.py.*--port (${PORT_RE}) " >/dev/null 2>&1; then
            echo "[WARN]  部分进程未退出，强制结束 ..."
            pkill -9 -f -- "main.py.*--port (${PORT_RE}) "
            sleep 2
        fi
        echo "[STOP]  已停止"
    else
        echo "[INFO]  没有运行中的多实例 ComfyUI"
    fi
}

status_all() {
    ensure_conda
    printf "%-5s %-7s %-9s %-7s %-16s\n" "GPU" "PORT" "PID" "QUEUE" "VRAM_FREE"
    printf "%-5s %-7s %-9s %-7s %-16s\n" "---" "----" "---" "-----" "---------"
    local gpu port pid qlen vfree
    for gpu in $(seq 0 $((GPU_COUNT - 1))); do
        port=$(port_of "$gpu")
        pid="-"; qlen="-"; vfree="-"
        if is_running "$port"; then
            pid=$(pgrep -f -- "main.py.*--port ${port} " | head -1)
            qlen=$(curl -s --max-time 3 "http://127.0.0.1:${port}/queue" \
                | python -c "import sys,json;d=json.load(sys.stdin);print(len(d.get('queue_running',[]))+len(d.get('queue_pending',[])))" 2>/dev/null || echo "?")
            vfree=$(curl -s --max-time 3 "http://127.0.0.1:${port}/system_stats" \
                | python -c "import sys,json;d=json.load(sys.stdin);v=d['devices'][0]['vram_free'];print(f'{v/1024**3:.1f}GB')" 2>/dev/null || echo "?")
        fi
        printf "%-5s %-7s %-9s %-7s %-16s\n" "$gpu" "$port" "${pid:0:9}" "$qlen" "$vfree"
    done
}

case "${1:-start}" in
    start)   start_all ;;
    stop)    stop_all ;;
    restart) stop_all; sleep 2; start_all ;;
    status)  status_all ;;
    *)
        echo "用法: bash $0 {start|stop|restart|status}"
        exit 1
        ;;
esac
