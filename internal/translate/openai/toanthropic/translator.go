// Package toanthropic 实现 OpenAI (Chat Completions & Responses API) → Anthropic Messages API 的协议转换。
//
// 主要用于让 OpenAI 生态的客户端（如 Codex, Cursor）无缝对接 Claude 3.5/4.x 等 Anthropic 后端。
// 核心支持：
//   - 双输入协议兼容（Chat Completions / Responses API）
//   - 完整工具调用互转（OpenAI tool_calls ↔ Anthropic tool_use）
//   - 多模态图片支持（OpenAI image_url ↔ Anthropic image block）
//   - 2026年思考模式支持（reasoning_effort ↔ thinking.budget_tokens）
package toanthropic

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/polarisagi/hermes/internal/domain"
)

// OpenAIToAnthropicTranslator 实现从 OpenAI 到 Anthropic 的协议翻译
type OpenAIToAnthropicTranslator struct{}

func NewTranslator() *OpenAIToAnthropicTranslator {
	return &OpenAIToAnthropicTranslator{}
}

func (t *OpenAIToAnthropicTranslator) TranslateRequest(
	r *http.Request,
	bodyBytes []byte,
	provider *domain.UserProvider,
	targetEndpoint *domain.SysAccessEndpoint,
	targetModel string,
) ([]byte, string, error) {
	// 1. 尝试解析请求（兼容 Responses API 与 Chat Completions）
	var oReq map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &oReq); err != nil {
		return nil, "", fmt.Errorf("invalid OpenAI JSON payload: %w", err)
	}

	// 2. 转换为 Anthropic 格式
	// body 已由 proxy.Server 统一预转换为 Chat Completions 格式（Responses API 已在上游处理）
	aReq, err := buildAnthropicRequest(oReq, targetModel)
	if err != nil {
		return nil, "", fmt.Errorf("failed to convert to Anthropic format: %w", err)
	}
	aReqBytes, _ := json.Marshal(aReq)

	slog.Debug("→ [openai/toanthropic] 翻译请求",
		"model", aReq.Model,
		"stream", aReq.Stream,
	)

	return aReqBytes, "/messages", nil
}

func (t *OpenAIToAnthropicTranslator) TranslateResponse(w http.ResponseWriter, r *http.Request, resp *http.Response) error {
	// 始终输出 Chat Completions 格式；Responses API 的包装由 proxy.Server 统一处理
	// 实际模型名在 handleStream/handleNonStream 内从 Anthropic 响应中提取
	if strings.Contains(resp.Header.Get("Content-Type"), "event-stream") {
		handleStream(w, resp, "")
	} else {
		handleNonStream(w, resp, "")
	}
	return nil
}
