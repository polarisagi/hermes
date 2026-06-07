package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"runtime"

	"github.com/polarisagi/hermes/internal/store"
)

// firstRunBrowserKey 持久化于 system_settings 表，标记"是否已自动打开过管理页面"。
// 浏览器仅在程序首次启动时弹出一次；开机自启等后续启动保持静默，
// 与"隐藏式后台运行"的产品定位（见 commit f0bd175）保持一致。
const firstRunBrowserKey = "ui_first_run_browser_opened"

// maybeOpenDashboard 在程序首次启动时自动用系统默认浏览器打开管理页面，
// 让用户第一时间看到"服务已就绪、如何管理"——提供接近传统桌面软件的开箱体验。
//
// 测试模式（TEST_MODE=true）及非首次启动均不触发：前者避免干扰开发调试，
// 后者避免随系统自启而反复弹出浏览器打扰用户。
func maybeOpenDashboard(ctx context.Context, settingsRepo *store.SettingsRepo, listenAddr string) {
	if os.Getenv("TEST_MODE") == "true" {
		return
	}

	opened, err := settingsRepo.GetSetting(ctx, firstRunBrowserKey)
	if err != nil {
		slog.Warn("读取首次启动标记失败，跳过自动打开管理页面", "error", err)
		return
	}
	if opened != "" {
		return
	}

	url := dashboardURL(listenAddr)
	if err := openBrowser(url); err != nil {
		slog.Warn("自动打开管理页面失败，请手动访问该地址", "url", url, "error", err)
		return
	}

	slog.Info("首次启动，已自动打开管理页面", "url", url)
	if err := settingsRepo.SetSetting(ctx, firstRunBrowserKey, "true"); err != nil {
		slog.Warn("保存首次启动标记失败，下次启动可能再次自动打开管理页面", "error", err)
	}
}

// dashboardURL 将监听地址转换为浏览器可直接访问的管理页面地址。
// 监听地址若形如 "0.0.0.0:27777" 或 ":27777"，其主机段并非浏览器可达地址，
// 统一替换为 "127.0.0.1"。
func dashboardURL(listenAddr string) string {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "http://" + listenAddr + "/"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s/", net.JoinHostPort(host, port))
}

// openBrowser 调用系统默认浏览器打开指定地址，按平台分发对应命令。
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		// 经由系统 URL 关联打开，避免 cmd /c start 对 & 等特殊字符的转义问题
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
