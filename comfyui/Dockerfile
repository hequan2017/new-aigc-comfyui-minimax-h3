# ============================================================
# ComfyUI 容器镜像（依赖复用宿主 conda 环境，镜像内不装 torch）
#   基础镜像  : docker.m.daocloud.io 国内加速镜像
#   python    : 运行用宿主挂载的 /opt/miniconda3/envs/comfyenv
#               （镜像只提供 libgl1 等系统运行库，镜像构建秒级）
#   模型/素材 : 全部由 docker-compose 挂载宿主 /opt/comfyUI 目录
#   依赖增量  : deploy.sh 在宿主机用 conda python 增量 pip install（幂等）
# ============================================================
FROM docker.m.daocloud.io/library/python:3.11-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        libgl1 libglib2.0-0 libsm6 libxext6 libxrender1 \
        ffmpeg curl git procps ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime

WORKDIR /opt/comfyUI

# ComfyUI 代码（models/custom_nodes/input/output 等数据目录由 compose 挂载覆盖）
COPY comfyui/ ./

# 清理仓库无关文件
RUN rm -rf .git .github .ci tests tests-unit .gitattributes .coderabbit.yaml .spectral.yaml .gitignore \
    && find . -name "__pycache__" -type d -prune -exec rm -rf {} +

EXPOSE 8188
ENTRYPOINT ["/opt/miniconda3/envs/comfyenv/bin/python", "main.py"]
