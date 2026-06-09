package sync

import (
	"context"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/polarisagi/hermes/internal/domain"
	"github.com/polarisagi/hermes/internal/store"
)

// cutoff2025 是 2025-01-01 00:00:00 UTC 的 Unix 时间戳，早于此值的模型拒绝晋升
const cutoff2025 = int64(1735689600)

// PromoteService 将暂存表数据按规则晋升到 sys_* 正式表
type PromoteService struct {
	extCacheRepo *store.ExternalCacheRepo
	modelRepo    *store.ModelRepo
	intentRepo   *store.IntentRepo
	providerRepo *store.ProviderRepo
}

func NewPromoteService(
	extCacheRepo *store.ExternalCacheRepo,
	modelRepo *store.ModelRepo,
	intentRepo *store.IntentRepo,
	providerRepo *store.ProviderRepo,
) *PromoteService {
	return &PromoteService{
		extCacheRepo: extCacheRepo,
		modelRepo:    modelRepo,
		intentRepo:   intentRepo,
		providerRepo: providerRepo,
	}
}

var familySuffixRe = regexp.MustCompile(`(?i)(-\d{4}-?\d{2}-?\d{2}|-\d{4}|:.*|-preview.*|-latest.*|-instruct.*|-chat.*|-vision.*)$`)

func extractModelFamily(modelID string) string {
	family := familySuffixRe.ReplaceAllString(modelID, "")
	return strings.TrimSuffix(family, "-")
}

// PromoteAll 对所有 pending 记录执行晋升或拒绝
func (p *PromoteService) PromoteAll(ctx context.Context) error {
	promoted, rejected, pending := 0, 0, 0

	models, err := p.extCacheRepo.GetPendingModels(ctx)
	if err != nil {
		return err
	}

	// 按 Model Family 分组
	familyGroups := make(map[string][]domain.ExternalModelCache)
	for _, m := range models {
		action, reason := p.classifyModel(&m)
		if action == "reject" {
			_ = p.extCacheRepo.MarkModelRejected(ctx, m.ID, reason)
			rejected++
			continue
		}
		if action == "pending" {
			pending++
			continue
		}
		
		family := extractModelFamily(m.ModelID)
		familyGroups[family] = append(familyGroups[family], m)
	}

	// 针对每个分组，按时间和权重降序排序，仅保留前2名
	for _, group := range familyGroups {
		sort.Slice(group, func(i, j int) bool {
			if group[i].ReleasedAt != group[j].ReleasedAt {
				return group[i].ReleasedAt > group[j].ReleasedAt
			}
			return group[i].ModelID > group[j].ModelID // Fallback to lexicographical descending (e.g. -20240806 > -20240513)
		})

		for i, m := range group {
			if i < 2 {
				if err := p.promoteModel(ctx, &m); err != nil {
					slog.Warn("[Promote] 模型晋升失败", "model_id", m.ModelID, "error", err)
					continue
				}
				_ = p.extCacheRepo.MarkModelPromoted(ctx, m.ID)
				promoted++
			} else {
				_ = p.extCacheRepo.MarkModelRejected(ctx, m.ID, domain.RejectReasonOutdatedVersion)
				rejected++
			}
		}
	}

	providers, err := p.extCacheRepo.GetPendingProviders(ctx)
	if err != nil {
		return err
	}
	provPromoted, provSkipped := 0, 0
	for _, prov := range providers {
		if err := p.promoteProvider(ctx, &prov); err != nil {
			slog.Warn("[Promote] 厂商晋升失败", "provider_id", prov.ProviderID, "error", err)
			provSkipped++
			continue
		}
		_ = p.extCacheRepo.MarkProviderPromoted(ctx, prov.ID)
		provPromoted++
	}

	slog.Info("[Promote] 晋升完成",
		"model_promoted", promoted,
		"model_rejected", rejected,
		"model_pending", pending,
		"provider_promoted", provPromoted,
		"provider_skipped", provSkipped,
	)
	return nil
}

func (p *PromoteService) classifyModel(m *domain.ExternalModelCache) (string, string) {
	if m.IsLegacy {
		return "reject", domain.RejectReasonIsLegacy
	}
	if m.ReleasedAt > 0 && m.ReleasedAt < cutoff2025 {
		return "reject", domain.RejectReasonPre2025
	}
	if m.ReleasedAt >= cutoff2025 {
		return "promote", ""
	}
	if m.CapabilityTier != "" {
		return "promote", ""
	}
	return "pending", ""
}

func (p *PromoteService) promoteModel(ctx context.Context, m *domain.ExternalModelCache) error {
	sysModel := &domain.SysModel{
		ModelID:              m.ModelID,
		DisplayName:          m.DisplayName,
		CapabilityTier:       m.CapabilityTier,
		ContextLength:        m.ContextLength,
		MaxOutputTokens:      m.MaxOutputTokens,
		SupportsVision:       m.SupportsVision,
		SupportsAudioInput:   m.SupportsAudioInput,
		SupportsAudioOutput:  m.SupportsAudioOutput,
		SupportsTools:        m.SupportsTools,
		PromptPricePer1k:     m.PromptPricePer1k,
		CompletionPricePer1k: m.CompletionPricePer1k,
		ReleasedAt:           m.ReleasedAt,
		IsLegacy:             m.IsLegacy,
		IsActive:             true,
	}

	if err := p.modelRepo.UpsertSysModel(ctx, sysModel); err != nil {
		return err
	}

	if m.CapabilityTier != "" {
		if err := p.intentRepo.SaveSysIntent(ctx, &domain.UserModelIntentDict{
			ModelID:        m.ModelID,
			CapabilityTier: m.CapabilityTier,
			Source:         "sync_" + m.Source,
		}); err != nil {
			slog.Warn("[Promote] 意图字典写入失败", "model_id", m.ModelID, "error", err)
		}
	}
	return nil
}

func (p *PromoteService) promoteProvider(ctx context.Context, prov *domain.ExternalProviderCache) error {
	return p.providerRepo.InsertSysProviderIfNotExists(ctx, &domain.SysProvider{
		ProviderID:   prov.ProviderID,
		ProviderName: prov.ProviderName,
		APIProtocol:  prov.APIProtocol,
	})
}

// ScheduledSync 封装"同步 + 晋升"完整流程，供定时器调用
func ScheduledSync(ctx context.Context, syncSvc *SyncService, promoteSvc *PromoteService) {
	if err := syncSvc.SyncGlobalModels(ctx); err != nil {
		slog.Error("[Sync] 全局模型同步失败", "error", err)
		return
	}
	if err := promoteSvc.PromoteAll(ctx); err != nil {
		slog.Error("[Promote] 晋升失败", "error", err)
	}
}

// NextDailyRun 计算距下次 UTC 凌晨 3 点的等待时间（错峰执行）
func NextDailyRun() time.Duration {
	now := time.Now().UTC()
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 3, 0, 0, 0, time.UTC)
	return time.Until(next)
}
