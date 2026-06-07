// Package toopenai 实现 Google Gemini API → OpenAI Chat Completions API 的协议转换。
//
// 客户端（如 Gemini CLI）发送 Gemini generateContent 格式请求，
// 本翻译器将其转换为 OpenAI Chat Completions 格式发往 OpenAI/DeepSeek 等兼容后端，
// 并将 OpenAI 响应转换回 Gemini 格式返回给客户端。
//
// 支持三类后端：
//  1. OpenAI 官方 API（api.openai.com）
//     - 推理参数：reasoning: {effort: "none"/"low"/"medium"/"high"/"xhigh"}
//     - max_completion_tokens（max_tokens 已弃用）
//     - stream_options: {include_usage: true}
//  2. DeepSeek OpenAI 兼容 API（api.deepseek.com/v1）
//     - 推理参数：reasoning_effort: "high"/"max"（两档）
//     - max_tokens
//  3. 通用 OpenAI 兼容厂商
//     - reasoning_effort: "high"/"max"（多数厂商支持 DeepSeek 格式）
//     - max_tokens
//
// 思考参数映射（Google thinkingConfig → OpenAI reasoning）：
//   - thinkingLevel: NONE     → reasoning_effort: none / reasoning.effort: none
//   - thinkingLevel: LOW/MINIMAL → reasoning_effort: low
//   - thinkingLevel: MEDIUM   → reasoning_effort: medium
//   - thinkingLevel: HIGH     → reasoning_effort: high
//   - thinkingBudget > 32000  → reasoning_effort: xhigh（OpenAI）/ max（DeepSeek）
//   - thinkingBudget 8001..32000 → high
//   - thinkingBudget 1..8000  → low/medium
package toopenai

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/polarisagi/hermes/internal/domain"
	"github.com/polarisagi/hermes/internal/translate"
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
	var gReq map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &gReq); err != nil {
		return nil, "", fmt.Errorf("invalid Gemini JSON payload: %w", err)
	}

	kind := translate.DetectBackend(provider, targetEndpoint)
	isStream := strings.Contains(r.URL.RawQuery, "alt=sse") ||
		strings.Contains(r.URL.Path, "stream")

	oReq := buildOpenAIRequest(gReq, targetModel, isStream, kind)
	payload, err := json.Marshal(oReq)
	if err != nil {
		return nil, "", fmt.Errorf("marshal OpenAI request: %w", err)
	}

	slog.Debug("[google→openai] 翻译请求",
		"backend", kind.String(),
		"model", targetModel,
		"stream", isStream,
	)
	return payload, "/chat/completions", nil
}

func (t *Translator) TranslateResponse(w http.ResponseWriter, r *http.Request, resp *http.Response) error {
	kind := translate.BackendGeneric
	if resp.Request != nil {
		upstreamHost := strings.ToLower(resp.Request.URL.Host)
		switch {
		case strings.Contains(upstreamHost, "api.openai.com"):
			kind = translate.BackendOpenAI
		case strings.Contains(upstreamHost, "deepseek"):
			kind = translate.BackendDeepSeek
		}
	}

	isStream := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
	if isStream {
		handleStream(w, resp, kind)
	} else {
		handleNonStream(w, resp, kind)
	}
	return nil
}
