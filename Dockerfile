# ============================================================
# ComfyUI Console · Docker 多阶段构建（全链路中国源）
#   基础镜像  : docker.m.daocloud.io 国内加速镜像（避免 Docker Hub 拉取失败）
#   前端 npm  : registry.npmmirror.com（淘宝镜像）
#   Go 依赖   : GOPROXY=goproxy.cn,direct
#   构建缓存  : BuildKit cache mount 复用 npm / go mod 缓存
# ============================================================
# syntax=docker/dockerfile:1

# ---------- 阶段 1：构建前端 ----------
FROM docker.m.daocloud.io/library/node:22-alpine AS frontend
RUN npm config set registry https://registry.npmmirror.com
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

# ---------- 阶段 2：构建后端（前端产物 embed 进单二进制） ----------
FROM docker.m.daocloud.io/library/golang:1.25-alpine AS backend
ENV GOPROXY=https://goproxy.cn,direct \
    CGO_ENABLED=0
WORKDIR /app/backend
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY backend/ ./
COPY --from=frontend /app/frontend/dist ./internal/static/dist
RUN go build -ldflags="-s -w" -o /out/console .

# ---------- 阶段 3：运行镜像 ----------
# 使用 glibc 发行版（Debian slim）：宿主机挂载的 nvidia-smi 为 glibc 二进制；
# 容器自带 python 作为 ComfyUI 实例的兜底解释器（生产建议挂载宿主机 conda 环境）。
FROM docker.m.daocloud.io/library/python:3.11-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends procps curl bash ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
COPY --from=backend /out/console /usr/local/bin/console
WORKDIR /app
EXPOSE 18000
CMD ["console"]
