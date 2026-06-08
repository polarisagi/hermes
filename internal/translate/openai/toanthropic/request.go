package toanthropic

import (
	"encoding/json"
	"strings"

	"github.com/polarisagi/hermes/internal/translate"
	anthr "github.com/polarisagi/hermes/internal/translate/anthropic"
	"github.com/polarisagi/hermes/internal/translate/openai"
)

func buildAnthropicRequest(oReq map[string]interface{}, targetModel string) (*anthr.MessageRequest, error) {
	aReq := &anthr.MessageRequest{}

	// Model
	if targetModel != "" {
		aReq.Model = targetModel
	} else if m, ok := oReq["model"].(string); ok {
		aReq.Model = m
	} else {
		aReq.Model = "claude-3-5-sonnet-latest" // 兜底
	}

	// Stream
	if s, ok := oReq["stream"].(bool); ok {
		aReq.Stream = s
	}

	// MaxTokens：兼容 max_tokens / max_completion_tokens（Responses API 经 server 预转换后统一为 max_tokens）
	if mt, ok := oReq["max_completion_tokens"].(float64); ok && mt > 0 {
		aReq.MaxTokens = int(mt)
	} else if mt, ok := oReq["max_tokens"].(float64); ok && mt > 0 {
		aReq.MaxTokens = int(mt)
	} else {
		aReq.MaxTokens = 8192
	}

	// Temperature & TopP
	if temp, ok := oReq["temperature"].(float64); ok {
		aReq.Temperature = &temp
	}
	if topP, ok := oReq["top_p"].(float64); ok {
		aReq.TopP = &topP
	}

	// 消息解析：body 在进入此翻译器前已由 server 统一转为 Chat Completions 格式（messages 数组）
	var msgs []map[string]interface{}
	if rawMsgs, ok := oReq["messages"].([]interface{}); ok {
		for _, rm := range rawMsgs {
			if m, ok := rm.(map[string]interface{}); ok {
				msgs = append(msgs, m)
			}
		}
	}
	aReq.System, aReq.Messages = convertMessages(msgs)

	// 工具调用转换
	if tools, ok := oReq["tools"].([]interface{}); ok {
		aReq.Tools = convertTools(tools)
	}
	if tc, ok := oReq["tool_choice"]; ok {
		aReq.ToolChoice = convertToolChoice(tc)
	}

	// 思考模式配置（2026年 Anthropic API 标准）
	// 优先读取 reasoning.effort（OpenAI Responses API），其次 reasoning_effort（Chat Completions）
	var effort string
	if reasoning, ok := oReq["reasoning"].(map[string]interface{}); ok {
		if ef, ok := reasoning["effort"].(string); ok {
			effort = ef
		}
	} else if ef, ok := oReq["reasoning_effort"].(string); ok {
		effort = ef
	}
	if effort != "" {
		if effort == "none" {
			// effort=none: 明确禁用思考模式（对旧模型不传 thinking 则默认启用）
			// Claude 新型 adaptive 模型不传 thinking 字段即不思考；传 disabled 对旧模型更安全
			aReq.Thinking = &anthr.ThinkingConfig{Type: "disabled"}
		} else {
			// 2026 年统一使用 adaptive 格式 + 顶层 effort（适用 Claude Opus 4.6+ / Sonnet 4.6+）
			// 旧的 {type:"enabled", budget_tokens:N} 格式已废弃
			aReq.Thinking = &anthr.ThinkingConfig{Type: "adaptive"}
			aReq.Effort = translate.MapEffortToAnthropic(effort)
		}
	}

	return aReq, nil
}

// convertMessages 将 OpenAI messages 转换为 Anthropic system 字符串和 messages 数组
func convertMessages(msgs []map[string]interface{}) (interface{}, []anthr.Message) {
	var sysLines []string
	var aMsgs []anthr.Message

	for _, msg := range msgs {
		role, _ := msg["role"].(string)

		switch role {
		case "system", "developer":
			// developer 等价于 system，Anthropic 不支持该角色
			sysLines = append(sysLines, openai.ExtractTextFromContent(msg["content"]))
		case "user":
			aMsgs = append(aMsgs, anthr.Message{
				Role:    "user",
				Content: convertUserContent(msg["content"]),
			})
		case "assistant":
			contentBlocks := convertAssistantContent(msg)
			if len(contentBlocks) > 0 {
				aMsgs = append(aMsgs, anthr.Message{
					Role:    "assistant",
					Content: contentBlocks,
				})
			}
		case "tool":
			// OpenAI: role:tool, tool_call_id:..., content:...
			// Anthropic: role:user, content:[{type:tool_result, tool_use_id:..., content:...}]
			toolCallID, _ := msg["tool_call_id"].(string)
			content := openai.ExtractTextFromContent(msg["content"])
			aMsgs = append(aMsgs, anthr.Message{
				Role: "user",
				Content: []interface{}{
					map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": toolCallID,
						"content":     content,
					},
				},
			})
		}
	}

	var system interface{}
	if len(sysLines) > 0 {
		system = strings.Join(sysLines, "\n")
	}

	return system, mergeConsecutiveRoles(aMsgs)
}

// convertUserContent 将 OpenAI 用户输入转换为 Anthropic content block
func convertUserContent(content interface{}) interface{} {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var blocks []interface{}
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			switch m["type"] {
			case "text":
				blocks = append(blocks, map[string]interface{}{
					"type": "text", "text": m["text"],
				})
			case "image_url":
				if urlObj, ok := m["image_url"].(map[string]interface{}); ok {
					url, _ := urlObj["url"].(string)
					// data:image/jpeg;base64,...
					if strings.HasPrefix(url, "data:") {
						parts := strings.SplitN(url, ",", 2)
						if len(parts) == 2 {
							meta := parts[0]
							mimeType := "image/jpeg"
							if idx := strings.Index(meta, ":"); idx != -1 {
								if idx2 := strings.Index(meta[idx+1:], ";"); idx2 != -1 {
									mimeType = meta[idx+1 : idx+1+idx2]
								}
							}
							blocks = append(blocks, map[string]interface{}{
								"type": "image",
								"source": map[string]interface{}{
									"type":       "base64",
									"media_type": mimeType,
									"data":       parts[1],
								},
							})
						}
					}
				}
			}
		}
		if len(blocks) == 0 {
			return ""
		}
		return blocks
	}
	return ""
}

// convertAssistantContent 将 assistant 消息转换为 Anthropic blocks（处理 tool_calls）
func convertAssistantContent(msg map[string]interface{}) []interface{} {
	var blocks []interface{}
	if content, ok := msg["content"].(string); ok && content != "" {
		blocks = append(blocks, map[string]interface{}{
			"type": "text", "text": content,
		})
	}

	if tcs, ok := msg["tool_calls"].([]interface{}); ok {
		for _, tc := range tcs {
			tcMap, ok := tc.(map[string]interface{})
			if !ok {
				continue
			}
			fn, _ := tcMap["function"].(map[string]interface{})
			if fn == nil {
				continue
			}
			name, _ := fn["name"].(string)
			argsStr, _ := fn["arguments"].(string)
			var input interface{}
			if err := json.Unmarshal([]byte(argsStr), &input); err != nil {
				input = map[string]interface{}{}
			}
			blocks = append(blocks, map[string]interface{}{
				"type":  "tool_use",
				"id":    tcMap["id"],
				"name":  name,
				"input": input,
			})
		}
	}
	return blocks
}

// mergeConsecutiveRoles 缝合连续的相同 Role（Anthropic 不允许连续的 user 消息）
func mergeConsecutiveRoles(msgs []anthr.Message) []anthr.Message {
	if len(msgs) <= 1 {
		return msgs
	}
	var merged []anthr.Message
	for _, m := range msgs {
		if len(merged) > 0 && merged[len(merged)-1].Role == m.Role {
			// 都是 string
			prevStr, ok1 := merged[len(merged)-1].Content.(string)
			curStr, ok2 := m.Content.(string)
			if ok1 && ok2 {
				merged[len(merged)-1].Content = prevStr + "\n" + curStr
				continue
			}
			// 转换为 []interface{} block 拼接
			var prevBlocks []interface{}
			if ok1 {
				prevBlocks = []interface{}{map[string]interface{}{"type": "text", "text": prevStr}}
			} else {
				prevBlocks = merged[len(merged)-1].Content.([]interface{})
			}

			var curBlocks []interface{}
			if ok2 {
				curBlocks = []interface{}{map[string]interface{}{"type": "text", "text": curStr}}
			} else {
				curBlocks = m.Content.([]interface{})
			}

			merged[len(merged)-1].Content = append(prevBlocks, curBlocks...)
		} else {
			merged = append(merged, m)
		}
	}
	return merged
}

func convertTools(oTools []interface{}) []anthr.Tool {
	var aTools []anthr.Tool
	for _, tRaw := range oTools {
		t, ok := tRaw.(map[string]interface{})
		if !ok || t["type"] != "function" {
			continue
		}
		fn, ok := t["function"].(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		aTool := anthr.Tool{
			Name:        name,
			Description: desc,
		}
		if params, ok := fn["parameters"].(map[string]interface{}); ok {
			aTool.InputSchema = params
		} else {
			aTool.InputSchema = map[string]interface{}{"type": "object"}
		}
		aTools = append(aTools, aTool)
	}
	return aTools
}

func convertToolChoice(tc interface{}) *anthr.ToolChoice {
	switch v := tc.(type) {
	case string:
		switch v {
		case "auto":
			return &anthr.ToolChoice{Type: "auto"}
		case "required":
			return &anthr.ToolChoice{Type: "any"}
		}
	case map[string]interface{}:
		if fn, ok := v["function"].(map[string]interface{}); ok {
			if name, ok := fn["name"].(string); ok {
				return &anthr.ToolChoice{Type: "tool", Name: name}
			}
		}
	}
	return nil
}
