package clientcfg

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/polarisagi/hermes/internal/config"
	"github.com/polarisagi/hermes/internal/domain"
	"github.com/polarisagi/hermes/internal/store"
)

const polarisAPIKey = "sk-hermes"
const polarisMarker = "hermes"

type clientDef struct {
	Name           string
	InstallDir     string
	ConfigRelPath  string
	getInstallDir  func(home string) string
	getConfigPath  func(home string) string
	applyFn        func(home, listenAddr string) error
	isApplied      func(home string) bool
	getInjectedURL func(listenAddr string) string
}

var allClients = []clientDef{
	{
		Name:          "codex",
		InstallDir:    ".codex",
		ConfigRelPath: ".codex/config.toml",
		applyFn: func(home, listenAddr string) error {
			if err := os.MkdirAll(filepath.Join(home, ".codex"), 0755); err != nil {
				return err
			}

			// 采用 openai_base_url 覆盖方案，无需再注入 auth.json 或自定义 provider
			return applyCodexTOML(filepath.Join(home, ".codex/config.toml"), listenAddr)
		},
		isApplied: func(home string) bool {
			data, err := os.ReadFile(filepath.Join(home, ".codex/config.toml"))
			if err != nil {
				return false
			}
			return strings.Contains(string(data), `openai_base_url = "http://`)
		},
		getInjectedURL: func(listenAddr string) string { return "http://" + listenAddr + "/v1/openai" },
	},
	{
		Name:          "claude_code",
		InstallDir:    ".claude",
		ConfigRelPath: ".claude/settings.json",
		applyFn: func(home, listenAddr string) error {
			return applyJSONConfig(
				filepath.Join(home, ".claude/settings.json"),
				map[string]any{
					"primaryApiKey": polarisAPIKey,
					"env": map[string]any{
						"ANTHROPIC_BASE_URL":   "http://" + listenAddr + "/v1/anthropic",
						"ANTHROPIC_AUTH_TOKEN": polarisAPIKey,
						"ANTHROPIC_API_KEY":    polarisAPIKey,
					},
				},
			)
		},
		isApplied: func(home string) bool {
			return isJSONConfigured(filepath.Join(home, ".claude/settings.json"), "primaryApiKey")
		},
		getInjectedURL: func(listenAddr string) string { return "http://" + listenAddr + "/v1/anthropic" },
	},
	{
		Name: "claude_desktop",
		getInstallDir: func(home string) string {
			dir, err := os.UserConfigDir()
			if err != nil {
				return filepath.Join(home, "Library", "Application Support", "Claude")
			}
			return filepath.Join(dir, "Claude")
		},
		getConfigPath: func(home string) string {
			dir, err := os.UserConfigDir()
			base := filepath.Join(home, "Library", "Application Support")
			if err == nil {
				base = dir
			}
			return filepath.Join(base, "Claude-3p", "configLibrary", "polarisagi.json")
		},
		applyFn: func(home, listenAddr string) error {
			dir, err := os.UserConfigDir()
			base := filepath.Join(home, "Library", "Application Support")
			if err == nil {
				base = dir
			}

			claudeConfigPath := filepath.Join(base, "Claude", "claude_desktop_config.json")
			_ = applyJSONConfig(claudeConfigPath, map[string]any{"deploymentMode": "3p"})

			claude3pConfigPath := filepath.Join(base, "Claude-3p", "claude_desktop_config.json")
			if err := os.MkdirAll(filepath.Dir(claude3pConfigPath), 0755); err == nil {
				_ = applyJSONConfig(claude3pConfigPath, map[string]any{"deploymentMode": "3p"})
			}

			polarisConfigPath := filepath.Join(base, "Claude-3p", "configLibrary", "polarisagi.json")
			if err := os.MkdirAll(filepath.Dir(polarisConfigPath), 0755); err != nil {
				return err
			}

			config := map[string]any{
				"inferenceProvider":            "gateway",
				"inferenceCredentialKind":      "static",
				"inferenceGatewayBaseUrl":      "http://" + listenAddr + "/v1/anthropic",
				"inferenceGatewayApiKey":       polarisAPIKey,
				"coworkEgressAllowedHosts":     []string{"*"},
				"disableDeploymentModeChooser": true,
				"inferenceModels": []any{
					map[string]any{
						"name":          "claude-sonnet-4-6",
						"labelOverride": "PolarisAGI Sonnet",
						"supports1m":    false,
					},
					map[string]any{
						"name":          "claude-opus-4-8",
						"labelOverride": "PolarisAGI Opus",
						"supports1m":    true,
					},
				},
			}

			out, err := json.MarshalIndent(config, "", "  ")
			if err != nil {
				return err
			}
			if err := atomicWriteFile(polarisConfigPath, out, 0644); err != nil {
				return err
			}

			metaPath := filepath.Join(base, "Claude-3p", "configLibrary", "_meta.json")
			meta := make(map[string]any)
			if data, err := os.ReadFile(metaPath); err == nil {
				_ = json.Unmarshal(data, &meta)
			}
			meta["appliedId"] = "polarisagi"

			var entries []any
			if e, ok := meta["entries"].([]any); ok {
				entries = e
			}
			found := false
			for _, e := range entries {
				if eMap, ok := e.(map[string]any); ok {
					if eMap["id"] == "polarisagi" {
						found = true
						break
					}
				}
			}
			if !found {
				entries = append(entries, map[string]any{
					"id":   "polarisagi",
					"name": "PolarisAGI Hermes",
				})
				meta["entries"] = entries
			}

			metaOut, err := json.MarshalIndent(meta, "", "  ")
			if err != nil {
				return err
			}
			return atomicWriteFile(metaPath, metaOut, 0644)
		},
		isApplied: func(home string) bool {
			dir, err := os.UserConfigDir()
			base := filepath.Join(home, "Library", "Application Support")
			if err == nil {
				base = dir
			}
			polarisConfigPath := filepath.Join(base, "Claude-3p", "configLibrary", "polarisagi.json")
			if _, err := os.Stat(polarisConfigPath); os.IsNotExist(err) {
				return false
			}

			metaPath := filepath.Join(base, "Claude-3p", "configLibrary", "_meta.json")
			data, err := os.ReadFile(metaPath)
			if err != nil {
				return false
			}
			var meta map[string]any
			if err := json.Unmarshal(data, &meta); err != nil {
				return false
			}
			appliedId, _ := meta["appliedId"].(string)
			return appliedId == "polarisagi"
		},
		getInjectedURL: func(listenAddr string) string { return "http://" + listenAddr + "/v1/anthropic" },
	},
	{
		Name:          "opencode",
		InstallDir:    ".config/opencode",
		ConfigRelPath: ".config/opencode/opencode.json",
		applyFn: func(home, listenAddr string) error {
			path := filepath.Join(home, ".config/opencode/opencode.json")
			// 旧版本曾以自定义 provider "vertex"（npm: @ai-sdk/google，且未声明任何 models）误注入，
			// 导致 OpenCode 模型列表中出现一个无可用模型的空 provider；现改为直接覆盖内置的
			// "google-vertex" provider（models.dev 目录已自带模型定义），此处先清理遗留的旧条目。
			removeStaleProviderEntry(path, "vertex")
			return applyJSONConfig(
				path,
				map[string]any{
					"provider": map[string]any{
						"google-vertex": map[string]any{
							"options": map[string]any{
								"baseURL": "http://" + listenAddr + "/v1/google",
								// 注入 apiKey 使 @ai-sdk/google-vertex 进入 "Express Mode"：
								// 跳过 OAuth/ADC（GOOGLE_APPLICATION_CREDENTIALS）凭证查找，
								// 否则用户本机若未配置 GCP 凭证，请求会在到达本网关前就失败。
								"apiKey": polarisAPIKey,
							},
						},
					},
				},
			)
		},
		isApplied: func(home string) bool {
			return isJSONConfigured(
				filepath.Join(home, ".config/opencode/opencode.json"),
				"provider.google-vertex.options.apiKey",
			)
		},
		getInjectedURL: func(listenAddr string) string { return "http://" + listenAddr + "/v1/google" },
	},
	{
		Name:          "gemini_cli",
		InstallDir:    ".gemini",
		ConfigRelPath: ".gemini/.env",
		applyFn: func(home, listenAddr string) error {
			return applyEnvConfig(
				filepath.Join(home, ".gemini/.env"),
				[]envKeyDef{
					{Key: "GEMINI_API_KEY", Value: func(_ string) string { return polarisAPIKey }},
					{Key: "GOOGLE_GEMINI_BASE_URL", Value: func(addr string) string { return "http://" + addr + "/v1/google" }},
				},
				listenAddr,
			)
		},
		isApplied: func(home string) bool {
			return isEnvConfigured(filepath.Join(home, ".gemini/.env"), "GEMINI_API_KEY")
		},
		getInjectedURL: func(listenAddr string) string { return "http://" + listenAddr + "/v1/google" },
	},
}

// Manager 管理所有 AI 客户端的自动配置注入和恢复
type Manager struct {
	settingsRepo *store.SettingsRepo
	backupRepo   *store.ClientBackupRepo
}

func NewManager(settingsRepo *store.SettingsRepo, backupRepo *store.ClientBackupRepo) *Manager {
	return &Manager{settingsRepo: settingsRepo, backupRepo: backupRepo}
}

func (m *Manager) GetAllStatuses(ctx context.Context) ([]domain.ClientStatus, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("无法获取用户主目录: %w", err)
	}

	backups, err := m.backupRepo.GetAll(ctx)
	if err != nil {
		slog.Warn("加载客户端备份记录失败", "error", err)
		backups = make(map[string]*store.ClientBackupRecord)
	}

	statuses := make([]domain.ClientStatus, 0, len(allClients))
	for _, def := range allClients {
		statuses = append(statuses, m.detectStatus(home, def, backups))
	}
	return statuses, nil
}

func (m *Manager) detectStatus(home string, def clientDef, backups map[string]*store.ClientBackupRecord) domain.ClientStatus {
	st := domain.ClientStatus{
		Name: def.Name,
	}
	installDir := filepath.Join(home, def.InstallDir)
	if def.getInstallDir != nil {
		installDir = def.getInstallDir(home)
	}
	if _, err := os.Stat(installDir); err == nil {
		st.IsInstalled = true
	}
	st.IsConfigured = def.isApplied(home)
	if rec, ok := backups[def.Name]; ok {
		st.HasBackup = true
		st.InjectedURL = rec.InjectedURL
	}
	return st
}

func (m *Manager) ApplyConfig(ctx context.Context, clientName string) error {
	def, ok := findClient(clientName)
	if !ok {
		return fmt.Errorf("不支持的客户端: %s", clientName)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("无法获取用户主目录: %w", err)
	}

	listenAddr, _ := m.settingsRepo.GetSetting(ctx, "listen_addr")
	if listenAddr == "" {
		listenAddr = config.GlobalConfig.Server.ListenAddr
	}

	configPath := filepath.Join(home, def.ConfigRelPath)
	if def.getConfigPath != nil {
		configPath = def.getConfigPath(home)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("无法创建配置目录: %w", err)
	}

	if err := m.backup(ctx, def.Name, configPath, def.getInjectedURL(listenAddr)); err != nil {
		slog.Warn("备份配置文件失败，继续写入", "client", def.Name, "error", err)
	}

	if err := def.applyFn(home, listenAddr); err != nil {
		return fmt.Errorf("注入配置失败: %w", err)
	}

	slog.Info("客户端代理配置注入成功", "client", def.Name, "config", configPath)
	return nil
}

func (m *Manager) RestoreConfig(ctx context.Context, clientName string) error {
	def, ok := findClient(clientName)
	if !ok {
		return fmt.Errorf("不支持的客户端: %s", clientName)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("无法获取用户主目录: %w", err)
	}
	configPath := filepath.Join(home, def.ConfigRelPath)
	if def.getConfigPath != nil {
		configPath = def.getConfigPath(home)
	}
	backupPath := configPath + ".polarisagi_backup"

	rec, dbErr := m.backupRepo.Get(ctx, clientName)
	var hasDBRecord bool
	var originalContent string
	var hasOriginalContent bool

	if dbErr == nil && rec != nil {
		hasDBRecord = true
		if rec.OriginalContent != "" {
			originalContent = rec.OriginalContent
			hasOriginalContent = true
		}
	}

	// 如果数据库中没有记录或者内容为空，尝试使用本地备份文件恢复
	if !hasOriginalContent {
		if data, readErr := os.ReadFile(backupPath); readErr == nil {
			originalContent = string(data)
			hasOriginalContent = true
			slog.Info("数据库中未找到原始配置或读取失败，使用本地备份文件进行恢复", "client", def.Name)
		} else {
			if !hasDBRecord {
				return fmt.Errorf("未找到客户端 %s 的备份数据（数据库和本地文件均不存在）", def.Name)
			}
		}
	}

	if !hasOriginalContent {
		if removeErr := os.Remove(configPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("删除注入的配置文件失败: %w", removeErr)
		}
	} else {
		if writeErr := atomicWriteFile(configPath, []byte(originalContent), 0644); writeErr != nil {
			return fmt.Errorf("恢复配置文件失败: %w", writeErr)
		}
	}

	if hasDBRecord {
		if delErr := m.backupRepo.Delete(ctx, clientName); delErr != nil {
			slog.Warn("删除备份记录失败", "client", clientName, "error", delErr)
		}
	}

	// 尝试清理物理备份文件（如果存在）
	_ = os.Remove(backupPath)

	slog.Info("客户端配置已恢复原始状态", "client", def.Name)
	return nil
}

func (m *Manager) backup(ctx context.Context, clientName, configPath, injectedURL string) error {
	exists, err := m.backupRepo.Exists(ctx, clientName)
	if err != nil {
		return err
	}

	// 确保本地备份文件存在（即使用户删除了本地备份，如果是第一次备份，则创建它，如果是已存在于数据库，最好从数据库恢复本地备份）
	backupPath := configPath + ".polarisagi_backup"

	if exists {
		// 已经有备份记录了，说明原始配置已经安全保存，不要覆盖它，否则会把修改后的内容当成原始配置
		// 尝试从数据库读取原始配置并恢复丢失的本地备份文件
		if _, statErr := os.Stat(backupPath); os.IsNotExist(statErr) {
			if rec, getErr := m.backupRepo.Get(ctx, clientName); getErr == nil && rec != nil && rec.OriginalContent != "" {
				if writeErr := atomicWriteFile(backupPath, []byte(rec.OriginalContent), 0644); writeErr != nil {
					slog.Warn("恢复丢失的物理备份文件失败", "path", backupPath, "error", writeErr)
				}
			}
		}

		// 但是我们需要更新注入的 URL
		return m.backupRepo.UpdateInjectedURL(ctx, clientName, injectedURL)
	}

	content := ""
	if data, readErr := os.ReadFile(configPath); readErr == nil {
		content = string(data)

		// 额外在本地创建一个物理备份文件，方便用户手动兜底恢复
		if _, statErr := os.Stat(backupPath); os.IsNotExist(statErr) {
			if writeErr := atomicWriteFile(backupPath, data, 0644); writeErr != nil {
				slog.Warn("写入物理备份文件失败", "path", backupPath, "error", writeErr)
			}
		}
	}
	return m.backupRepo.Upsert(ctx, clientName, configPath, content, injectedURL)
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".polarisagi-tmp-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

type envKeyDef struct {
	Key   string
	Value func(listenAddr string) string
}

func applyEnvConfig(path string, keys []envKeyDef, listenAddr string) error {
	existingLines := []string{}
	if data, err := os.ReadFile(path); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			existingLines = append(existingLines, scanner.Text())
		}
	}

	injectSet := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		injectSet[k.Key] = struct{}{}
	}

	filtered := make([]string, 0, len(existingLines))
	skipBlock := false
	for _, line := range existingLines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "PolarisAGI-Hermes Proxy Config") {
			skipBlock = true
		}
		if strings.Contains(trimmed, "End PolarisAGI-Hermes") {
			skipBlock = false
			continue
		}
		if skipBlock {
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			filtered = append(filtered, line)
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) == 2 {
			if _, exists := injectSet[parts[0]]; exists {
				continue
			}
		}
		filtered = append(filtered, line)
	}

	filtered = append(filtered, "")
	filtered = append(filtered, "# ── PolarisAGI-Hermes Proxy Config (auto-injected) ──")
	for _, k := range keys {
		filtered = append(filtered, fmt.Sprintf("%s=%s", k.Key, k.Value(listenAddr)))
	}
	filtered = append(filtered, "# ── End PolarisAGI-Hermes ──")

	return atomicWriteFile(path, []byte(strings.Join(filtered, "\n")+"\n"), 0644)
}

func isEnvConfigured(path, signatureKey string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, signatureKey+"=") {
			val := strings.TrimPrefix(line, signatureKey+"=")
			return strings.Contains(strings.ToLower(val), polarisMarker)
		}
	}
	return false
}

func applyJSONConfig(path string, patch map[string]any) error {
	existing := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &existing)
	}
	merged := deepMerge(existing, patch)
	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, out, 0644)
}

// removeStaleProviderEntry 删除 provider.<key> 节点（仅当其 options.apiKey 带有 polarisMarker 标记时），
// 用于清理本程序历史版本误注入、且现已改名/废弃的自定义 provider 条目，避免误删用户自行配置的同名 provider。
func removeStaleProviderEntry(path, providerKey string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return
	}
	providers, ok := obj["provider"].(map[string]any)
	if !ok {
		return
	}
	entry, ok := providers[providerKey].(map[string]any)
	if !ok {
		return
	}
	options, _ := entry["options"].(map[string]any)
	apiKey, _ := options["apiKey"].(string)
	if !strings.Contains(strings.ToLower(apiKey), polarisMarker) {
		return
	}
	delete(providers, providerKey)
	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return
	}
	_ = atomicWriteFile(path, out, 0644)
}

func isJSONConfigured(path, jsonPath string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return false
	}
	val, _ := getJSONPath(obj, jsonPath).(string)
	return strings.Contains(strings.ToLower(val), polarisMarker)
}

func deepMerge(base, patch map[string]any) map[string]any {
	result := make(map[string]any, len(base))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range patch {
		if pMap, ok := v.(map[string]any); ok {
			if bMap, ok := result[k].(map[string]any); ok {
				result[k] = deepMerge(bMap, pMap)
				continue
			}
		}
		result[k] = v
	}
	return result
}

func getJSONPath(obj map[string]any, path string) any {
	parts := strings.SplitN(path, ".", 2)
	val, ok := obj[parts[0]]
	if !ok {
		return nil
	}
	if len(parts) == 1 {
		return val
	}
	sub, ok := val.(map[string]any)
	if !ok {
		return nil
	}
	return getJSONPath(sub, parts[1])
}

func findClient(name string) (clientDef, bool) {
	for _, c := range allClients {
		if c.Name == name {
			return c, true
		}
	}
	return clientDef{}, false
}

// applyCodexTOML 使用官方提供的 openai_base_url 参数进行代理重定向，
// 这样 Codex 仍然认为使用的是内置的 openai provider，从而保留所有官方插件权限。
// 同时我们不需要注入任何第三方 api_key。
func applyCodexTOML(path, listenAddr string) error {
	newBaseURL := fmt.Sprintf("http://%s/v1/openai", listenAddr)

	content := ""
	if data, err := os.ReadFile(path); err == nil {
		content = string(data)
	}

	lines := strings.Split(content, "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = []string{}
	}

	modelProviderIdx := -1
	openaiBaseUrlIdx := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "model_provider") && strings.Contains(trimmed, "=") {
			modelProviderIdx = i
		}
		if strings.HasPrefix(trimmed, "openai_base_url") && strings.Contains(trimmed, "=") {
			openaiBaseUrlIdx = i
		}
	}

	if modelProviderIdx >= 0 {
		lines[modelProviderIdx] = `model_provider = "openai"`
	} else {
		// 插入到开头
		lines = append([]string{`model_provider = "openai"`}, lines...)
		if openaiBaseUrlIdx >= 0 {
			openaiBaseUrlIdx++
		}
	}

	if openaiBaseUrlIdx >= 0 {
		lines[openaiBaseUrlIdx] = fmt.Sprintf("openai_base_url = \"%s\"", newBaseURL)
	} else {
		// 插入到 model_provider 后面
		insert := fmt.Sprintf("openai_base_url = \"%s\"", newBaseURL)
		lines = append(lines[:1], append([]string{insert}, lines[1:]...)...)
	}

	return atomicWriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}
