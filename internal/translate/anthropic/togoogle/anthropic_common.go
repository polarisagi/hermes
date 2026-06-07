// togoogle 专属辅助函数（非通用 SSE 工具）
// SSE 写入辅助函数已迁移至父包 internal/translator/anthropic/sse.go，
// 此处仅保留 togoogle 特有的业务辅助函数。
package togoogle

import (
	"net/http"
	"strings"

	anthr "github.com/polarisagi/hermes/internal/translate/anthropic"
)

// writeSSEMessageStart 是父包 anthr.WriteSSEMessageStart 的包级私有代理。
// estimatedInputTokens > 0 时填入 Usage.InputTokens，让 Claude Code 的 /context 命令
// 在首个事件就显示上下文占比；后续 message_delta 以精确值覆盖。
func writeSSEMessageStart(w http.ResponseWriter, flusher http.Flusher, traceID, modelName string, estimatedInputTokens int) {
	anthr.WriteSSEMessageStart(w, flusher, traceID, modelName, estimatedInputTokens)
}

// writeSSEContentBlockStop 是父包 anthr.WriteSSEContentBlockStop 的包级私有代理。
func writeSSEContentBlockStop(w http.ResponseWriter, flusher http.Flusher, index int) {
	anthr.WriteSSEContentBlockStop(w, flusher, index)
}

// writeSSEMessageStop 是父包 anthr.WriteSSEMessageStop 的包级私有代理。
func writeSSEMessageStop(w http.ResponseWriter, flusher http.Flusher) {
	anthr.WriteSSEMessageStop(w, flusher)
}

// ExtractAndStripBillingHeader 检查并移除 System prompt 中的 x-anthropic-billing-header。
// 返回提取到的 header 字符串，以便在返回给客户端时将其带上。
// 支持 string 和 []interface{} 两种 System 字段格式。
func ExtractAndStripBillingHeader(req *MessageRequest) string {
	if req.System == nil {
		return ""
	}

	var extracted string
	prefix := "x-anthropic-billing-header:"

	switch sys := req.System.(type) {
	case string:
		if strings.HasPrefix(strings.TrimSpace(sys), prefix) {
			parts := strings.SplitN(sys, "\n", 2)
			extracted = strings.TrimSpace(parts[0])
			if len(parts) > 1 {
				req.System = strings.TrimSpace(parts[1])
			} else {
				req.System = nil
			}
		}
	case []interface{}:
		var newSys []interface{}
		for _, item := range sys {
			if m, ok := item.(map[string]interface{}); ok {
				if m["type"] == "text" {
					if text, ok := m["text"].(string); ok {
						if strings.HasPrefix(strings.TrimSpace(text), prefix) {
							extracted = strings.TrimSpace(text)
							continue // 跳过此 block
						}
					}
				}
			}
			newSys = append(newSys, item)
		}
		if len(newSys) == 0 {
			req.System = nil
		} else {
			req.System = newSys
		}
	}
	return extracted
}
