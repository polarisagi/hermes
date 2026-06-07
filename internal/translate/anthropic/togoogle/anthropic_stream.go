// Anthropic SSE 写入代理 + Gemini 专属工具函数
// SSE 核心实现位于父包 internal/translator/anthropic/sse.go，
// 此处仅保留包级私有代理函数（供 togoogle 内部调用，无需修改各处 call site），
// 以及 Gemini 专属的 normalizeFunctionCallArgs 工具。
package togoogle

import (
	"bytes"
	"encoding/json"
	"net/http"

	anthr "github.com/polarisagi/hermes/internal/translate/anthropic"
)

// writeSSE 是父包 anthr.WriteSSE 的包级私有代理。
// togoogle 内部所有调用点无需修改。
func writeSSE(w http.ResponseWriter, flusher http.Flusher, eventType string, data interface{}) {
	anthr.WriteSSE(w, flusher, eventType, data)
}

// ptrInt 是父包 anthr.PtrInt 的包级私有代理。
func ptrInt(i int) *int { return anthr.PtrInt(i) }

// normalizeFunctionCallArgs 统一将 Gemini functionCall.args 转换为规范的 JSON 字节。
// Gemini 可能将 args 返回为 map[string]interface{}、JSON 字符串或其他类型，
// 此函数负责将所有可能的形式归一化为紧凑的 JSON 字节数组。
// 注意：此函数是 Gemini 特有的，不属于通用 Anthropic 协议层。
func normalizeFunctionCallArgs(args interface{}) []byte {
	if args == nil {
		return []byte("{}")
	}

	switch v := args.(type) {
	case map[string]interface{}:
		buffer := &bytes.Buffer{}
		encoder := json.NewEncoder(buffer)
		encoder.SetEscapeHTML(false)
		_ = encoder.Encode(v)
		result := buffer.Bytes()
		if len(result) > 0 && result[len(result)-1] == '\n' {
			result = result[:len(result)-1]
		}
		if len(result) == 0 || string(result) == "null" {
			return []byte("{}")
		}
		return result
	case string:
		if v == "" || v == "null" {
			return []byte("{}")
		}
		// args 可能是 JSON 字符串，尝试解析后再序列化以规范化格式
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(v), &parsed); err == nil {
			buffer := &bytes.Buffer{}
			encoder := json.NewEncoder(buffer)
			encoder.SetEscapeHTML(false)
			_ = encoder.Encode(parsed)
			result := buffer.Bytes()
			if len(result) > 0 && result[len(result)-1] == '\n' {
				result = result[:len(result)-1]
			}
			return result
		}
		// 不是合法 JSON，直接当纯文本返回
		return []byte(v)
	default:
		raw, _ := json.Marshal(v)
		if len(raw) == 0 || string(raw) == "null" {
			return []byte("{}")
		}
		return raw
	}
}
