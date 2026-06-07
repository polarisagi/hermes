#!/bin/bash
set -e

REPO="polarisagi/hermes"
BIN_NAME="hermes"
INSTALL_DIR="$HOME/.polarisagi/hermes/bin"
PLIST_PATH="$HOME/Library/LaunchAgents/com.polarisagi.hermes.plist"

if [[ "$LANG" == *"zh"* ]] || [[ "$LC_ALL" == *"zh"* ]]; then
    LANG_ZH=true
else
    LANG_ZH=false
fi

function print_msg() {
    local zh_msg="$1"
    local en_msg="$2"
    if [ "$LANG_ZH" = true ]; then
        echo "$zh_msg"
    else
        echo "$en_msg"
    fi
}

print_msg "🌌 正在安装/更新 PolarisAGI Hermes (用户态模式)..." "🌌 Installing/Updating PolarisAGI Hermes (User mode)..."

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

if [ "$ARCH" = "x86_64" ]; then
    ARCH="amd64"
elif [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
    ARCH="arm64"
else
    print_msg "❌ 不支持的架构: $ARCH" "❌ Unsupported architecture: $ARCH"
    exit 1
fi

DL_URL="https://github.com/${REPO}/releases/latest/download/${BIN_NAME}-${OS}-${ARCH}.tar.gz"

print_msg "🌐 正在检测 GitHub 网络连通性..." "🌐 Checking GitHub connectivity..."
if ! curl -sSL -I -f --connect-timeout 5 "https://github.com" > /dev/null 2>&1; then
    print_msg "⚠️ 访问 GitHub 超时，自动切换至国内加速节点 (ghproxy.net)..." "⚠️ GitHub timeout, switching to domestic proxy..."
    DL_URL="https://ghproxy.net/${DL_URL}"
fi

print_msg "⬇️ 正在下载最新版本: $DL_URL" "⬇️ Downloading latest version: $DL_URL"
curl -sSL -f -o /tmp/${BIN_NAME}.tar.gz "$DL_URL" || {
    print_msg "❌ 下载失败。请检查网络或确认仓库是否已发布 Release。" "❌ Download failed. Check network or Release availability."
    exit 1
}

print_msg "📦 正在解压..." "📦 Extracting..."
tar -xzf /tmp/${BIN_NAME}.tar.gz -C /tmp/
rm -f /tmp/${BIN_NAME}.tar.gz

chmod +x /tmp/${BIN_NAME}-${OS}-${ARCH}

# Stop old service
if [ "$OS" = "linux" ] && command -v systemctl >/dev/null; then
    if systemctl --user is-active --quiet hermes 2>/dev/null; then
        print_msg "🛑 正在停止运行中的 Linux 用户级服务..." "🛑 Stopping running Linux user service..."
        systemctl --user stop hermes || true
    fi
    if systemctl is-active --quiet hermes 2>/dev/null || [ -f "/etc/systemd/system/hermes.service" ]; then
        print_msg "🧹 发现旧的全局 Systemd 服务，尝试停止并清理 (可能需要 sudo 密码)..." "🧹 Found legacy global Systemd service, attempting to stop and clean up (may require sudo)..."
        sudo systemctl stop hermes 2>/dev/null || true
        sudo systemctl disable hermes 2>/dev/null || true
        sudo rm -f /etc/systemd/system/hermes.service
        sudo systemctl daemon-reload
    fi
elif [ "$OS" = "darwin" ]; then
    if launchctl list | grep -q "com.polarisagi.hermes"; then
        print_msg "🛑 正在停止运行中的 macOS 服务..." "🛑 Stopping running macOS service..."
        launchctl unload "$PLIST_PATH" 2>/dev/null || true
    fi
    pkill -f "${BIN_NAME}" 2>/dev/null || true
fi

mkdir -p "${INSTALL_DIR}"
rm -f "${INSTALL_DIR}/${BIN_NAME}"
mv -f /tmp/${BIN_NAME}-${OS}-${ARCH} "${INSTALL_DIR}/${BIN_NAME}"

print_msg "✅ 二进制文件已安装至: ${INSTALL_DIR}/${BIN_NAME}" "✅ Binary installed to: ${INSTALL_DIR}/${BIN_NAME}"

if [ "$OS" = "linux" ] && command -v systemctl >/dev/null; then
    print_msg "⚙️ 正在配置 Linux 用户级 Systemd 后台服务..." "⚙️ Configuring Linux user Systemd service..."
    
    SYSTEMD_USER_DIR="$HOME/.config/systemd/user"
    mkdir -p "$SYSTEMD_USER_DIR"
    
    cat <<EOF2 > "$SYSTEMD_USER_DIR/hermes.service"
[Unit]
Description=PolarisAGI AI Gateway (User Service)
After=network.target

[Service]
ExecStart=${INSTALL_DIR}/${BIN_NAME}
Restart=always
WorkingDirectory=${HOME}

[Install]
WantedBy=default.target
EOF2

    loginctl enable-linger $USER 2>/dev/null || true

    systemctl --user daemon-reload
    systemctl --user enable hermes
    systemctl --user restart hermes
    print_msg "✅ Systemd 服务已启动。可通过 systemctl --user status hermes 查看状态。" "✅ Systemd service started. Check status with: systemctl --user status hermes"

elif [ "$OS" = "darwin" ]; then
    print_msg "⚙️ 正在配置 macOS launchd 后台服务..." "⚙️ Configuring macOS launchd service..."
    
    mkdir -p "$HOME/Library/LaunchAgents"

    cat <<EOF2 > "$PLIST_PATH"
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.polarisagi.hermes</string>
    <key>ProgramArguments</key>
    <array>
        <string>${INSTALL_DIR}/${BIN_NAME}</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>WorkingDirectory</key>
    <string>${HOME}</string>
</dict>
</plist>
EOF2
    launchctl load "$PLIST_PATH"
    print_msg "✅ macOS 服务已配置。将随当前用户登录自动启动并在后台运行。" "✅ macOS service configured. It will auto-start in background on user login."
fi

print_msg "🎉 安装完成！请打开浏览器访问 http://127.0.0.1:27777 进入控制台。" "🎉 Installation complete! Open your browser at http://127.0.0.1:27777 to access the console."
print_msg "💡 提示：如果需要在命令行直接使用 hermes，请将 ${INSTALL_DIR} 加入环境变量。" "💡 Tip: To use hermes directly in CLI, add ${INSTALL_DIR} to your PATH."
