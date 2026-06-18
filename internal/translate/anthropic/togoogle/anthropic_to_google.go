package togoogle

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/polarisagi/hermes/internal/domain"
	"github.com/polarisagi/hermes/internal/translate"
	anthr "github.com/polarisagi/hermes/internal/translate/anthropic"
)

const anthropicVersionGEAP = "vertex-2023-10-16"

func isClaudeModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(model), "claude-")
}



// Translator 实现了 translate.Translator 接口（Anthropic → Google Agent Platform）
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
	var req MessageRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return nil, "", fmt.Errorf("invalid json: %w", err)
	}

	finalModel := targetModel
	if finalModel == "" {
		finalModel = req.Model
	}
	useGEAPClaude := isClaudeModel(finalModel)

	isCompact := anthr.IsCompactRequest(&req)

	if isCompact {
		req.Tools = nil
	}

	if useGEAPClaude {
		geapBody, err := rewriteBodyForGEAPClaude(bodyBytes, false, "")
		if err != nil {
			return nil, "", err
		}
		var subpath string
		if req.Stream {
			subpath = fmt.Sprintf("models/%s:streamRawPredict", finalModel)
		} else {
			subpath = fmt.Sprintf("models/%s:rawPredict", finalModel)
		}
		return geapBody, "/" + subpath, nil
	}

	var vReq map[string]interface{}
	if isCompact {
		vReq = buildCompactVertexRequest(req, finalModel)
	} else {
		vReq, _ = mapToVertexRequest(req, finalModel)
	}

	vReqBytes, _ := json.Marshal(vReq)

	if finalModel == "" {
		finalModel = "gemini-3.1-pro-preview"
	}

	var subpath string
	if req.Stream {
		subpath = fmt.Sprintf("models/%s:streamGenerateContent?alt=sse", finalModel)
	} else {
		subpath = fmt.Sprintf("models/%s:generateContent", finalModel)
	}
	return vReqBytes, "/" + subpath, nil
}

func (t *Translator) TranslateResponse(w http.ResponseWriter, r *http.Request, resp *http.Response) error {
	stream := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") ||
		strings.Contains(resp.Request.URL.Path, "stream")

	isGEAPClaude := strings.Contains(resp.Request.URL.Path, "anthropic")
	// 从响应 URL 路径提取真实的 Gemini 模型名，而非硬编码占位符
	model := extractGeminiModelFromPath(resp.Request.URL.Path)
	if model == "" {
		model = "gemini"
	}
	// 生成唯一 traceID（时间戳 + 随机数），确保工具调用 ID 的全局唯一性
	traceID := generateTraceID()

	if isGEAPClaude {
		if stream {
			streamGEAPClaude(w, r, resp, model)
		} else {
			nonStreamGEAPClaude(w, resp)
		}
	} else {
		var req MessageRequest
		var isCompact bool
		var reqBody []byte
		if originalBody, ok := r.Context().Value(translate.OriginalReqBodyKey{}).([]byte); ok {
			reqBody = originalBody
			if err := json.Unmarshal(originalBody, &req); err != nil {
				slog.Warn("⚠️ [CompactDetect] 原始请求体解析失败，compact 检测降级为仅文本特征",
					"error", err.Error(),
					"body_preview", string(originalBody[:min(len(originalBody), 200)]))
			}
			isCompact = anthr.IsCompactRequest(&req)
			slog.Info("🔍 [CompactDetect] 请求类型判断",
				"is_compact", isCompact,
				"has_context_mgmt", req.ContextManagement != nil,
				"msg_count", len(req.Messages),
			)
		}

		if stream {
			// 使用 r.Context() 确保客户端断开时能取消上游流读取，避免资源浪费
			streamAnthropicResponse(r.Context(), w, resp, req, traceID, nil, "Anthropic-Adapter", model, reqBody, isCompact)
		} else {
			handleAnthropicNonStreamResponse(w, resp, req, traceID, nil, "Anthropic-Adapter", model, reqBody, isCompact)
		}
	}
	return nil
}


// extractGeminiModelFromPath 从 Gemini API 请求路径提取模型名。
// 路径格式：/models/{model}:generateContent 或 /models/{model}:streamGenerateContent
func extractGeminiModelFromPath(path string) string {
	const prefix = "/models/"
	idx := strings.Index(path, prefix)
	if idx == -1 {
		return ""
	}
	sub := path[idx+len(prefix):]
	if i := strings.IndexAny(sub, ":?"); i != -1 {
		sub = sub[:i]
	}
	return sub
}

// generateTraceID 生成基于时间戳与随机数的短 ID，用于工具调用 ID、消息 ID 的唯一性保障。
func generateTraceID() string {
	return fmt.Sprintf("%x%04x", time.Now().UnixNano()&0xFFFFFFFF, rand.Intn(0x10000))
}

// buildCompactVertexRequest 为 /compact 请求构建最小 Vertex AI 请求。
// 跳过 mapToVertexRequest 的昂贵工作（ContextManagement 编辑、消息映射等），
// 因为 compact 模式会将其全部覆盖。直接构建仅含 compact prompt 的请求体。
func buildCompactVertexRequest(req MessageRequest, model string) map[string]interface{} {
	promptInjection := anthr.BuildCompactPrompt(&req)

	vReq := map[string]interface{}{
		"contents": []map[string]interface{}{{
			"role":  "user",
			"parts": []map[string]interface{}{{"text": promptInjection}},
		}},
		"generationConfig": map[string]interface{}{
			"temperature": 0.0,
			"thinkingConfig": map[string]interface{}{
				"includeThoughts": false,
			},
		},
		"safetySettings": geapSafetySettings,
	}

	if maxTokens := req.MaxTokens; maxTokens > 0 {
		genCfg := vReq["generationConfig"].(map[string]interface{})
		genCfg["maxOutputTokens"] = maxTokens
	}

	if req.Metadata != nil && req.Metadata.UserID != "" {
		sanitized := sanitizeLabelValue(req.Metadata.UserID)
		if sanitized != "" {
			vReq["labels"] = map[string]string{"user-id": sanitized}
		}
	}

	return vReq
}

func rewriteBodyForGEAPClaude(bodyBytes []byte, isCountTokens bool, targetModel string) ([]byte, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &m); err != nil {
		return nil, err
	}
	m["anthropic_version"] = anthropicVersionGEAP
	if isCountTokens {
		if targetModel != "" {
			m["model"] = targetModel
		}
		delete(m, "stream")
		delete(m, "max_tokens")
		delete(m, "temperature")
	} else {
		delete(m, "model")
	}
	return json.Marshal(m)
}

func streamGEAPClaude(w http.ResponseWriter, r *http.Request, upstreamResp *http.Response, modelName string) {
	translate.CopyHeaders(w.Header(), upstreamResp.Header)
	w.WriteHeader(upstreamResp.StatusCode)
	translate.ForwardStreamBody(r.Context(), w, upstreamResp.Body)
}

func nonStreamGEAPClaude(w http.ResponseWriter, upstreamResp *http.Response) {
	bodyBytes, _ := io.ReadAll(upstreamResp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(upstreamResp.StatusCode)
	_, _ = w.Write(bodyBytes)
}


