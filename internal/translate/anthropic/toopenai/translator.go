// Package toopenai 实现 Anthropic Messages API → OpenAI Chat Completions API 的协议转换。
//
// 支持三类后端：
//
//  1. OpenAI 官方 API（api.openai.com）
//     - 思考参数：reasoning: {effort: "none"/"low"/"medium"/"high"/"xhigh"}
//     - 不暴露思考内容到响应字段（内部隐含）
//     - 使用 max_completion_tokens（max_tokens 已弃用）
//     - 流式需要 stream_options: {include_usage: true}
//     - 工具调用：完整的 tool_calls 格式
//
//  2. DeepSeek OpenAI 兼容 API（api.deepseek.com/v1）
//     - 思考参数：thinking: {type:"enabled"} + reasoning_effort: "high"/"max"
//     - 暴露思考内容：delta.reasoning_content / message.reasoning_content
//     - 使用 max_tokens
//     - 工具调用：同 OpenAI
//
//  3. 通用 OpenAI 兼容厂商
//     - 尽量兼容 DeepSeek 格式（多数厂商也支持 reasoning_effort）
//     - 使用 max_tokens
//
// 2026年5月 API 标准（不支持 o1 等旧推理模型）：
//   - 旧的 o1/o3 独立推理模型参数已废弃，统一用 reasoning_effort 控制
//   - GPT-5 系列内置思考模式，通过 reasoning.effort 开启（OpenAI 官方格式）
package toopenai

import (
	"encoding/json"
	"fmt"
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
	kind := translate.DetectBackend(provider, targetEndpoint)

	var aReq anthr.MessageRequest
	if err := json.Unmarshal(bodyBytes, &aReq); err != nil {
		return nil, "", fmt.Errorf("invalid Anthropic JSON payload: %w", err)
	}

	oReq := buildOpenAIRequest(&aReq, targetModel, kind)
	oReqBytes, _ := json.Marshal(oReq)

	slog.Debug("[toopenai] 翻译请求",
		"backend", kind.String(),
		"model", oReq["model"],
		"stream", aReq.Stream,
	)

	return oReqBytes, "/chat/completions", nil
}

func (t *Translator) TranslateResponse(w http.ResponseWriter, r *http.Request, resp *http.Response) error {
	// 用上游实际请求 URL（resp.Request）判断后端类型，而非客户端请求路径
	kind := translate.BackendGeneric
	if resp.Request != nil {
		upstreamHost := strings.ToLower(resp.Request.URL.Host)
		upstreamPath := strings.ToLower(resp.Request.URL.Path)
		switch {
		case strings.Contains(upstreamHost, "api.openai.com"):
			kind = translate.BackendOpenAI
		case strings.Contains(upstreamHost, "deepseek") || strings.Contains(upstreamPath, "deepseek"):
			kind = translate.BackendDeepSeek
		}
	}

	stream := strings.Contains(resp.Header.Get("Content-Type"), "event-stream")

	if stream {
		handleStream(w, resp, kind)
	} else {
		handleNonStream(w, resp, kind)
	}
	return nil
}
