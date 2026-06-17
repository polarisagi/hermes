package pool

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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

// DefaultWaitTimeout 请求在排队等待时的最长等待时间（超过后返回 ErrAllChannelsBusy）
// 设置为 5 分钟：符合 AI 编码工具实际超时范围，超过 5min 排队说明真正过载
const DefaultWaitTimeout = 5 * time.Minute

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

// waitResult 调度器分配给等待请求的结果
type waitResult struct {
	ch    *ActiveChannel
	model string
	err   error
}

// waitRequest 代表一个正在公平队列中等待的客户端请求
type waitRequest struct {
	ctx        context.Context
	filter     func(*ActiveChannel) (string, SysModelCacheInfo, int, bool)
	resultCh   chan waitResult // 缓冲为 1
	enqueuedAt time.Time
}

// clientQueue 单个客户端的 FIFO 请求列表
type clientQueue struct {
	clientID       string
	requests       []*waitRequest
	firstArrivedAt time.Time
}

// Manager 维护所有健康渠道的内存状态，执行并发控制与负载均衡
type Manager struct {
	providerRepo *store.ProviderRepo
	modelRepo    *store.ModelRepo

	mu           sync.RWMutex
	channels     map[int]*ActiveChannel
	sysModels    map[string]map[string]SysModelCacheInfo // ModelID → ProviderID → SysModelCacheInfo
	waitingCount int64                                   // 当前正在排队等待渠道的请求数（原子操作）

	// 公平调度队列：per-client round-robin
	queueMu        sync.Mutex
	clientQueues   map[string]*clientQueue // clientID → per-client FIFO 队列
	clientOrder    []string                // 按首次到达顺序排列，用于 round-robin
	dispatchNotify chan struct{}            // 有缓冲(1)，渠道释放/冷却结束时触发调度器
}

func NewManager(providerRepo *store.ProviderRepo, modelRepo *store.ModelRepo) *Manager {
	m := &Manager{
		providerRepo:   providerRepo,
		modelRepo:      modelRepo,
		channels:       make(map[int]*ActiveChannel),
		sysModels:      make(map[string]map[string]SysModelCacheInfo),
		clientQueues:   make(map[string]*clientQueue),
		dispatchNotify: make(chan struct{}, 1),
	}
	go m.cooldownManager()
	go m.dispatcherLoop()
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
	matchedCount := 0 // 匹配到 filter 的渠道总数（不论是否可用），用于区分"无匹配"和"全繁忙"

	for _, ch := range m.channels {
		targetModel, info, affinity, matched := filter(ch)
		if !matched {
			continue
		}
		matchedCount++

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
		if matchedCount == 0 {
			// 没有任何渠道能匹配此请求（模型未配置/厂商不存在）→ 快速报错，不需等待
			return nil, "", ErrChannelNotFound
		}
		// 渠道存在但全部繁忙/冷却/Exhausted
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

// computeNextAvailableTime 扫描所有匹配渠道，计算最近某个渠道何时可用（基于 MinIntervalSec / Cooldown）
// 返回零值表示没有匹配的渠道（或全部 Exhausted）
func (m *Manager) computeNextAvailableTime(filter func(*ActiveChannel) (string, SysModelCacheInfo, int, bool)) time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	var earliest time.Time

	for _, ch := range m.channels {
		_, _, _, matched := filter(ch)
		if !matched {
			continue
		}

		ch.mu.Lock()
		status := ch.Status
		lastAcq := ch.LastAcquireTime
		coolUntil := ch.CooldownUntil
		minInterval := ch.Provider.MinIntervalSec
		ch.mu.Unlock()

		if status == StatusExhausted {
			continue
		}

		var candidate time.Time
		if status == StatusCooldown && coolUntil.After(now) {
			// Cooldown 结束即可变为 Probation，尝试调度
			candidate = coolUntil
		} else if minInterval > 0 && !lastAcq.IsZero() {
			next := lastAcq.Add(time.Duration(minInterval) * time.Second)
			if next.After(now) {
				candidate = next
			} else {
				// 间隔已经过了，但可能被并发数占满，1 秒后轮询
				candidate = now.Add(1 * time.Second)
			}
		} else {
			// 并发数限制：任意时刻都可能释放，1 秒后轮询
			candidate = now.Add(1 * time.Second)
		}

		if earliest.IsZero() || candidate.Before(earliest) {
			earliest = candidate
		}
	}

	return earliest
}

// drainCancelledHead 从客户端队列头部清除已取消的请求
func drainCancelledHead(requests []*waitRequest) []*waitRequest {
	for len(requests) > 0 {
		select {
		case <-requests[0].ctx.Done():
			requests = requests[1:]
		default:
			return requests
		}
	}
	return requests
}

// notifyDispatcher 通知调度器有渠道变化（非阻塞，已有待处理通知时跳过）
func (m *Manager) notifyDispatcher() {
	select {
	case m.dispatchNotify <- struct{}{}:
	default:
	}
}

// tryDispatch Multi-Client Round-Robin 公平调度：
// 每轮从每个客户的队列各取一个请求尝试分配，差差轮流直到所有人都无法分配为止
// 客户端内部保持 FIFO，客户端间保持公平
func (m *Manager) tryDispatch() {
	m.queueMu.Lock()
	defer m.queueMu.Unlock()

	for {
		anyServed := false

		// 一轮遍历所有客户端，每个客户端最多服务一个请求（round-robin）
		for _, clientID := range m.clientOrder {
			cq := m.clientQueues[clientID]
			if cq == nil {
				continue
			}

			// 惰性清除队列头部已取消的请求
			cq.requests = drainCancelledHead(cq.requests)
			if len(cq.requests) == 0 {
				continue
			}

			head := cq.requests[0]
			ch, model, err := m.selectBest(head.filter)
			if errors.Is(err, ErrAllChannelsBusy) {
				// 这个客户端用的渠道此刻全部繁忙，跳过，继续服务下个客户端
				continue
			}

			// 服务成功（或永久错误）：移出队列并通知请求方
			cq.requests = cq.requests[1:]
			head.resultCh <- waitResult{ch: ch, model: model, err: err}
			anyServed = true
			slog.Info("公平调度：成功为客户端分配渠道",
				"client", clientID,
				"wait_duration", time.Since(head.enqueuedAt).Truncate(time.Millisecond),
			)
		}

		if !anyServed {
			// 所有客户端要么没有请求，要么全部渠道繁忙，当轮结束
			break
		}
		// 当轮服务了请求，开始下一轮（可能有更多渠道可用）
	}

	// 清理空队列的客户端，保持 clientOrder 干净
	cleanOrder := make([]string, 0, len(m.clientOrder))
	for _, clientID := range m.clientOrder {
		cq := m.clientQueues[clientID]
		if cq != nil {
			cq.requests = drainCancelledHead(cq.requests)
		}
		if cq == nil || len(cq.requests) == 0 {
			delete(m.clientQueues, clientID)
		} else {
			cleanOrder = append(cleanOrder, clientID)
		}
	}
	m.clientOrder = cleanOrder
}

// dispatcherLoop 中央调度 goroutine：监听通知并精确定时，驱动公平 round-robin 队列
func (m *Manager) dispatcherLoop() {
	for {
		// 根据队列中任意客户端的过滤器计算最优睡眠时长
		m.queueMu.Lock()
		var anyFilter func(*ActiveChannel) (string, SysModelCacheInfo, int, bool)
		for _, clientID := range m.clientOrder {
			cq := m.clientQueues[clientID]
			if cq != nil && len(cq.requests) > 0 {
				anyFilter = cq.requests[0].filter
				break
			}
		}
		m.queueMu.Unlock()

		var sleepDur time.Duration
		if anyFilter != nil {
			// 有排队请求：精确等到最近渠道可用
			next := m.computeNextAvailableTime(anyFilter)
			if next.IsZero() || !next.After(time.Now()) {
				sleepDur = 1 * time.Second
			} else {
				sleepDur = time.Until(next)
				if sleepDur < 50*time.Millisecond {
					sleepDur = 50 * time.Millisecond
				}
			}
		} else {
			// 队列为空：等待通知即可（最长等 1 小时防止意外卡死）
			sleepDur = 1 * time.Hour
		}

		select {
		case <-m.dispatchNotify:
		case <-time.After(sleepDur):
		}

		m.tryDispatch()
	}
}

// selectBestWithWait 公平 FIFO 入口：先快速尝试，失败后按 clientID 加入 per-client 公平队列，
// 由 dispatcherLoop 进行 round-robin 公平分配，最长等待 maxWait
// 若模型未配置（ErrChannelNotFound），立即报错不进队列
func (m *Manager) selectBestWithWait(ctx context.Context, clientID string, filter func(*ActiveChannel) (string, SysModelCacheInfo, int, bool), maxWait time.Duration) (*ActiveChannel, string, error) {
	if clientID == "" {
		clientID = "unknown"
	}
	// 第一次快速尝试（无需入队）
	ch, model, err := m.selectBest(filter)
	if err == nil {
		return ch, model, nil
	}
	// 模型/厂商未配置 → 无需等待，立即报错
	if errors.Is(err, ErrChannelNotFound) {
		slog.Warn("请求模型无任何匹配渠道（未配置），快速报错不进队列", "client", clientID)
		return nil, "", err
	}
	if !errors.Is(err, ErrAllChannelsBusy) {
		return nil, "", err
	}

	// 所有渠道繁忙 → 加入 per-client 公平队列
	waiter := &waitRequest{
		ctx:        ctx,
		filter:     filter,
		resultCh:   make(chan waitResult, 1),
		enqueuedAt: time.Now(),
	}

	m.queueMu.Lock()
	cq := m.clientQueues[clientID]
	if cq == nil {
		cq = &clientQueue{
			clientID:       clientID,
			firstArrivedAt: time.Now(),
		}
		m.clientQueues[clientID] = cq
		m.clientOrder = append(m.clientOrder, clientID)
	}
	cq.requests = append(cq.requests, waiter)
	clientQueueLen := len(cq.requests)
	totalWaiting := 0
	for _, c := range m.clientQueues {
		totalWaiting += len(c.requests)
	}
	m.queueMu.Unlock()

	atomic.AddInt64(&m.waitingCount, 1)
	defer atomic.AddInt64(&m.waitingCount, -1)

	slog.Info("请求进入公平排队队列",
		"client", clientID,
		"client_queue_pos", clientQueueLen,
		"total_waiting", totalWaiting,
		"max_wait", maxWait,
	)

	select {
	case <-ctx.Done():
		slog.Info("客户端已断开，取消公平排队等待", "client", clientID, "reason", ctx.Err())
		return nil, "", ctx.Err()
	case <-time.After(maxWait):
		slog.Warn("公平排队等待超时", "client", clientID, "max_wait", maxWait)
		return nil, "", ErrAllChannelsBusy
	case result := <-waiter.resultCh:
		return result.ch, result.model, result.err
	}
}

// SelectBestChannelByTier 极简模式负载均衡：按能力梯队选最优渠道（公平排队）
func (m *Manager) SelectBestChannelByTier(ctx context.Context, clientID, tier, requestedModelID string) (*ActiveChannel, string, error) {
	filter := func(ch *ActiveChannel) (string, SysModelCacheInfo, int, bool) {
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
	}
	return m.selectBestWithWait(ctx, clientID, filter, DefaultWaitTimeout)
}

// SelectBestChannelByProviderAndModel 专业模式负载均衡：在指定厂商和模型内选最优渠道（公平排队）
func (m *Manager) SelectBestChannelByProviderAndModel(ctx context.Context, clientID, providerID, modelID string) (*ActiveChannel, string, error) {
	if providerID == "" || modelID == "" {
		return nil, "", ErrChannelNotFound
	}

	filter := func(ch *ActiveChannel) (string, SysModelCacheInfo, int, bool) {
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
	}
	return m.selectBestWithWait(ctx, clientID, filter, DefaultWaitTimeout)
}

// ReleaseChannel 归还并发连接，并通知 FIFO 调度器有渠道释放
func (m *Manager) ReleaseChannel(ch *ActiveChannel) {
	if ch == nil {
		return
	}
	ch.mu.Lock()
	if ch.ConcurrentConnections > 0 {
		ch.ConcurrentConnections--
	}
	if ch.Status == StatusBusy {
		ch.Status = StatusIdle
	}
	ch.mu.Unlock()
	// 通知调度器：有渠道释放，可以服务等待队列中的下一个请求
	m.notifyDispatcher()
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

// cooldownManager 守护协程，定期将 Cooldown 渠道恢复到 Probation，并通知调度器
func (m *Manager) cooldownManager() {
	for {
		time.Sleep(1 * time.Second)
		now := time.Now()
		notified := false
		m.mu.RLock()
		for _, ch := range m.channels {
			ch.mu.Lock()
			if ch.Status == StatusCooldown && now.After(ch.CooldownUntil) {
				ch.Status = StatusProbation
				ch.LastAcquireTime = now
				slog.Info("渠道冷却结束，进入 Probation", "channel", ch.Provider.Name)
				notified = true
			}
			ch.mu.Unlock()
		}
		m.mu.RUnlock()
		// 有渠道从 Cooldown 恢复时，通知 FIFO 调度器尝试服务等待队列
		if notified {
			m.notifyDispatcher()
		}
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
	// 返回真实的排队等待请求数（原子计数器）
	waiting = int(atomic.LoadInt64(&m.waitingCount))
	return active, waiting, max
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
