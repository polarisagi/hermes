package pool

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/polarisagi/hermes/internal/domain"
	"github.com/polarisagi/hermes/internal/store"
)

var (
	ErrChannelNotFound = errors.New("channel not found")
	ErrAllChannelsBusy = errors.New("all channels are busy, cooling down, or exhausted")
)

// 节点状态机常量
const (
	StatusIdle      = 0
	StatusBusy      = 1
	StatusCooldown  = 2
	StatusProbation = 3
	StatusExhausted = 4
)

// ActiveChannel 内存中一个活跃的用户渠道实例
type ActiveChannel struct {
	Provider  *domain.UserProvider
	Models    []domain.UserModel
	Endpoints map[string]*domain.SysAccessEndpoint // Key: APIProtocol

	mu                    sync.Mutex
	Status                int
	ConcurrentConnections int
	LastAcquireTime       time.Time
	CooldownUntil         time.Time
}

// SysModelCacheInfo 缓存的系统模型元数据
type SysModelCacheInfo struct {
	ActualModelID string
	IsLegacy      bool
	VersionWeight int
}

// Manager 维护所有健康渠道的内存状态，执行并发控制与负载均衡
type Manager struct {
	providerRepo *store.ProviderRepo
	modelRepo    *store.ModelRepo

	mu        sync.RWMutex
	channels  map[int]*ActiveChannel
	sysModels map[string]map[string]SysModelCacheInfo // ModelID → ProviderID → SysModelCacheInfo
}

func NewManager(providerRepo *store.ProviderRepo, modelRepo *store.ModelRepo) *Manager {
	m := &Manager{
		providerRepo: providerRepo,
		modelRepo:    modelRepo,
		channels:     make(map[int]*ActiveChannel),
		sysModels:    make(map[string]map[string]SysModelCacheInfo),
	}
	go m.cooldownManager()
	return m
}

// filterEndpoints 根据凭证字段精确匹配最合适的系统端点，并应用用户的覆写规则
func filterEndpoints(endpoints []domain.SysAccessEndpoint, credentials []byte, userOverrides []domain.UserAccessEndpoint) map[string]*domain.SysAccessEndpoint {
	var credsMap map[string]interface{}
	if err := json.Unmarshal(credentials, &credsMap); err != nil {
		credsMap = make(map[string]interface{})
	}

	overrideMap := make(map[string]domain.UserAccessEndpoint)
	for _, uep := range userOverrides {
		overrideMap[uep.SysEndpointID] = uep
	}

	best := make(map[string]*domain.SysAccessEndpoint)
	maxFields := make(map[string]int)

	for i := range endpoints {
		ep := endpoints[i] // 拷贝一份

		override, hasOverride := overrideMap[ep.EndpointID]
		if hasOverride && !override.IsEnabled {
			continue
		}
		if hasOverride && override.CustomBaseURL != "" {
			ep.DefaultBaseURL = override.CustomBaseURL
		}

		var reqFields []string
		if err := json.Unmarshal(ep.RequiredCredentialFields, &reqFields); err != nil {
			reqFields = []string{}
		}

		satisfied := true
		for _, field := range reqFields {
			if _, exists := credsMap[field]; !exists {
				satisfied = false
				break
			}
		}

		if satisfied {
			count := len(reqFields)
			if existingCount, exists := maxFields[ep.APIProtocol]; !exists || count > existingCount {
				epPtr := ep // 指向拷贝的新指针
				best[ep.APIProtocol] = &epPtr
				maxFields[ep.APIProtocol] = count
			}
		}
	}
	return best
}

// Reload 从数据库热加载所有开启的渠道和模型
func (m *Manager) Reload(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	providers, err := m.providerRepo.GetUserProviders(ctx)
	if err != nil {
		return err
	}

	models, err := m.modelRepo.GetUserModels(ctx)
	if err != nil {
		return err
	}

	globalSysModels := make(map[string]domain.SysModel)
	if sysModelsList, err := m.modelRepo.GetSysModels(ctx); err == nil {
		for _, sm := range sysModelsList {
			globalSysModels[sm.ModelID] = sm
		}
	}

	sysModelsMap := make(map[string]map[string]SysModelCacheInfo)
	if sysProviderModels, err := m.modelRepo.GetSysProviderModels(ctx); err == nil {
		for _, pm := range sysProviderModels {
			if sysModelsMap[pm.ModelID] == nil {
				sysModelsMap[pm.ModelID] = make(map[string]SysModelCacheInfo)
			}
			isLegacy, versionWeight := false, 0
			if sm, ok := globalSysModels[pm.ModelID]; ok {
				isLegacy = sm.IsLegacy
				versionWeight = sm.VersionWeight
				if versionWeight == 0 && sm.ReleasedAt > 0 {
					versionWeight = int(sm.ReleasedAt)
				}
			}
			sysModelsMap[pm.ModelID][pm.ProviderID] = SysModelCacheInfo{
				ActualModelID: pm.ActualModelID,
				IsLegacy:      isLegacy,
				VersionWeight: versionWeight,
			}
		}
	}

	newChannels := make(map[int]*ActiveChannel)
	for _, p := range providers {
		if p.Status <= 0 {
			continue
		}

		endpointsList, err := m.providerRepo.GetSysAccessEndpointsByProvider(ctx, p.ProviderID)
		if err != nil || len(endpointsList) == 0 {
			slog.Warn("加载系统端点失败或无端点，跳过渠道", "provider", p.Name, "provider_id", p.ProviderID, "error", err)
			continue
		}

		provCopy := p
		ch := &ActiveChannel{
			Provider:  &provCopy,
			Endpoints: filterEndpoints(endpointsList, p.AuthCredentials, p.Endpoints),
			Status:    StatusIdle,
		}

		if ch.Provider.Balance > 0 {
			limit := ch.Provider.Balance
			if ch.Provider.LimitPercent > 0 {
				limit = limit * float64(ch.Provider.LimitPercent) / 100.0
			}
			if ch.Provider.UsedAmount >= limit {
				ch.Status = StatusExhausted
			}
		}

		newChannels[p.ID] = ch
	}

	for _, mod := range models {
		if ch, exists := newChannels[mod.UserProviderID]; exists {
			ch.Models = append(ch.Models, mod)
		}
	}

	// 继承存量请求的内存状态，避免热重载中断进行中的连接
	for id, newCh := range newChannels {
		if oldCh, exists := m.channels[id]; exists {
			newCh.mu.Lock()
			oldCh.mu.Lock()
			newCh.Status = oldCh.Status
			newCh.ConcurrentConnections = oldCh.ConcurrentConnections
			newCh.LastAcquireTime = oldCh.LastAcquireTime
			newCh.CooldownUntil = oldCh.CooldownUntil
			oldCh.mu.Unlock()
			newCh.mu.Unlock()
		}
	}

	m.channels = newChannels
	m.sysModels = sysModelsMap
	return nil
}

func (m *Manager) resolveActualModelID(modelID, providerID string) SysModelCacheInfo {
	if providers, ok := m.sysModels[modelID]; ok {
		if info, ok := providers[providerID]; ok {
			return info
		}
	}
	return SysModelCacheInfo{ActualModelID: modelID}
}

// selectBest 通用核心：筛选、排序、CAS 抢占。filter 返回 (targetModelID, info, affinityScore, isMatch)
func (m *Manager) selectBest(filter func(*ActiveChannel) (string, SysModelCacheInfo, int, bool)) (*ActiveChannel, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	type candidate struct {
		ch              *ActiveChannel
		targetModelID   string
		info            SysModelCacheInfo
		affinity        int
		lastAcquireTime time.Time
	}
	var candidates []candidate

	for _, ch := range m.channels {
		targetModel, info, affinity, matched := filter(ch)
		if !matched {
			continue
		}

		ch.mu.Lock()
		avail := ch.Status == StatusIdle || ch.Status == StatusProbation
		if avail {
			if ch.Status == StatusIdle && ch.Provider.ConcurrencyLimit > 0 && ch.ConcurrentConnections >= ch.Provider.ConcurrencyLimit {
				avail = false
			} else if ch.Status == StatusProbation && ch.ConcurrentConnections >= 1 {
				avail = false
			}
		}
		if avail && !ch.LastAcquireTime.IsZero() && ch.Provider.MinIntervalSec > 0 {
			if time.Since(ch.LastAcquireTime) < time.Duration(ch.Provider.MinIntervalSec)*time.Second {
				avail = false
			}
		}
		lastAcq := ch.LastAcquireTime
		ch.mu.Unlock()

		if avail {
			candidates = append(candidates, candidate{ch: ch, targetModelID: targetModel, info: info, affinity: affinity, lastAcquireTime: lastAcq})
		}
	}

	if len(candidates) == 0 {
		return nil, "", ErrAllChannelsBusy
	}

	// 排序：模型亲和力 > 非Legacy > 版本权重高 > 优先级小 > LRU
	sort.SliceStable(candidates, func(i, j int) bool {
		ai, aj := candidates[i].affinity, candidates[j].affinity
		if ai != aj {
			return ai > aj
		}
		li, lj := candidates[i].info.IsLegacy, candidates[j].info.IsLegacy
		if li != lj {
			return !li
		}
		wi, wj := candidates[i].info.VersionWeight, candidates[j].info.VersionWeight
		if wi != wj {
			return wi > wj
		}
		pi, pj := candidates[i].ch.Provider.Priority, candidates[j].ch.Provider.Priority
		if pi != pj {
			return pi < pj
		}
		ti, tj := candidates[i].lastAcquireTime, candidates[j].lastAcquireTime
		if ti.IsZero() != tj.IsZero() {
			return ti.IsZero()
		}
		return ti.Before(tj)
	})

	// CAS 抢占
	for _, c := range candidates {
		ch := c.ch
		ch.mu.Lock()

		if ch.Status != StatusIdle && ch.Status != StatusProbation {
			ch.mu.Unlock()
			continue
		}
		if ch.Status == StatusIdle && ch.Provider.ConcurrencyLimit > 0 && ch.ConcurrentConnections >= ch.Provider.ConcurrencyLimit {
			ch.mu.Unlock()
			continue
		}
		if ch.Status == StatusProbation && ch.ConcurrentConnections >= 1 {
			ch.mu.Unlock()
			continue
		}
		if !ch.LastAcquireTime.IsZero() && ch.Provider.MinIntervalSec > 0 && time.Since(ch.LastAcquireTime) < time.Duration(ch.Provider.MinIntervalSec)*time.Second {
			ch.mu.Unlock()
			continue
		}

		ch.ConcurrentConnections++
		if ch.Status == StatusIdle && ch.Provider.ConcurrencyLimit > 0 && ch.ConcurrentConnections >= ch.Provider.ConcurrencyLimit {
			ch.Status = StatusBusy
		}
		ch.LastAcquireTime = time.Now()
		ch.mu.Unlock()

		slog.Debug("负载均衡抢占成功", "channel", ch.Provider.Name, "model", c.targetModelID)
		return ch, c.targetModelID, nil
	}

	return nil, "", ErrAllChannelsBusy
}

// SelectBestChannelByTier 极简模式负载均衡：按能力梯队选最优渠道
func (m *Manager) SelectBestChannelByTier(tier string, requestedModelID string) (*ActiveChannel, string, error) {
	return m.selectBest(func(ch *ActiveChannel) (string, SysModelCacheInfo, int, bool) {
		var bestMod *domain.UserModel
		for i := range ch.Models {
			mod := &ch.Models[i]
			if mod.CapabilityTier == tier && mod.IsActive {
				if bestMod == nil {
					bestMod = mod
				} else {
					if bestMod.IsLegacy && !mod.IsLegacy {
						bestMod = mod
					} else if bestMod.IsLegacy == mod.IsLegacy && mod.VersionWeight > bestMod.VersionWeight {
						bestMod = mod
					}
				}
			}
		}
		if bestMod != nil {
			info := m.resolveActualModelID(bestMod.ModelID, ch.Provider.ProviderID)
			
			affinity := 0
			lowerActual := strings.ToLower(info.ActualModelID)
			lowerReq := strings.ToLower(requestedModelID)
			if lowerActual == lowerReq {
				affinity = 2
			} else if strings.Contains(lowerActual, lowerReq) || strings.Contains(lowerReq, lowerActual) {
				affinity = 1
			}

			return info.ActualModelID, info, affinity, true
		}
		return "", SysModelCacheInfo{}, 0, false
	})
}

// SelectBestChannelByProviderAndModel 专业模式负载均衡：在指定厂商和模型内选最优渠道
func (m *Manager) SelectBestChannelByProviderAndModel(providerID, modelID string) (*ActiveChannel, string, error) {
	if providerID == "" || modelID == "" {
		return nil, "", ErrChannelNotFound
	}

	return m.selectBest(func(ch *ActiveChannel) (string, SysModelCacheInfo, int, bool) {
		if ch.Provider.ProviderID != providerID {
			return "", SysModelCacheInfo{}, 0, false
		}

		var bestMod *domain.UserModel
		for i := range ch.Models {
			mod := &ch.Models[i]
			if mod.ModelID == modelID && mod.IsActive {
				if bestMod == nil {
					bestMod = mod
				} else {
					if bestMod.IsLegacy && !mod.IsLegacy {
						bestMod = mod
					} else if bestMod.IsLegacy == mod.IsLegacy && mod.VersionWeight > bestMod.VersionWeight {
						bestMod = mod
					}
				}
			}
		}
		if bestMod != nil {
			info := m.resolveActualModelID(bestMod.ModelID, ch.Provider.ProviderID)
			return info.ActualModelID, info, 2, true
		}
		return "", SysModelCacheInfo{}, 0, false
	})
}

// ReleaseChannel 归还并发连接
func (m *Manager) ReleaseChannel(ch *ActiveChannel) {
	if ch == nil {
		return
	}
	ch.mu.Lock()
	defer ch.mu.Unlock()
	if ch.ConcurrentConnections > 0 {
		ch.ConcurrentConnections--
	}
	if ch.Status == StatusBusy {
		ch.Status = StatusIdle
	}
}

// ReportSuccess 请求成功，Probation 状态恢复为 Idle
func (m *Manager) ReportSuccess(ch *ActiveChannel) {
	if ch == nil {
		return
	}
	ch.mu.Lock()
	defer ch.mu.Unlock()
	if ch.Status == StatusProbation {
		slog.Info("渠道试探成功，恢复为 Idle", "channel", ch.Provider.Name)
		ch.Status = StatusIdle
	}
}

// ReportError 请求失败，触发熔断或隔离
func (m *Manager) ReportError(ch *ActiveChannel, statusCode int) {
	if ch == nil {
		return
	}
	ch.mu.Lock()
	defer ch.mu.Unlock()

	if statusCode == 402 || statusCode == 403 {
		slog.Warn("渠道预算耗尽或被禁，标记为 Exhausted", "channel", ch.Provider.Name, "status_code", statusCode)
		ch.Status = StatusExhausted
		return
	}

	if statusCode == 429 || statusCode >= 500 {
		cooldown := 10 * time.Second
		if time.Now().Before(ch.CooldownUntil.Add(1 * time.Minute)) {
			cooldown = 30 * time.Second
		}
		ch.Status = StatusCooldown
		ch.CooldownUntil = time.Now().Add(cooldown)
		slog.Warn("渠道遭遇限流或服务端错误，进入 Cooldown",
			"channel", ch.Provider.Name,
			"status_code", statusCode,
			"cooldown_until", ch.CooldownUntil.Format(time.RFC3339))
	}
}

// cooldownManager 守护协程，定期将 Cooldown 渠道恢复到 Probation
func (m *Manager) cooldownManager() {
	for {
		time.Sleep(1 * time.Second)
		now := time.Now()
		m.mu.RLock()
		for _, ch := range m.channels {
			ch.mu.Lock()
			if ch.Status == StatusCooldown && now.After(ch.CooldownUntil) {
				ch.Status = StatusProbation
				ch.LastAcquireTime = now
				slog.Info("渠道冷却结束，进入 Probation", "channel", ch.Provider.Name)
			}
			ch.mu.Unlock()
		}
		m.mu.RUnlock()
	}
}

func (m *Manager) GetStats() (active, waiting, max int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, ch := range m.channels {
		ch.mu.Lock()
		active += ch.ConcurrentConnections
		if ch.Provider.ConcurrencyLimit > 0 {
			max += ch.Provider.ConcurrencyLimit
		} else {
			// 如果没有限制，可以认为是一个很大的值或者不计入总和
			max += 100
		}
		ch.mu.Unlock()
	}
	// 目前没有明确的排队等待队列机制，返回0
	return active, 0, max
}

// AddUsage updates the provider's used amount in memory, checks the limit, and triggers an async DB update.
func (m *Manager) AddUsage(providerID int, cost float64) {
	if cost <= 0 {
		return
	}

	m.mu.RLock()
	ch, exists := m.channels[providerID]
	m.mu.RUnlock()

	if !exists {
		return
	}

	ch.mu.Lock()
	ch.Provider.UsedAmount += cost
	
	if ch.Provider.Balance > 0 {
		limit := ch.Provider.Balance
		if ch.Provider.LimitPercent > 0 {
			limit = limit * float64(ch.Provider.LimitPercent) / 100.0
		}
		if ch.Provider.UsedAmount >= limit && ch.Status != StatusExhausted {
			ch.Status = StatusExhausted
			slog.Warn("渠道由于余额耗尽或达到百分比限制，已自动被禁用", "channel", ch.Provider.Name, "used", ch.Provider.UsedAmount, "limit", limit)
		}
	}
	ch.mu.Unlock()

	m.providerRepo.IncrementUsedAmountAsync(providerID, cost)
}
