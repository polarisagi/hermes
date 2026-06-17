package togoogle

import (
	"context"
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

	vReq, _ := mapToVertexRequest(req, finalModel)
	if isCompact {
		promptInjection := anthr.BuildCompactPrompt(&req)

		vReq["contents"] = []map[string]interface{}{{
			"role":  "user",
			"parts": []map[string]interface{}{{"text": promptInjection}},
		}}
		delete(vReq, "systemInstruction")
		delete(vReq, "tools")
		delete(vReq, "toolConfig")
		
		if genCfg, ok := vReq["generationConfig"].(map[string]interface{}); ok {
			genCfg["temperature"] = 0.0
		} else {
			vReq["generationConfig"] = map[string]interface{}{
				"temperature": 0.0,
			}
		}
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
	model := "google_model"

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
			streamAnthropicResponse(context.Background(), w, resp, req, "", nil, "Anthropic-Adapter", model, reqBody, isCompact)
		} else {
			handleAnthropicNonStreamResponse(w, resp, req, "", nil, "Anthropic-Adapter", model, reqBody, isCompact)
		}
	}
	return nil
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


