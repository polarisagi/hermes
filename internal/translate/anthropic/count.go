package anthropic

import (
	"encoding/json"
	"strings"

	"github.com/polarisagi/hermes/internal/billing"
)

// EstimateTokens 本地估算 Anthropic Messages 请求的 input token 数
// 使用 tiktoken 提供高精度的精确内存估算，支持内置工具、图片、多模态块的成本预估。
func EstimateTokens(bodyBytes []byte) int64 {
	var req MessageRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return billing.EstimatePromptTokens(bodyBytes) // fallback
	}

	var total int64 = 0

	// System prompt：支持字符串或内容块数组
	switch sys := req.System.(type) {
	case string:
		total += billing.EstimateCompletionTokens(sys)
	case []interface{}:
		for _, item := range sys {
			if m, ok := item.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok {
					total += billing.EstimateCompletionTokens(t)
				}
			}
		}
	}

	// Messages
	for _, msg := range req.Messages {
		switch v := msg.Content.(type) {
		case string:
			total += billing.EstimateCompletionTokens(v)
		case []interface{}:
			for _, item := range v {
				m, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				switch m["type"] {
				case "text":
					if t, ok := m["text"].(string); ok {
						total += billing.EstimateCompletionTokens(t)
					}
				case "image", "document":
					total += 1500
				case "tool_use":
					if input, ok := m["input"]; ok {
						b, _ := json.Marshal(input)
						total += billing.EstimateCompletionTokens(string(b))
					}
					if name, ok := m["name"].(string); ok {
						total += billing.EstimateCompletionTokens(name)
					}
				case "tool_result":
					if c, ok := m["content"].(string); ok {
						total += billing.EstimateCompletionTokens(c)
					} else if arr, ok := m["content"].([]interface{}); ok {
						for _, ci := range arr {
							if cm, ok := ci.(map[string]interface{}); ok {
								if t, ok := cm["text"].(string); ok {
									total += billing.EstimateCompletionTokens(t)
								}
							}
						}
					}
				case "thinking":
					if t, ok := m["thinking"].(string); ok {
						total += billing.EstimateCompletionTokens(t)
					}
				case "redacted_thinking":
					total += 50
				case "compaction":
					if c, ok := m["content"].(string); ok {
						total += billing.EstimateCompletionTokens(c)
					}
				}
			}
		}
		total += 4 // role 结构开销
	}

	// Tools
	for _, tool := range req.Tools {
		if tool.Type != "" {
			total += builtinToolTokenCost(tool.Type)
			continue
		}
		total += billing.EstimateCompletionTokens(tool.Name)
		total += billing.EstimateCompletionTokens(tool.Description)
		if tool.InputSchema != nil {
			b, _ := json.Marshal(tool.InputSchema)
			total += billing.EstimateCompletionTokens(string(b))
		}
	}

	return total
}

func builtinToolTokenCost(toolType string) int64 {
	switch {
	case strings.Contains(toolType, "bash"):
		return 245
	case strings.Contains(toolType, "text_editor"), strings.Contains(toolType, "str_replace_based_edit_tool"):
		return 700
	case strings.Contains(toolType, "computer"):
		return 735
	case strings.Contains(toolType, "web_search"), strings.Contains(toolType, "web_fetch"):
		return 100
	case strings.Contains(toolType, "code_execution"):
		return 150
	case strings.Contains(toolType, "memory"):
		return 600
	default:
		return 100
	}
}
