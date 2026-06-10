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

	// Tools: 通过严格的 XML 格式化来模拟 Anthropic 底层的 Tool 消耗
	if len(req.Tools) > 0 {
		toolXml := RenderToolsToXML(req.Tools)
		total += billing.EstimateCompletionTokens(toolXml)
		
		// 对没有任何实际内容的 Tool 做低保 fallback
		for _, tool := range req.Tools {
			if tool.InputSchema == nil && tool.Type != "" && tool.Type != "custom" {
				total += builtinToolTokenCost(tool.Type)
			}
		}
	}

	return total
}

// RenderToolsToXML 将 JSON Schema 转换为类似 Anthropic 底层的 XML 结构，以实现精准的 Token 估算。
func RenderToolsToXML(tools []Tool) string {
	var sb strings.Builder
	sb.WriteString("<tools>\n")
	for _, tool := range tools {
		if tool.Type == "custom" || tool.Type == "" {
			sb.WriteString("<tool_description>\n")
			sb.WriteString("<tool_name>")
			sb.WriteString(tool.Name)
			sb.WriteString("</tool_name>\n")
			sb.WriteString("<description>\n")
			sb.WriteString(tool.Description)
			sb.WriteString("\n</description>\n")
			sb.WriteString("<parameters>\n")
			if tool.InputSchema != nil {
				JSONSchemaToXML(&sb, tool.InputSchema)
			}
			sb.WriteString("</parameters>\n")
			sb.WriteString("</tool_description>\n")
		}
	}
	sb.WriteString("</tools>\n")
	return sb.String()
}

func JSONSchemaToXML(sb *strings.Builder, schema interface{}) {
	s, ok := schema.(map[string]interface{})
	if !ok {
		return
	}
	properties, ok := s["properties"].(map[string]interface{})
	if !ok {
		return
	}
	for name, propIface := range properties {
		prop, ok := propIface.(map[string]interface{})
		if !ok {
			continue
		}
		sb.WriteString("<parameter>\n")
		// name
		sb.WriteString("<name>")
		sb.WriteString(name)
		sb.WriteString("</name>\n")
		// type
		if t, ok := prop["type"].(string); ok {
			sb.WriteString("<type>")
			sb.WriteString(t)
			sb.WriteString("</type>\n")
		}
		// description
		if d, ok := prop["description"].(string); ok {
			sb.WriteString("<description>")
			sb.WriteString(d)
			sb.WriteString("</description>\n")
		}
		// items
		if items, ok := prop["items"]; ok {
			sb.WriteString("<items>")
			b, _ := json.Marshal(items)
			sb.WriteString(string(b))
			sb.WriteString("</items>\n")
		}
		// enum
		if enums, ok := prop["enum"]; ok {
			sb.WriteString("<enum>")
			b, _ := json.Marshal(enums)
			sb.WriteString(string(b))
			sb.WriteString("</enum>\n")
		}
		sb.WriteString("</parameter>\n")
	}
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
