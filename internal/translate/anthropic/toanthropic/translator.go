// Package toanthropic 实现 Anthropic Messages API → Anthropic Messages API 的协议转换。
//
// 支持两种后端模式：
//
//  1. 真实 Anthropic API 后端（api.anthropic.com）
//     → 最小化改动透传：只替换 model 名，原始字节直接转发
//     → 透传客户端携带的所有 Anthropic 相关 header（anthropic-version、anthropic-beta 等）
//     → 响应原样返回给 Claude Code 客户端
//
//  2. DeepSeek Anthropic 兼容接口（api.deepseek.com/anthropic）
//     → 需要进行协议适配（DeepSeek 的 Anthropic 实现与官方存在差异）
//     → 详见 adaptForDeepSeek() 注释
//
// DeepSeek Anthropic API 与官方 Anthropic API 的主要差异（2026年5月官方文档）：
//   - thinking.type 只支持 "enabled"/"disabled"，不支持 "adaptive"（Claude 2026 新增）
//   - budget_tokens 被忽略（DeepSeek 用 output_config.effort 控制强度）
//   - 顶层 effort 字段不被识别，需转为 output_config: {effort: "high"/"max"}
//   - redacted_thinking 不支持，历史消息中出现时需过滤
//   - image、document 等多模态 content 不支持
//   - context_management 等 Claude Code 专属字段需清理（DeepSeek 不识别会报错）
//   - anthropic-beta header 被忽略（无害）
package toanthropic

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/polarisagi/hermes/internal/domain"
	"github.com/polarisagi/hermes/internal/translate"
	anthr "github.com/polarisagi/hermes/internal/translate/anthropic"
)

type Translator struct{}

func NewTranslator() *Translator {
	return &Translator{}
}

func (t *Translator) TranslateRequest(
	r *http.Request,
	bodyBytes []byte,
	provider *domain.UserProvider,
	targetEndpoint *domain.SysAccessEndpoint,
	targetModel string,
) ([]byte, string, error) {
	isDeepSeek := detectDeepSeekBackend(provider, targetEndpoint)

	var newBodyBytes []byte

	if isDeepSeek {
		var aReq anthr.MessageRequest
		if err := json.Unmarshal(bodyBytes, &aReq); err != nil {
			return nil, "", fmt.Errorf("invalid JSON payload: %w", err)
		}
		if targetModel != "" {
			aReq.Model = targetModel
		}
		adaptForDeepSeek(&aReq)
		newBodyBytes, _ = json.Marshal(aReq)
		slog.Debug("[toanthropic] DeepSeek 模式：协议适配完成",
			"model", aReq.Model,
			"thinking_type", thinkingType(aReq.Thinking),
			"output_config_effort", outputConfigEffort(aReq.OutputConfig),
		)
	} else {
		newBodyBytes = replaceModelInRawJSON(bodyBytes, targetModel)
	}

	return newBodyBytes, "/messages", nil
}

func (t *Translator) TranslateResponse(w http.ResponseWriter, r *http.Request, resp *http.Response) error {
	translate.CopyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		translate.ForwardStreamBody(w, resp.Body)
	} else {
		_, _ = io.Copy(w, resp.Body)
	}
	return nil
}

// ── 后端检测 ──────────────────────────────────────────────────────────────────

// detectDeepSeekBackend 检测后端节点是否为 DeepSeek Anthropic 兼容接口
func detectDeepSeekBackend(provider *domain.UserProvider, ep *domain.SysAccessEndpoint) bool {
	if ep != nil {
		if epURL := strings.ToLower(ep.DefaultBaseURL); strings.Contains(epURL, "deepseek") {
			return true
		}
	}
	if providerID := strings.ToLower(provider.ProviderID); strings.Contains(providerID, "deepseek") {
		return true
	}
	return false
}
