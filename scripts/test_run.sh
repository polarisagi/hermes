#!/bin/bash

# 本地打包编译并重启测试的脚本

set -e

echo "======================================"
echo "🎨 正在编译前端样式..."
echo "======================================"
(cd web && npm install && npm run build)

echo "======================================"
echo "🚀 正在编译 PolarisAGI Hermes 后端..."
echo "======================================"

# 编译到 tmp_bin 以免污染全局 bin
mkdir -p tmp_bin
go build -o tmp_bin/hermes ./cmd/hermes

echo "✅ 编译成功！"

echo "======================================"
echo "🛑 停止旧的网关进程..."
echo "======================================"

# 查找并杀死正在运行的旧进程
PID=$(pgrep -f "tmp_bin/hermes" || true)

if [ -n "$PID" ]; then
    echo "找到旧进程 PID: $PID，正在关闭..."
    kill -9 $PID
    echo "旧进程已成功停止。"
else
    echo "未发现运行中的旧进程。"
fi

echo "======================================"
echo "✨ 启动新的网关实例..."
echo "======================================"

# 如果没有测试配置文件，则基于线上配置生成一份，并将端口改为 27779
if [ ! -f config.test.toml ]; then
    if [ -f config.toml ]; then
        cp config.toml config.test.toml
        # 兼容 macOS 和 Linux 的 sed 替换
        sed -i.bak 's/listen_addr = "127.0.0.1:27777"/listen_addr = "127.0.0.1:27779"/g' config.test.toml && rm -f config.test.toml.bak
    else
        echo '[server]' > config.test.toml
        echo 'listen_addr = "127.0.0.1:27779"' >> config.test.toml
    fi
fi

# 启动新的实例（放在后台运行或者前台运行，这里为了查看日志放前台）
# 如果希望它在后台运行，可以加 & 并在末尾 tail -f 日志
HERMES_CONFIG=config.test.toml ./tmp_bin/hermes


# bash scripts/test_run.sh
