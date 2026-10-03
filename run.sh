#!/usr/bin/env bash
# 一条命令本地启动：厨房台面开孔复核与返工系统
# 用法:
#   ./run.sh            # 初始化（重置演示库）并启动 http://localhost:8080
#   ./run.sh --keep     # 保留已有数据启动
#   PORT=9000 ./run.sh  # 自定义端口
set -e
cd "$(dirname "$0")"
PORT="${PORT:-8080}"

# 1) 定位 / 安装 Go
if ! command -v go >/dev/null 2>&1; then
  if [ -x "$HOME/go-sdk/go/bin/go" ]; then
    export PATH="$HOME/go-sdk/go/bin:$PATH"
  elif [ -x /usr/local/go/bin/go ]; then
    export PATH="/usr/local/go/bin:$PATH"
  else
    echo ">> 未检测到 Go，开始下载 Go 1.23.4 到 \$HOME/go-sdk ..."
    curl -fsSL -o /tmp/go.tar.gz https://go.dev/dl/go1.23.4.linux-amd64.tar.gz
    mkdir -p "$HOME/go-sdk" && tar -C "$HOME/go-sdk" -xzf /tmp/go.tar.gz
    export PATH="$HOME/go-sdk/go/bin:$PATH"
  fi
fi

# 2) 拉取依赖并编译
echo ">> 编译后端（Gin + GORM + SQLite 纯 Go 驱动）..."
go mod download
go build -o countertop-app .

mkdir -p uploads

# 3) 启动（首次带 -reset -seed，写入四个演示工单）
if [ "$1" = "--keep" ]; then
  echo ">> 保留现有数据启动"
  exec ./countertop-app -addr ":$PORT"
else
  echo ">> 以演示数据启动（超差 / 客户未确认 / 加工后换版 / 返工 四例）"
  exec ./countertop-app -addr ":$PORT" -reset -seed
fi
