#!/bin/bash

BIN_NAME="hermes"
INSTALL_DIR="$HOME/.polarisagi/hermes/bin"

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

print_msg "🗑️ 正在卸载 PolarisAGI Hermes (用户态模式)..." "🗑️ Uninstalling PolarisAGI Hermes (User mode)..."

OS=$(uname -s | tr '[:upper:]' '[:lower:]')

if [ "$OS" = "linux" ] && command -v systemctl >/dev/null; then
    if systemctl --user is-active --quiet hermes 2>/dev/null; then
        print_msg "⚙️ 停止用户级 systemd 服务..." "⚙️ Stopping user systemd service..."
        systemctl --user stop hermes
    fi
    if systemctl --user is-enabled --quiet hermes 2>/dev/null; then
        print_msg "⚙️ 禁用用户级 systemd 服务..." "⚙️ Disabling user systemd service..."
        systemctl --user disable hermes
    fi
    
    SYSTEMD_USER_DIR="$HOME/.config/systemd/user"
    if [ -f "$SYSTEMD_USER_DIR/hermes.service" ]; then
        print_msg "⚙️ 删除用户级 systemd 配置文件..." "⚙️ Removing user systemd configuration file..."
        rm "$SYSTEMD_USER_DIR/hermes.service"
        systemctl --user daemon-reload
    fi
elif [ "$OS" = "darwin" ]; then
    PLIST_PATH="$HOME/Library/LaunchAgents/com.polarisagi.hermes.plist"
    if [ -f "$PLIST_PATH" ]; then
        print_msg "⚙️ 卸载 macOS launchd 服务..." "⚙️ Unloading macOS launchd service..."
        launchctl unload "$PLIST_PATH" 2>/dev/null
        rm "$PLIST_PATH"
    fi
fi

if [ -f "${INSTALL_DIR}/${BIN_NAME}" ]; then
    print_msg "🗑️ 删除二进制文件: ${INSTALL_DIR}/${BIN_NAME}" "🗑️ Removing binary file: ${INSTALL_DIR}/${BIN_NAME}"
    rm "${INSTALL_DIR}/${BIN_NAME}"
fi

echo ""
print_msg "⚠️  注意: 您的数据库和配置数据仍保留在 ~/.polarisagi/hermes 目录中。" "⚠️  Notice: Your database and configuration data remain in the ~/.polarisagi/hermes directory."
print_msg "如果您想彻底清理所有数据（这会删除所有配置和账单记录），请手动执行：" "If you want to completely clean up all data (this deletes all configs and billing records), please manually execute:"
echo "rm -rf ~/.polarisagi/hermes"
echo ""
print_msg "✅ 卸载完成！" "✅ Uninstallation complete!"
