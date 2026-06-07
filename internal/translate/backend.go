// Package translate 协议转换公共层：后端类型检测与 effort 映射。
//
// 所有翻译器共享的后端类型定义、后端检测逻辑、跨协议 effort 映射函数。
// 任何翻译器在需要判断目标后端类型或映射思考级别时，都应使用此包中的函数，
// 避免在各个 to* 子包中重复定义。
package translate

import (
	"strings"

	"github.com/polarisagi/hermes/internal/domain"
)

// ── 后端类型 ──────────────────────────────────────────────────────────────────

// BackendKind 目标后端类型，用于决定参数适配策略。
//
// 支持三类后端：
//   - BackendGeneric：通用 OpenAI 兼容厂商
//   - BackendOpenAI：OpenAI 官方 (api.openai.com)
//   - BackendDeepSeek：DeepSeek (api.deepseek.com)
type BackendKind int

const (
	BackendGeneric  BackendKind = iota // 通用 OpenAI 兼容
	BackendOpenAI                      // OpenAI 官方
	BackendDeepSeek                    // DeepSeek
)

func (k BackendKind) String() string {
	switch k {
	case BackendOpenAI:
		return "openai"
	case BackendDeepSeek:
		return "deepseek"
	default:
		return "generic"
	}
}

// DetectBackend 根据 provider 和 endpoint 信息检测目标后端类型。
//
// 检测优先级：
//  1. endpoint URL 包含 api.openai.com → BackendOpenAI
//  2. endpoint URL 包含 deepseek → BackendDeepSeek
//  3. providerID 为 "openai" → BackendOpenAI
//  4. providerID 包含 deepseek → BackendDeepSeek
//  5. 其他 → BackendGeneric
func DetectBackend(provider *domain.UserProvider, ep *domain.SysAccessEndpoint) BackendKind {
	urls := []string{strings.ToLower(provider.ProviderID)}
	if ep != nil {
		urls = append(urls, strings.ToLower(ep.DefaultBaseURL))
	}
	for _, u := range urls {
		if strings.Contains(u, "api.openai.com") || u == "openai" {
			return BackendOpenAI
		}
		if strings.Contains(u, "deepseek") {
			return BackendDeepSeek
		}
	}
	return BackendGeneric
}

// ── Effort 映射（跨协议统一）──────────────────────────────────────────────────
//
// 各厂商的思考级别名称和档位数量各不相同，此处提供统一的转换函数。
// 所有函数接受一个通用 effort 字符串（来自任意源协议），返回目标协议对应的值。
//
// 通用 effort 值域（超集）：
//   none / minimal / low / medium / high / xhigh / max / ultra_code

// MapEffortToOpenAI 将通用 effort 映射到 OpenAI 官方五档。
//
// OpenAI 支持：none / low / medium / high / xhigh
func MapEffortToOpenAI(effort string) string {
	switch strings.ToLower(effort) {
	case "max", "ultra_code":
		return "xhigh"
	case "xhigh":
		return "xhigh"
	case "high", "":
		return "high"
	case "medium":
		return "medium"
	case "minimal", "low":
		return "low"
	case "none":
		return "none"
	default:
		return "high"
	}
}

// MapEffortToDeepSeek 将通用 effort 映射到 DeepSeek 两档。
//
// DeepSeek 支持：high / max
func MapEffortToDeepSeek(effort string) string {
	switch strings.ToLower(effort) {
	case "xhigh", "max", "ultra_code", "high":
		return "max"
	default:
		return "high"
	}
}

// MapEffortToAnthropic 将通用 effort 映射到 Anthropic 四档。
//
// Anthropic 支持：low / medium / high / max
func MapEffortToAnthropic(effort string) string {
	switch strings.ToLower(effort) {
	case "minimal", "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	case "xhigh", "max":
		return "max"
	default:
		return "high"
	}
}
