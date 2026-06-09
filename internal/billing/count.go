package billing

import (
	"encoding/json"
	"strings"
)

// EstimateAnthropicTokens 更加精确地估算 Anthropic 请求的 token
func EstimateAnthropicTokens(bodyBytes []byte) int64 {
	var req map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return EstimatePromptTokens(bodyBytes) // fallback
	}

	var sb strings.Builder

	// 提取 system
	if sys, ok := req["system"]; ok {
		if s, ok := sys.(string); ok {
			sb.WriteString(s)
		} else if arr, ok := sys.([]interface{}); ok {
			for _, item := range arr {
				if m, ok := item.(map[string]interface{}); ok {
					if t, ok := m["text"].(string); ok {
						sb.WriteString(t)
					}
				}
			}
		}
	}

	// 提取 messages
	if msgs, ok := req["messages"].([]interface{}); ok {
		for _, msg := range msgs {
			if m, ok := msg.(map[string]interface{}); ok {
				if content, ok := m["content"]; ok {
					if s, ok := content.(string); ok {
						sb.WriteString(s)
					} else if arr, ok := content.([]interface{}); ok {
						for _, item := range arr {
							if im, ok := item.(map[string]interface{}); ok {
								if t, ok := im["text"].(string); ok {
									sb.WriteString(t)
								}
							}
						}
					}
				}
			}
		}
	}

	// 提取 tools
	if tools, ok := req["tools"].([]interface{}); ok {
		for _, tool := range tools {
			b, _ := json.Marshal(tool)
			sb.Write(b)
		}
	}

	text := sb.String()
	if len(text) == 0 {
		return 0
	}

	return EstimateCompletionTokens(text)
}
