package api

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/polarisagi/hermes/internal/clientcfg"
	"github.com/polarisagi/hermes/internal/pool"
	"github.com/polarisagi/hermes/internal/router"
	"github.com/polarisagi/hermes/internal/store"
)

// Version can be injected via build flags (-ldflags="-X 'github.com/polarisagi/hermes/internal/api.Version=vX.Y.Z'")
var (
	Version         = "dev"
	oauthStateStore = sync.Map{}
)

// AdminHandler 处理后台管理面板的 RESTful API 请求
type AdminHandler struct {
	providerRepo *store.ProviderRepo
	modelRepo    *store.ModelRepo
	routeRepo    *store.RouteRepo
	intentRepo   *store.IntentRepo
	settingsRepo *store.SettingsRepo
	extCacheRepo *store.ExternalCacheRepo
	clientSvc    *clientcfg.Manager

	// 热重载目标：写操作完成后同步刷新内存状态
	chanManager *pool.Manager
	pipeline    *router.Pipeline
}

func NewAdminHandler(
	pRepo *store.ProviderRepo,
	mRepo *store.ModelRepo,
	rRepo *store.RouteRepo,
	iRepo *store.IntentRepo,
	sRepo *store.SettingsRepo,
	ecRepo *store.ExternalCacheRepo,
	clientSvc *clientcfg.Manager,
	chanManager *pool.Manager,
	pipeline *router.Pipeline,
) *AdminHandler {
	return &AdminHandler{
		providerRepo: pRepo,
		modelRepo:    mRepo,
		routeRepo:    rRepo,
		intentRepo:   iRepo,
		settingsRepo: sRepo,
		extCacheRepo: ecRepo,
		clientSvc:    clientSvc,
		chanManager:  chanManager,
		pipeline:     pipeline,
	}
}

func (h *AdminHandler) reloadAll(ctx context.Context) {
	if err := h.chanManager.Reload(ctx); err != nil {
		slog.Warn("渠道热重载失败", "error", err)
	}
	if err := h.pipeline.Reload(ctx); err != nil {
		slog.Warn("路由管线热重载失败", "error", err)
	}
}

func (h *AdminHandler) reloadPipeline(ctx context.Context) {
	if err := h.pipeline.Reload(ctx); err != nil {
		slog.Warn("路由管线热重载失败", "error", err)
	}
}

func (h *AdminHandler) GetInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"version": Version,
		"debug":   false,
	})
}

func (h *AdminHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	start := r.URL.Query().Get("start")
	end := r.URL.Query().Get("end")
	if start == "" {
		start = "1970-01-01"
	}
	if end == "" {
		end = "2099-12-31"
	}

	details, err := store.GetAccountLogRepo().GetDashboardStats(r.Context(), start, end)
	if err != nil {
		slog.Error("获取大盘数据失败", "error", err)
		details = []store.DashboardStat{}
	}

	active, waiting, max := h.chanManager.GetStats()

	resp := map[string]interface{}{
		"details":       details,
		"active_count":  active,
		"waiting_count": waiting,
		"max_limit":     max,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *AdminHandler) UpdateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TargetVersion string `json:"target_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TargetVersion == "" {
		http.Error(w, "invalid request or missing target_version", http.StatusBadRequest)
		return
	}

	goos := runtime.GOOS
	goarch := runtime.GOARCH
	if goarch == "x86_64" {
		goarch = "amd64" // Go reports amd64, but just in case
	}

	fileName := fmt.Sprintf("hermes-%s-%s", goos, goarch)
	if goos == "windows" {
		fileName += ".zip"
	} else {
		fileName += ".tar.gz"
	}

	baseURL := fmt.Sprintf("https://github.com/polarisagi/hermes/releases/download/%s/%s", req.TargetVersion, fileName)
	downloadURL := baseURL

	proxyChannel, _ := h.settingsRepo.GetSetting(r.Context(), "update_proxy_channel")

	if proxyChannel == "disable" {
		slog.Info("🌐 用户配置为原生直连，跳过代理...")
	} else if strings.HasPrefix(proxyChannel, "http") {
		slog.Info("🌐 使用用户自定义代理通道...", "proxy", proxyChannel)
		if !strings.HasSuffix(proxyChannel, "/") {
			proxyChannel += "/"
		}
		downloadURL = proxyChannel + baseURL
	} else {
		// "auto" or empty
		slog.Info("🌐 正在检测 GitHub 网络连通性(Auto)...")
		checkClient := &http.Client{Timeout: 5 * time.Second}
		if _, err := checkClient.Head("https://github.com"); err != nil {
			slog.Warn("⚠️ 访问 GitHub 超时，自动切换至国内加速节点 (ghproxy.net)...")
			downloadURL = "https://ghproxy.net/" + baseURL
		}
	}

	slog.Info("🔄 开始热更新程序...", "url", downloadURL)

	go func() {
		// 延迟执行以确保 HTTP 响应先返回给客户端
		time.Sleep(1 * time.Second)

		// 使用自定义 HTTP Client 提升在 Windows 下的下载稳定性
		client := &http.Client{
			Timeout: 10 * time.Minute, // 设定整体超时，防止死锁
		}

		resp, err := client.Get(downloadURL)
		if err != nil {
			slog.Error("更新失败: 下载错误", "error", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			slog.Error("更新失败: 非 200 响应", "status", resp.StatusCode)
			return
		}

		exePath, err := os.Executable()
		if err != nil {
			slog.Error("更新失败: 无法获取可执行文件路径", "error", err)
			return
		}

		tmpArchive := exePath + ".archive.tmp"
		outArchive, err := os.OpenFile(tmpArchive, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			slog.Error("更新失败: 无法创建临时包文件", "error", err)
			return
		}

		buf := make([]byte, 1024*1024)
		if _, err := io.CopyBuffer(outArchive, resp.Body, buf); err != nil {
			outArchive.Close()
			os.Remove(tmpArchive)
			slog.Error("更新失败: 写入临时包错误", "error", err)
			return
		}
		outArchive.Close()

		tmpPath := exePath + ".new"
		slog.Info("📦 正在解压更新包...")
		if err := extractArchiveFile(tmpArchive, tmpPath, goos); err != nil {
			os.Remove(tmpArchive)
			os.Remove(tmpPath)
			slog.Error("更新失败: 解压包错误", "error", err)
			return
		}
		os.Remove(tmpArchive) // 解压成功后删除临时包
		if err := os.Chmod(tmpPath, 0755); err != nil {
			slog.Warn("警告: 无法设置文件执行权限", "error", err)
		}

		oldPath := exePath + ".old"
		if runtime.GOOS == "windows" {
			_ = os.Remove(oldPath)
			if err := os.Rename(exePath, oldPath); err != nil {
				os.Remove(tmpPath)
				slog.Error("更新失败: 无法重命名当前运行的文件(Windows)", "error", err)
				return
			}
		}

		if err := os.Rename(tmpPath, exePath); err != nil {
			if runtime.GOOS == "windows" {
				_ = os.Rename(oldPath, exePath)
			}
			os.Remove(tmpPath)
			slog.Error("更新失败: 替换可执行文件错误", "error", err)
			return
		}

		slog.Info("✅ 更新成功，准备退出并由守护进程自动重启...")
		os.Exit(0)
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status": "success", "message": "正在后台更新，系统将在几秒内自动重启"}`))
}

// RegisterRoutes 注册所有管理面板 API 路由
func (h *AdminHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/admin/info", h.GetInfo)
	mux.HandleFunc("/api/admin/update", h.UpdateHandler)
	mux.HandleFunc("/api/admin/ai_accounts", h.HandleAIAccounts)
	mux.HandleFunc("/api/admin/ai_accounts/auto_import", h.AutoImportNodeModels)
	mux.HandleFunc("/api/admin/sys_providers", h.HandleSysProviders)
	mux.HandleFunc("/api/admin/models", h.HandleModels)
	mux.HandleFunc("/api/admin/models/sync", h.SyncModels)
	mux.HandleFunc("/api/admin/models/sync-global", h.SyncGlobalModels)
	mux.HandleFunc("/api/admin/smart_routings", h.HandleSmartRoutings)
	mux.HandleFunc("/api/admin/intents", h.HandleIntents)
	mux.HandleFunc("/api/admin/usage_billing/stats", h.GetStats)
	mux.HandleFunc("/api/admin/settings", h.HandleSettings)
	mux.HandleFunc("/api/admin/client_access/status", h.GetClientsStatus)
	mux.HandleFunc("/api/admin/client_access/apply", h.ApplyClientConfig)
	mux.HandleFunc("/api/admin/client_access/restore", h.RestoreClientConfig)
	mux.HandleFunc("/api/admin/logs", h.GetLogs)
	mux.HandleFunc("/api/admin/debug", h.SetDebug)
	mux.HandleFunc("/api/admin/oauth/google/start", h.StartGoogleOAuth)
	mux.HandleFunc("/api/admin/oauth/google/callback", h.CallbackGoogleOAuth)

	// MCP Management
	mux.HandleFunc("/api/admin/mcp/registry", h.ListMCPRegistry)
	mux.HandleFunc("/api/admin/mcp/installed", h.ListInstalledMCP)
	mux.HandleFunc("/api/admin/mcp/install", h.InstallMCP)
	mux.HandleFunc("/api/admin/mcp/uninstall", h.UninstallMCP)
}

// AutoImportNodeModels 触发为特定 Node 自动导入模型
func (h *AdminHandler) AutoImportNodeModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		NodeID     int    `json:"node_id"`
		ProviderID string `json:"provider_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if payload.NodeID <= 0 || payload.ProviderID == "" {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	err := h.providerRepo.SeedUserModels(r.Context(), payload.NodeID, payload.ProviderID)
	if err != nil {
		slog.Error("自动导入模型失败", "node_id", payload.NodeID, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	go h.reloadAll(context.Background())
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"success"}`))
}

func extractArchiveFile(archivePath, targetPath, goos string) error {
	if goos == "windows" {
		r, err := zip.OpenReader(archivePath)
		if err != nil {
			return err
		}
		defer r.Close()
		for _, f := range r.File {
			if !f.FileInfo().IsDir() && strings.HasSuffix(f.Name, ".exe") {
				rc, err := f.Open()
				if err != nil {
					return err
				}
				defer rc.Close()
				out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
				if err != nil {
					return err
				}
				defer out.Close()
				_, err = io.Copy(out, rc)
				return err
			}
		}
		return fmt.Errorf("executable not found in zip")
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if !hdr.FileInfo().IsDir() && !strings.Contains(hdr.Name, "..") {
			out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				return err
			}
			defer out.Close()
			_, err = io.Copy(out, tr)
			return err
		}
	}
	return fmt.Errorf("executable not found in tar.gz")
}
