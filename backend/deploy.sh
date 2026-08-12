#!/bin/bash
# 构建并部署 ComfyUI Console 到 GPU 服务器
set -e
cd "$(dirname "$0")"

SERVER=your-gpu-server-ip
REMOTE_DIR=/opt/comfyui-console

echo "==> 构建前端..."
cd ../frontend
npm run build
cd ../backend

echo "==> 同步前端产物到 embed..."
rm -rf internal/static/dist
cp -r ../frontend/dist internal/static/dist

echo "==> 交叉编译 Linux 版本..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o console-linux-amd64 .

echo "==> 上传到服务器..."
cp config.yaml.example config.yaml
scp -o StrictHostKeyChecking=no console-linux-amd64 config.yaml root@${SERVER}:${REMOTE_DIR}/

echo "==> 重启服务..."
ssh -o StrictHostKeyChecking=no root@${SERVER} "cd ${REMOTE_DIR} && pkill -f console-linux-amd64 || true; sleep 1; nohup ./console-linux-amd64 > console.log 2>&1 & sleep 2; curl -s http://127.0.0.1:18000/api/health && echo ' [OK]'"

echo "==> 完成"
