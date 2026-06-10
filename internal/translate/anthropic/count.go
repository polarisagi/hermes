package anthropic

import (
	"encoding/json"
	"strings"

	"github.com/polarisagi/hermes/internal/billing"
)

// EstimateTokens 本地估算 Anthropic Messages 请求的 input token 数，完整支持中文（CJK）。
//
// 与官方 count_tokens API 对齐的估算策略：
//   - 消息内容（可能含中文）使用 EstimateClaudeTokens，修正 o200k_base 对 CJK 的低估
//   - Tool Schema（纯 ASCII JSON/XML）使用 EstimateCompletionTokens，无需修正
//   - 每条消息固定 5 token 开销（role header + 分隔符），全局 10 token 对话容器开销
//   - 图片/文档按 1500 token 固定成本（Anthropic 官方视觉定价）
//   - thinking block 计入 signature 开销（thoughtSignature 可能极长）
func EstimateTokens(bodyBytes []byte) int64 {
	var req MessageRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return billing.EstimatePromptTokens(bodyBytes) // fallback
	}

	// 全局对话容器开销（<messages> 标签、BOS 等固定结构）
	var total int64 = 10

	// System prompt：支持字符串或内容块数组
	switch sys := req.System.(type) {
	case string:
		total += billing.EstimateClaudeTokens(sys)
	case []interface{}:
		for _, item := range sys {
			if m, ok := item.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok {
					total += billing.EstimateClaudeTokens(t)
				}
			}
		}
	}

	// Messages
	for _, msg := range req.Messages {
		total += 5 // role header + 内容分隔符开销（Anthropic 格式约 5 token/消息）
		switch v := msg.Content.(type) {
		case string:
			total += billing.EstimateClaudeTokens(v)
		case []interface{}:
			for _, item := range v {
				m, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				switch m["type"] {
				case "text":
					if t, ok := m["text"].(string); ok {
						total += billing.EstimateClaudeTokens(t)
					}
				case "compaction":
					if c, ok := m["content"].(string); ok {
						total += billing.EstimateClaudeTokens(c)
					}
				case "image", "document":
					// 官方 Anthropic 视觉定价：约 1500 token/图片
					total += 1500
				case "tool_use":
					if name, ok := m["name"].(string); ok {
						total += billing.EstimateClaudeTokens(name)
					}
					if input, ok := m["input"]; ok {
						b, _ := json.Marshal(input)
						// tool_use input 通常为 ASCII JSON，用通用估算即可
						total += billing.EstimateCompletionTokens(string(b))
					}
				case "tool_result":
					if c, ok := m["content"].(string); ok {
						total += billing.EstimateClaudeTokens(c)
					} else if arr, ok := m["content"].([]interface{}); ok {
						for _, ci := range arr {
							if cm, ok := ci.(map[string]interface{}); ok {
								if t, ok := cm["text"].(string); ok {
									total += billing.EstimateClaudeTokens(t)
								}
							}
						}
					}
				case "thinking":
					if t, ok := m["thinking"].(string); ok {
						total += billing.EstimateClaudeTokens(t)
					}
					// thoughtSignature 通常是几百到几千字节的 base64，按字节估算
					if sig, ok := m["signature"].(string); ok && sig != "" {
						total += int64(len(sig)) / 4
					}
				case "redacted_thinking":
					total += 50
				}
			}
		}
	}

	// Tools: 通过 XML 格式化模拟 Anthropic 底层的 Tool token 消耗
	if len(req.Tools) > 0 {
		toolXML := RenderToolsToXML(req.Tools)
		// Tool schema 为 ASCII，用通用估算
		total += billing.EstimateCompletionTokens(toolXML)

		// 没有实际 schema 的内置工具做低保 fallback
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
