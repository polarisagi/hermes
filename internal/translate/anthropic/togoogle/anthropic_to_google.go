package togoogle

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// buildGEAPURL 构建 Google Agent Platform 端点 URL
func buildGEAPURL(provider *domain.UserProvider, targetEndpoint *domain.SysAccessEndpoint, publisher, subpath, defaultLocation string) string {
	tmpl := ""
	if targetEndpoint != nil {
		tmpl = strings.TrimSuffix(targetEndpoint.DefaultBaseURL, "/")
	}
	if tmpl == "" {
		tmpl = "https://aiplatform.googleapis.com/v1/projects/{project_id}/locations/{location}/publishers/" + publisher + "/{subpath}"
	}

	var creds map[string]interface{}
	_ = json.Unmarshal(provider.AuthCredentials, &creds)
	projectID := ""
	if pid, ok := creds["project_id"].(string); ok {
		projectID = pid
	}
	location := defaultLocation

	url := strings.ReplaceAll(tmpl, "{project_id}", projectID)
	url = strings.ReplaceAll(url, "{location}", location)
	url = strings.ReplaceAll(url, "{subpath}", subpath)
	return url
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
		targetURL := buildGEAPURL(provider, targetEndpoint, "anthropic", subpath, "us-east5")
		return geapBody, targetURL, nil
	}

	vReq, _ := mapToVertexRequest(req, finalModel)
	if isCompact {
		var sb strings.Builder
		for _, msg := range req.Messages {
			sb.WriteString(fmt.Sprintf("<turn role=\"%s\">\n", msg.Role))
			switch c := msg.Content.(type) {
			case string:
				sb.WriteString(c)
			case []interface{}:
				for _, block := range c {
					if blk, ok := block.(map[string]interface{}); ok {
						switch blk["type"] {
						case "text", "compaction":
							if t, ok := blk["text"].(string); ok && t != "" {
								sb.WriteString(t)
							} else if ct, ok := blk["content"].(string); ok && ct != "" {
								sb.WriteString(ct)
							}
						case "tool_use":
							name, _ := blk["name"].(string)
							sb.WriteString(fmt.Sprintf("[tool_use: %s]", name))
						case "tool_result":
							if ct, ok := blk["content"].(string); ok && ct != "" {
								sb.WriteString(fmt.Sprintf("[tool_result: %s]", ct))
							}
						}
					}
				}
			}
			sb.WriteString("\n</turn>\n")
		}
		historyXML := sb.String()
		systemPrompt := flattenAnthropicSystem(req.System)
		promptInjection := fmt.Sprintf("System Context: %s\n\n<conversation_history>\n%s\n</conversation_history>\n\nSystem Task: You are performing a context compaction.", systemPrompt, historyXML)

		vReq["contents"] = []map[string]interface{}{{
			"role":  "user",
			"parts": []map[string]interface{}{{"text": promptInjection}},
		}}
		delete(vReq, "systemInstruction")
		delete(vReq, "tools")
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
	targetURL := buildGEAPURL(provider, targetEndpoint, "google", subpath, "global")
	return vReqBytes, targetURL, nil
}

func (t *Translator) TranslateResponse(w http.ResponseWriter, r *http.Request, resp *http.Response) error {
	stream := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") ||
		strings.Contains(r.URL.Path, "stream")

	isGEAPClaude := strings.Contains(r.URL.Path, "anthropic")
	model := "google_model"

	if isGEAPClaude {
		if stream {
			streamGEAPClaude(w, resp, model)
		} else {
			nonStreamGEAPClaude(w, resp)
		}
	} else {
		var req MessageRequest
		var isCompact bool
		var reqBody []byte
		if originalBody, ok := r.Context().Value(translate.OriginalReqBodyKey{}).([]byte); ok {
			reqBody = originalBody
			_ = json.Unmarshal(originalBody, &req)
			isCompact = anthr.IsCompactRequest(&req)
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

func streamGEAPClaude(w http.ResponseWriter, upstreamResp *http.Response, modelName string) {
	translate.CopyHeaders(w.Header(), upstreamResp.Header)
	w.WriteHeader(upstreamResp.StatusCode)
	translate.ForwardStreamBody(w, upstreamResp.Body)
}

func nonStreamGEAPClaude(w http.ResponseWriter, upstreamResp *http.Response) {
	bodyBytes, _ := io.ReadAll(upstreamResp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(upstreamResp.StatusCode)
	_, _ = w.Write(bodyBytes)
}

func flattenAnthropicSystem(sys interface{}) string {
	if sys == nil {
		return ""
	}
	switch v := sys.(type) {
	case string:
		return v
	case []interface{}:
		var sb strings.Builder
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok {
					sb.WriteString(t)
					sb.WriteString("\n")
				}
			}
		}
		return strings.TrimSpace(sb.String())
	}
	return ""
}
