// Package openai 实现 OpenAI 协议 → OpenAI 兼容后端的透传与协议适配。
//
// 支持三种后端场景：
//
//  1. OpenAI 官方后端（api.openai.com）
//     - 原路透传：只替换 model，其余字段原样保留
//     - Responses API 路径(/v1/responses)直接透传，server.go 已跳过预转换
//     - 支持最新格式：reasoning.effort, max_completion_tokens, stream_options
//
//  2. DeepSeek OpenAI 兼容 API（api.deepseek.com/v1）
//     - reasoning.effort → thinking:{type:"enabled"} + reasoning_effort（两档：high/max）
//     - max_completion_tokens → max_tokens
//     - stream_options 原样保留（DeepSeek 支持）
//
//  3. 通用 OpenAI 兼容厂商（其他便宜大模型）
//     - reasoning.effort → reasoning_effort（单独字段，无 thinking 包装）
//     - max_completion_tokens → max_tokens（多数通用厂商不支持 max_completion_tokens）
//     - 移除 stream_options（可能导致不支持的厂商报错）
package openai

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/polarisagi/hermes/internal/domain"
	"github.com/polarisagi/hermes/internal/translate"
	oai "github.com/polarisagi/hermes/internal/translate/openai"
)

// OpenAITranslator 实现 OpenAI → OpenAI 兼容后端的翻译器
type OpenAITranslator struct{}

func NewTranslator() *OpenAITranslator {
	return &OpenAITranslator{}
}

func (t *OpenAITranslator) TranslateRequest(
	r *http.Request,
	bodyBytes []byte,
	provider *domain.UserProvider,
	targetEndpoint *domain.SysAccessEndpoint,
	targetModel string,
) ([]byte, string, error) {
	kind := translate.DetectBackend(provider, targetEndpoint)

	path := "/chat/completions"
	// 官方 OpenAI 后端且客户端发的是 Responses API：透传到 /responses
	if kind == translate.BackendOpenAI && (strings.HasSuffix(r.URL.Path, "/responses") || strings.Contains(r.URL.Path, "/v1/responses")) {
		path = "/responses"
	}

	var payload []byte
	if kind == translate.BackendOpenAI {
		// 官方后端：只替换模型名，其余全部透传
		payload = replaceModelInJSON(bodyBytes, targetModel)
	} else {
		// 非官方后端：需要适配参数差异
		payload = adaptForCompatBackend(bodyBytes, targetModel, kind)
	}

	slog.Debug("→ [toopenai] 翻译请求",
		"backend", kind.String(),
		"path", path,
	)

	return payload, path, nil
}

func (t *OpenAITranslator) TranslateResponse(w http.ResponseWriter, r *http.Request, resp *http.Response) error {
	translate.CopyHeaders(w.Header(), resp.Header)

	stream := strings.Contains(resp.Header.Get("Content-Type"), "event-stream")
	if stream {
		w.Header().Set("X-Accel-Buffering", "no")
	}

	w.WriteHeader(resp.StatusCode)

	if stream {
		translate.ForwardStreamBody(w, resp.Body)
	} else {
		_, _ = io.Copy(w, resp.Body)
	}
	return nil
}

// ── 参数适配 ──────────────────────────────────────────────────────────────────

// adaptForCompatBackend 将 OpenAI 最新格式的参数适配为 DeepSeek/通用兼容后端能识别的格式。
//
// 适配规则：
//
//	reasoning.effort → DeepSeek: thinking:{type:"enabled"} + reasoning_effort
//	                   通用: reasoning_effort（直接字段）
//	max_completion_tokens → max_tokens（通用字段）
//	stream_options → DeepSeek 原样保留；通用后端移除（可能报错）
func adaptForCompatBackend(body []byte, targetModel string, kind translate.BackendKind) []byte {
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		return body
	}

	if targetModel != "" {
		req["model"] = targetModel
	}

	// 移除 user 字段，防止 Codex 的随机 session id 破坏第三方模型（如 DeepSeek）的 cache 命中率
	delete(req, "user")

	// compact 检测：非官方 OpenAI 后端不支持 truncation:"auto"，由网关实现等效上下文压缩
	if kind != translate.BackendOpenAI && oai.IsCompactRequestOpenAI(req) {
		msgs, _ := req["messages"].([]interface{})
		compactPrompt := oai.BuildCompactPromptFromOpenAI(msgs)
		req["messages"] = []map[string]interface{}{{"role": "user", "content": compactPrompt}}
		delete(req, "tools")
		delete(req, "tool_choice")
		delete(req, "__hermes_compact")
		req["temperature"] = 0.0
		result, _ := json.Marshal(req)
		return result
	}
	delete(req, "__hermes_compact")

	// max_completion_tokens → max_tokens（DeepSeek/通用不支持 max_completion_tokens）
	if mct, ok := req["max_completion_tokens"]; ok {
		if _, hasMaxTokens := req["max_tokens"]; !hasMaxTokens {
			req["max_tokens"] = mct
		}
		delete(req, "max_completion_tokens")
	}

	// reasoning.effort → 后端对应的推理参数
	var effortStr string
	if reasoning, ok := req["reasoning"].(map[string]interface{}); ok {
		if ef, ok := reasoning["effort"].(string); ok {
			effortStr = ef
		}
		delete(req, "reasoning")
	}

	if effortStr != "" && effortStr != "none" {
		switch kind {
		case translate.BackendDeepSeek:
			// DeepSeek: 需要 thinking.type="enabled" + reasoning_effort
			req["thinking"] = map[string]interface{}{"type": "enabled"}
			req["reasoning_effort"] = translate.MapEffortToDeepSeek(effortStr)
		default:
			// 通用：直接 reasoning_effort 字段（多数兼容厂商支持）
			req["reasoning_effort"] = effortStr
		}
	}

	// 通用后端移除 stream_options（部分不支持此字段）
	if kind == translate.BackendGeneric {
		delete(req, "stream_options")
	}

	// 部分兼容后端（如 DeepSeek）暂不支持 json_schema，降级为 json_object
	if rf, ok := req["response_format"].(map[string]interface{}); ok {
		if formatType, _ := rf["type"].(string); formatType == "json_schema" {
			if kind == translate.BackendDeepSeek || kind == translate.BackendGeneric {
				req["response_format"] = map[string]interface{}{"type": "json_object"}
			}
		}
	}

	result, err := json.Marshal(req)
	if err != nil {
		return body
	}
	return result
}

// replaceModelInJSON 替换 JSON 中的 model 字段并移除 user 字段
func replaceModelInJSON(body []byte, targetModel string) []byte {
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	if targetModel != "" {
		m["model"] = targetModel
	}
	
	// 移除 user 字段，防止每次请求唯一 ID 破坏 OpenAI 前缀缓存
	delete(m, "user")
	result, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return result
}
