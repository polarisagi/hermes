package router

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/polarisagi/hermes/internal/domain"
	"github.com/polarisagi/hermes/internal/pool"
	"github.com/polarisagi/hermes/internal/store"
)

var ErrNoAvailableModel = errors.New("no available model found for the requested capability tier")

type TargetPlatformRoute struct {
	ProviderID string
	ModelID    string
}

type routePattern struct {
	re          *regexp.Regexp
	prefix      string
	suffix      string
	targets     []TargetPlatformRoute
	specificity int
}

func (rp *routePattern) match(modelID string) bool {
	if rp.re != nil {
		return rp.re.MatchString(modelID)
	}
	if rp.prefix != "" && !strings.HasPrefix(modelID, rp.prefix) {
		return false
	}
	if rp.suffix != "" && !strings.HasSuffix(modelID, rp.suffix) {
		return false
	}
	return len(modelID) >= len(rp.prefix)+len(rp.suffix)
}

// Pipeline 实现 4 级降维匹配的路由管线，所有热路径均走内存缓存，零 DB 查询
type Pipeline struct {
	routeRepo      *store.RouteRepo
	intentRepo     *store.IntentRepo
	intentInferer  *IntentInferer
	chanManager    *pool.Manager
	routingLogRepo *store.RoutingLogRepo

	mu               sync.RWMutex
	customRouteMap   map[string][]TargetPlatformRoute
	patternRoutes    []routePattern
	wildcardRoutes   []TargetPlatformRoute
	sysIntentMap     map[string]string
	userIntentMap    map[string]string
}

func NewPipeline(
	routeRepo *store.RouteRepo,
	intentRepo *store.IntentRepo,
	intentInferer *IntentInferer,
	chanManager *pool.Manager,
	routingLogRepo *store.RoutingLogRepo,
) *Pipeline {
	return &Pipeline{
		routeRepo:      routeRepo,
		intentRepo:     intentRepo,
		intentInferer:  intentInferer,
		chanManager:    chanManager,
		routingLogRepo: routingLogRepo,
		customRouteMap: make(map[string][]TargetPlatformRoute),
		sysIntentMap:   make(map[string]string),
		userIntentMap:  make(map[string]string),
	}
}

// Reload 从数据库全量加载缓存，启动时和配置变更后调用
func (p *Pipeline) Reload(ctx context.Context) error {
	newCustomRouteMap := make(map[string][]TargetPlatformRoute)
	patternMap := make(map[string]*routePattern)
	var newWildcardRoutes []TargetPlatformRoute

	routes, err := p.routeRepo.GetUserCustomRoutes(ctx)
	if err != nil {
		slog.Warn("加载自定义路由缓存失败", "error", err)
	} else {
		for _, r := range routes {
			pat := r.RequestedModelID
			target := TargetPlatformRoute{ProviderID: r.TargetProviderID, ModelID: r.TargetModelID}

			if pat == "*" {
				newWildcardRoutes = append(newWildcardRoutes, target)
			} else if strings.ContainsAny(pat, "^$|()[]+") {
				if rp, exists := patternMap[pat]; exists {
					rp.targets = append(rp.targets, target)
				} else {
					re, err := regexp.Compile(pat)
					if err != nil {
						slog.Warn("无效的正则路由规则，已忽略", "pattern", pat, "error", err)
					} else {
						patternMap[pat] = &routePattern{
							re:          re,
							targets:     []TargetPlatformRoute{target},
							specificity: 1000,
						}
					}
				}
			} else if strings.Contains(pat, "*") {
				idx := strings.Index(pat, "*")
				prefix, suffix := pat[:idx], pat[idx+1:]
				if rp, exists := patternMap[pat]; exists {
					rp.targets = append(rp.targets, target)
				} else {
					patternMap[pat] = &routePattern{
						prefix:      prefix,
						suffix:      suffix,
						targets:     []TargetPlatformRoute{target},
						specificity: len(prefix) + len(suffix),
					}
				}
			} else {
				newCustomRouteMap[pat] = append(newCustomRouteMap[pat], target)
			}
		}
	}

	newPatternRoutes := make([]routePattern, 0, len(patternMap))
	for _, rp := range patternMap {
		newPatternRoutes = append(newPatternRoutes, *rp)
	}
	sort.Slice(newPatternRoutes, func(i, j int) bool {
		return newPatternRoutes[i].specificity > newPatternRoutes[j].specificity
	})

	newSysIntentMap, err := p.intentRepo.GetAllSysIntents(ctx)
	if err != nil {
		slog.Warn("加载系统意图字典缓存失败", "error", err)
		newSysIntentMap = make(map[string]string)
	}

	newUserIntentMap, err := p.intentRepo.GetAllUserIntents(ctx)
	if err != nil {
		slog.Warn("加载用户意图字典缓存失败", "error", err)
		newUserIntentMap = make(map[string]string)
	}

	p.mu.Lock()
	p.customRouteMap = newCustomRouteMap
	p.patternRoutes = newPatternRoutes
	p.wildcardRoutes = newWildcardRoutes
	p.sysIntentMap = newSysIntentMap
	p.userIntentMap = newUserIntentMap
	p.mu.Unlock()

	slog.Info("路由缓存热重载完成",
		"exact_routes", len(newCustomRouteMap),
		"pattern_routes", len(newPatternRoutes),
		"wildcard", len(newWildcardRoutes) > 0,
		"sys_intents", len(newSysIntentMap),
		"user_intents", len(newUserIntentMap),
	)
	return nil
}

// RouteRequest 核心路由入口：返回最终调用的后台通道和真实模型名
func (p *Pipeline) RouteRequest(ctx context.Context, requestedModelID string) (*pool.ActiveChannel, string, error) {
	slog.Debug("启动 4 级智能路由管线", "requested_model", requestedModelID)

	// [优先级 1] 自定义硬路由
	if targets := p.checkCustomRoute(requestedModelID); len(targets) > 0 {
		slog.Debug("命中自定义硬路由", "targets", targets)
		
		var lastRouteErr error
		for _, target := range targets {
			ch, actualModel, err := p.chanManager.SelectBestChannelByProviderAndModel(target.ProviderID, target.ModelID)
			if err == nil {
				p.routingLogRepo.SaveAsync(&domain.RoutingLog{
					RequestedModel: requestedModelID,
					ResolvedTier:   "custom",
					ResolutionSrc:  "custom_route",
					ProviderName:   ch.Provider.ProviderID,
					ActualModel:    actualModel,
					UserProviderID: ch.Provider.ID,
				})
				slog.Info("自定义路由匹配成功", "requested_model", requestedModelID, "actual_model", actualModel, "provider", ch.Provider.ProviderID, "account_name", ch.Provider.Name)
				return ch, actualModel, nil
			}
			lastRouteErr = err
		}
		slog.Warn("自定义路由候选节点均不可用，降级至意图推断", "targets", targets, "last_err", lastRouteErr)
	}

	tier, resolutionSrc := p.resolveCapabilityTier(ctx, requestedModelID)
	slog.Info("意图解析成功", "requested_model", requestedModelID, "resolved_tier", tier, "match_source", resolutionSrc)

	// Tier 级联熔断链
	var tiersToTry []string
	switch tier {
	case "fast":
		tiersToTry = []string{"fast", "smart"}
	default:
		tiersToTry = []string{"smart", "fast"}
	}

	var lastErr error

	for idx, t := range tiersToTry {
		ch, actualModel, err := p.chanManager.SelectBestChannelByTier(t, requestedModelID)
		if err != nil {
			slog.Debug("当前 tier 无可用节点，尝试下一级", "tier", t, "error", err)
			if errors.Is(err, pool.ErrAllChannelsBusy) {
				lastErr = err
			}
			continue
		}

		degraded := idx > 0
		originalTier := ""
		if degraded {
			originalTier = tier
			slog.Warn("Tier 降级成功", "requested_tier", tier, "fallback_tier", t, "model", requestedModelID)
		}

		p.routingLogRepo.SaveAsync(&domain.RoutingLog{
			RequestedModel: requestedModelID,
			ResolvedTier:   t,
			ResolutionSrc:  resolutionSrc,
			TierDegraded:   degraded,
			OriginalTier:   originalTier,
			ProviderName:   ch.Provider.ProviderID,
			ActualModel:    actualModel,
			UserProviderID: ch.Provider.ID,
		})
		slog.Info("智能路由匹配成功", "requested_model", requestedModelID, "actual_model", actualModel, "provider", ch.Provider.ProviderID, "account_name", ch.Provider.Name)
		return ch, actualModel, nil
	}

	slog.Error("没有找到匹配该请求的任何可用节点", "requested_model", requestedModelID, "tried_tiers", tiersToTry)
	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", ErrNoAvailableModel
}

func (p *Pipeline) resolveCapabilityTier(ctx context.Context, requestedModelID string) (string, string) {
	p.mu.RLock()
	userTier := p.userIntentMap[requestedModelID]
	sysTier := p.sysIntentMap[requestedModelID]
	p.mu.RUnlock()

	if userTier != "" {
		slog.Debug("命中用户级意图字典", "tier", userTier)
		return userTier, "user_dictionary"
	}

	if sysTier != "" {
		slog.Debug("命中系统级意图字典", "tier", sysTier)
		return sysTier, "system_dictionary"
	}

	slog.Warn("遇到未知请求模型，触发自动推断", "model", requestedModelID)
	inferredTier, src := p.intentInferer.InferUnknownModel(ctx, requestedModelID)
	slog.Info("自动推断引擎判定结果", "model", requestedModelID, "tier", inferredTier, "src", src)

	p.mu.Lock()
	p.userIntentMap[requestedModelID] = inferredTier
	p.mu.Unlock()

	return inferredTier, src
}

func (p *Pipeline) checkCustomRoute(requestedModelID string) []TargetPlatformRoute {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if targets, ok := p.customRouteMap[requestedModelID]; ok && len(targets) > 0 {
		return targets
	}

	for i := range p.patternRoutes {
		if p.patternRoutes[i].match(requestedModelID) {
			return p.patternRoutes[i].targets
		}
	}

	return p.wildcardRoutes
}
