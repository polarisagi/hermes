// Package toanthropic 实现 Google Gemini API → Anthropic Messages API 的协议转换。
//
// 客户端（如 Gemini CLI）发送 Gemini generateContent 格式请求，
// 本翻译器将其转换为 Anthropic Messages 格式发往 Anthropic 后端，
// 并将 Anthropic 响应转换回 Gemini 格式返回给客户端。
//
// 请求转换要点：
//   - contents(role:user/model) → messages(role:user/assistant)
//   - systemInstruction        → system 字段
//   - generationConfig         → max_tokens/temperature/top_p/top_k/stop_sequences
//   - thinkingConfig           → thinking（adaptive/enabled）+ effort
//   - tools(functionDeclarations) → tools(input_schema)
//   - toolConfig               → tool_choice
//   - functionCall part        → tool_use 内容块
//   - functionResponse part    → tool_result 内容块
//   - inlineData / fileData part → image 内容块
//
// 响应转换要点：
//   - text block               → text part
//   - thinking block           → thought=true text part（含 thoughtSignature）
//   - tool_use block           → functionCall part
//   - stop_reason              → finishReason
//   - usage                   → usageMetadata
package toanthropic

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/polarisagi/hermes/internal/domain"
	gcommon "github.com/polarisagi/hermes/internal/translate/google"
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

	aReq := make(map[string]interface{})
	aReq["model"] = targetModel

	// stream 检测：alt=sse query param 或路径含 stream
	isStream := strings.Contains(r.URL.RawQuery, "alt=sse") ||
		strings.Contains(r.URL.Path, "stream")
	aReq["stream"] = isStream

	// systemInstruction → system
	if sys := gcommon.ExtractSystemInstruction(gReq); sys != "" {
		aReq["system"] = sys
	}

	// generationConfig 字段映射
	genCfg, _ := gReq["generationConfig"].(map[string]interface{})
	mapGenerationConfig(genCfg, aReq)

	// thinkingConfig → Anthropic thinking + effort
	mapThinkingConfig(genCfg, aReq)

	// tools: functionDeclarations → Anthropic tools
	if tools := convertToolsToAnthropic(gReq["tools"]); len(tools) > 0 {
		aReq["tools"] = tools
	}

	// toolConfig → tool_choice
	if tc := convertToolChoiceToAnthropic(gReq["toolConfig"]); tc != nil {
		aReq["tool_choice"] = tc
	}

	// contents → messages（需要先建立 functionCall ID 映射供 functionResponse 使用）
	msgs := convertContentsToMessages(gReq["contents"])
	aReq["messages"] = msgs

	// 默认 max_tokens
	if _, ok := aReq["max_tokens"]; !ok {
		aReq["max_tokens"] = 8192
	}

	payload, err := json.Marshal(aReq)
	if err != nil {
		return nil, "", fmt.Errorf("marshal Anthropic request: %w", err)
	}
	return payload, "/messages", nil
}

func (t *Translator) TranslateResponse(w http.ResponseWriter, r *http.Request, resp *http.Response) error {
	isStream := strings.Contains(r.URL.RawQuery, "alt=sse") ||
		strings.Contains(r.URL.Path, "stream") ||
		strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
	if isStream {
		handleStream(w, r, resp)
	} else {
		handleNonStream(w, resp)
	}
	return nil
}
