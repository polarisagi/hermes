package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/polarisagi/hermes/internal/config"
	"github.com/polarisagi/hermes/internal/domain"
	"github.com/polarisagi/hermes/internal/router"
	"github.com/polarisagi/hermes/internal/store"
)

type SyncService struct {
	extCacheRepo *store.ExternalCacheRepo
	inferer      *router.IntentInferer
}

func NewSyncService(extCacheRepo *store.ExternalCacheRepo, inferer *router.IntentInferer) *SyncService {
	return &SyncService{extCacheRepo: extCacheRepo, inferer: inferer}
}

type openRouterModelsResponse struct {
	Data []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		ContextLength int    `json:"context_length"`
		Created       int64  `json:"created"`
		Pricing       struct {
			Prompt     string `json:"prompt"`
			Completion string `json:"completion"`
		} `json:"pricing"`
		Architecture struct {
			InputModalities  []string `json:"input_modalities"`
			OutputModalities []string `json:"output_modalities"`
		} `json:"architecture"`
		TopProvider struct {
			MaxCompletionTokens int `json:"max_completion_tokens"`
		} `json:"top_provider"`
	} `json:"data"`
}

type openRouterProvidersResponse struct {
	Data []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"data"`
}

type siliconFlowModelsResponse struct {
	Data []struct {
		ID      string `json:"id"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

func (s *SyncService) SyncGlobalModels(ctx context.Context) error {
	slog.Info("[Sync] 启动全局模型字典同步...")

	dataDir := config.GlobalConfig.Sync.DataDir
	if dataDir == "" {
		slog.Warn("[Sync] data_dir 未配置，跳过同步")
		return nil
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("创建 data_dir 失败: %w", err)
	}

	useGit := config.GlobalConfig.Sync.EnableGitTracking
	if useGit {
		if _, err := os.Stat(filepath.Join(dataDir, ".git")); os.IsNotExist(err) {
			_ = exec.Command("git", "-C", dataDir, "init").Run()
		}
	}

	downloads := []struct {
		url      string
		filename string
	}{
		{"https://openrouter.ai/api/v1/models", "openrouter_models.json"},
		{"https://openrouter.ai/api/v1/providers", "openrouter_providers.json"},
		{"https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json", "litellm_models.json"},
	}
	for _, d := range downloads {
		if err := downloadFileAtomic(ctx, d.url, filepath.Join(dataDir, d.filename), ""); err != nil {
			slog.Warn("[Sync] 下载失败", "file", d.filename, "error", err)
		}
	}

	sfCfg := config.GlobalConfig.Sync.SiliconFlow
	if sfCfg.Enabled && sfCfg.APIKey != "" {
		sfPath := filepath.Join(dataDir, "siliconflow_models.json")
		if err := downloadFileAtomic(ctx, "https://api.siliconflow.cn/v1/models", sfPath, sfCfg.APIKey); err != nil {
			slog.Warn("[Sync] SiliconFlow 下载失败", "error", err)
		}
	}

	// Git Zero-Cost 检测：无变化则跳过 DB 写入
	if useGit {
		out, err := exec.Command("git", "-C", dataDir, "status", "--porcelain").Output()
		if err == nil && len(bytes.TrimSpace(out)) == 0 {
			slog.Info("[Sync] 外部数据无变化，跳过暂存表写入")
			return nil
		}
	}

	slog.Info("[Sync] 检测到数据变更，开始解析写入暂存表...")

	s.parseAndCacheOpenRouterModels(ctx, filepath.Join(dataDir, "openrouter_models.json"))
	s.parseAndCacheOpenRouterProviders(ctx, filepath.Join(dataDir, "openrouter_providers.json"))
	s.parseAndCacheLiteLLM(ctx, filepath.Join(dataDir, "litellm_models.json"))

	if sfCfg.Enabled && sfCfg.APIKey != "" {
		s.parseAndCacheSiliconFlow(ctx, filepath.Join(dataDir, "siliconflow_models.json"))
	}

	if useGit {
		_ = exec.Command("git", "-C", dataDir, "add", ".").Run()
		msg := fmt.Sprintf("auto-sync: %s", time.Now().Format("2006-01-02 15:04:05"))
		_ = exec.Command("git", "-C", dataDir, "commit", "-m", msg).Run()
		slog.Info("[Sync] Git 历史快照已提交")
	}

	slog.Info("[Sync] 同步完成，数据已写入暂存表，等待晋升程序处理")
	return nil
}

func (s *SyncService) parseAndCacheOpenRouterModels(ctx context.Context, path string) {
	f, err := os.Open(path)
	if err != nil {
		slog.Warn("[Sync][OpenRouter] 打开文件失败", "path", path, "error", err)
		return
	}
	defer f.Close()

	var resp openRouterModelsResponse
	if err := json.NewDecoder(f).Decode(&resp); err != nil {
		slog.Warn("[Sync][OpenRouter] JSON 解析失败", "error", err)
		return
	}

	count := 0
	for _, m := range resp.Data {
		parts := strings.SplitN(m.ID, "/", 2)
		modelID := m.ID
		if len(parts) == 2 {
			modelID = parts[1]
		}

		entry := &domain.ExternalModelCache{
			ModelID:       modelID,
			Source:        "openrouter",
			DisplayName:   m.Name,
			ContextLength: m.ContextLength,
			ReleasedAt:    m.Created,
		}

		if m.TopProvider.MaxCompletionTokens > 0 {
			entry.MaxOutputTokens = m.TopProvider.MaxCompletionTokens
		}
		if p, err := strconv.ParseFloat(m.Pricing.Prompt, 64); err == nil && p > 0 {
			entry.PromptPricePer1k = p * 1000
		}
		if p, err := strconv.ParseFloat(m.Pricing.Completion, 64); err == nil && p > 0 {
			entry.CompletionPricePer1k = p * 1000
		}
		for _, mod := range m.Architecture.InputModalities {
			switch mod {
			case "image":
				entry.SupportsVision = true
			case "audio":
				entry.SupportsAudioInput = true
			}
		}
		for _, mod := range m.Architecture.OutputModalities {
			if mod == "audio" {
				entry.SupportsAudioOutput = true
			}
		}

		entry.IsLegacy = s.inferer.IsLegacyModel(modelID)
		entry.CapabilityTier, entry.TierInferSrc = s.inferer.InferTierOnly(ctx, modelID)

		if err := s.extCacheRepo.UpsertModel(ctx, entry); err != nil {
			slog.Warn("[Sync][OpenRouter] 写入暂存表失败", "model_id", modelID, "error", err)
			continue
		}
		count++
	}
	slog.Info("[Sync][OpenRouter] 模型解析完成", "count", count)
}

func (s *SyncService) parseAndCacheOpenRouterProviders(ctx context.Context, path string) {
	f, err := os.Open(path)
	if err != nil {
		slog.Warn("[Sync][OpenRouter] 厂商文件打开失败", "path", path, "error", err)
		return
	}
	defer f.Close()

	var resp openRouterProvidersResponse
	if err := json.NewDecoder(f).Decode(&resp); err != nil {
		slog.Warn("[Sync][OpenRouter] 厂商 JSON 解析失败", "error", err)
		return
	}

	count := 0
	for _, p := range resp.Data {
		if p.ID == "" {
			continue
		}
		providerID := strings.ToLower(strings.ReplaceAll(p.ID, " ", "_"))
		entry := &domain.ExternalProviderCache{
			ProviderID:   providerID,
			Source:       "openrouter",
			ProviderName: p.Name,
			Description:  p.Description,
			APIProtocol:  "openai",
		}
		if err := s.extCacheRepo.UpsertProvider(ctx, entry); err != nil {
			slog.Warn("[Sync][OpenRouter] 厂商写入暂存表失败", "provider_id", providerID, "error", err)
			continue
		}
		count++
	}
	slog.Info("[Sync][OpenRouter] 厂商解析完成", "count", count)
}

func (s *SyncService) parseAndCacheLiteLLM(ctx context.Context, path string) {
	f, err := os.Open(path)
	if err != nil {
		slog.Warn("[Sync][LiteLLM] 打开文件失败", "path", path, "error", err)
		return
	}
	defer f.Close()

	var raw map[string]any
	if err := json.NewDecoder(f).Decode(&raw); err != nil {
		slog.Warn("[Sync][LiteLLM] JSON 解析失败", "error", err)
		return
	}

	count := 0
	for key, val := range raw {
		if key == "sample_spec" {
			continue
		}
		modelID := key
		parts := strings.SplitN(key, "/", 2)
		if len(parts) == 2 && !strings.Contains(parts[0], "-") {
			modelID = parts[1]
		}

		info, ok := val.(map[string]any)
		if !ok {
			continue
		}

		entry := &domain.ExternalModelCache{ModelID: modelID, Source: "litellm"}
		if v, ok := info["max_input_tokens"].(float64); ok && v > 0 {
			entry.ContextLength = int(v)
		}
		if v, ok := info["max_output_tokens"].(float64); ok && v > 0 {
			entry.MaxOutputTokens = int(v)
		}
		if v, ok := info["supports_vision"].(bool); ok {
			entry.SupportsVision = v
		}
		if v, ok := info["supports_function_calling"].(bool); ok {
			entry.SupportsTools = v
		}
		if v, ok := info["supports_audio_input"].(bool); ok {
			entry.SupportsAudioInput = v
		}
		if v, ok := info["supports_audio_output"].(bool); ok {
			entry.SupportsAudioOutput = v
		}

		entry.IsLegacy = s.inferer.IsLegacyModel(modelID)
		entry.CapabilityTier, entry.TierInferSrc = s.inferer.InferTierOnly(ctx, modelID)

		if err := s.extCacheRepo.UpsertModel(ctx, entry); err != nil {
			slog.Warn("[Sync][LiteLLM] 写入暂存表失败", "model_id", modelID, "error", err)
			continue
		}
		count++
	}
	slog.Info("[Sync][LiteLLM] 模型解析完成", "count", count)
}

func (s *SyncService) parseAndCacheSiliconFlow(ctx context.Context, path string) {
	f, err := os.Open(path)
	if err != nil {
		slog.Warn("[Sync][SiliconFlow] 打开文件失败", "path", path, "error", err)
		return
	}
	defer f.Close()

	var resp siliconFlowModelsResponse
	if err := json.NewDecoder(f).Decode(&resp); err != nil {
		slog.Warn("[Sync][SiliconFlow] JSON 解析失败", "error", err)
		return
	}

	count := 0
	for _, m := range resp.Data {
		parts := strings.SplitN(m.ID, "/", 2)
		modelID := m.ID
		if len(parts) == 2 {
			modelID = parts[1]
		}

		entry := &domain.ExternalModelCache{
			ModelID:    modelID,
			Source:     "siliconflow",
			ReleasedAt: m.Created,
			IsLegacy:   s.inferer.IsLegacyModel(modelID),
		}
		entry.CapabilityTier, entry.TierInferSrc = s.inferer.InferTierOnly(ctx, modelID)

		if m.OwnedBy != "" {
			providerID := strings.ToLower(strings.ReplaceAll(m.OwnedBy, " ", "_"))
			_ = s.extCacheRepo.UpsertProvider(ctx, &domain.ExternalProviderCache{
				ProviderID:   providerID,
				Source:       "siliconflow",
				ProviderName: m.OwnedBy,
				APIProtocol:  "openai",
			})
		}

		if err := s.extCacheRepo.UpsertModel(ctx, entry); err != nil {
			slog.Warn("[Sync][SiliconFlow] 写入暂存表失败", "model_id", modelID, "error", err)
			continue
		}
		count++
	}
	slog.Info("[Sync][SiliconFlow] 模型解析完成", "count", count)
}

// downloadFileAtomic 原子写入：先写 .tmp 再 rename
func downloadFileAtomic(ctx context.Context, url, destPath, bearerToken string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}

	tmpPath := destPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return err
	}
	out.Close()
	return os.Rename(tmpPath, destPath)
}
