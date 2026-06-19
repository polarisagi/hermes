package toopenai

import (
	"encoding/json"
	"strings"

	"github.com/polarisagi/hermes/internal/translate"
	anthr "github.com/polarisagi/hermes/internal/translate/anthropic"
)

func buildOpenAIRequest(aReq *anthr.MessageRequest, targetModel string, kind translate.BackendKind) map[string]interface{} {
	oReq := make(map[string]interface{})

	if targetModel != "" {
		oReq["model"] = targetModel
	} else {
		oReq["model"] = aReq.Model
	}

	if aReq.Stream {
		oReq["stream"] = true
		if kind == translate.BackendOpenAI {
			oReq["stream_options"] = map[string]interface{}{"include_usage": true}
		}
	}

	if aReq.Temperature != nil {
		oReq["temperature"] = *aReq.Temperature
	}
	if aReq.TopP != nil {
		oReq["top_p"] = *aReq.TopP
	}

	if aReq.MaxTokens > 0 {
		if kind == translate.BackendOpenAI {
			oReq["max_completion_tokens"] = aReq.MaxTokens
		} else {
			oReq["max_tokens"] = aReq.MaxTokens
		}
	}

	if len(aReq.StopSequences) > 0 {
		oReq["stop"] = aReq.StopSequences
	}

	if aReq.Metadata != nil && aReq.Metadata.UserID != "" {
		oReq["user"] = aReq.Metadata.UserID
	}

	mapThinkingParam(aReq, oReq, kind)

	if len(aReq.Tools) > 0 {
		oReq["tools"] = convertTools(aReq.Tools)
		if aReq.ToolChoice != nil {
			oReq["tool_choice"] = convertToolChoice(aReq.ToolChoice)
		}
	}

	if anthr.IsCompactRequest(aReq) {
		promptInjection := anthr.BuildCompactPrompt(aReq)
		oReq["messages"] = []map[string]interface{}{{
			"role":    "user",
			"content": promptInjection,
		}}
		delete(oReq, "tools")
		delete(oReq, "tool_choice")
		oReq["temperature"] = 0.0
	} else {
		oReq["messages"] = buildOpenAIMessages(aReq)
	}

	return oReq
}

// mapThinkingParam 将 Anthropic 思考参数映射到对应后端格式
//
// OpenAI 官方（2026年5月格式）：reasoning: {effort: "none"/"low"/"medium"/"high"/"xhigh"}
// DeepSeek OpenAI 兼容：thinking: {type: "enabled"} + reasoning_effort: "high"/"max"
// 通用兼容：reasoning_effort: "high"/"max"
func mapThinkingParam(aReq *anthr.MessageRequest, oReq map[string]interface{}, kind translate.BackendKind) {
	thinkingActive := aReq.Thinking != nil &&
		(aReq.Thinking.Type == "adaptive" || aReq.Thinking.Type == "enabled")
	thinkingDisabled := aReq.Thinking != nil && aReq.Thinking.Type == "disabled"
	// 2026年：客户端可能只传 effort 不传 thinking（模型默认开启思考），视为隐式开启
	hasEffortOnly := aReq.Effort != "" && aReq.Thinking == nil

	switch kind {
	case translate.BackendOpenAI:
		if thinkingDisabled {
			oReq["reasoning"] = map[string]interface{}{"effort": "none"}
			return
		}
		if !thinkingActive && !hasEffortOnly {
			return
		}
		oReq["reasoning"] = map[string]interface{}{
			"effort": translate.MapEffortToOpenAI(aReq.Effort),
		}

	case translate.BackendDeepSeek:
		if thinkingDisabled || (!thinkingActive && !hasEffortOnly) {
			return
		}
		oReq["thinking"] = map[string]interface{}{"type": "enabled"}
		oReq["reasoning_effort"] = translate.MapEffortToDeepSeek(aReq.Effort)

	default:
		if thinkingDisabled || (!thinkingActive && !hasEffortOnly) {
			return
		}
		oReq["reasoning_effort"] = translate.MapEffortToDeepSeek(aReq.Effort)
	}
}

func buildOpenAIMessages(aReq *anthr.MessageRequest) []map[string]interface{} {
	var msgs []map[string]interface{}

	if sysText := extractSystemText(aReq.System); sysText != "" {
		msgs = append(msgs, map[string]interface{}{
			"role":    "system",
			"content": sysText,
		})
	}

	for _, msg := range aReq.Messages {
		converted := convertAnthropicMessage(msg)
		msgs = append(msgs, converted...)
	}
	return msgs
}

func convertAnthropicMessage(msg anthr.Message) []map[string]interface{} {
	switch content := msg.Content.(type) {
	case string:
		if content == "" {
			return nil
		}
		return []map[string]interface{}{{"role": msg.Role, "content": content}}
	case []interface{}:
		return convertBlocksToOpenAIMessages(msg.Role, content)
	}
	return nil
}

// convertBlocksToOpenAIMessages 将 Anthropic content blocks 转换为 OpenAI 消息
//
// 关键规则：
//  1. assistant 消息中的 tool_use → OpenAI assistant message 的 tool_calls 字段
//  2. user 消息中的 tool_result → OpenAI role=tool 消息（每个独立一条）
//  3. redacted_thinking → 丢弃（所有后端都不支持）
func convertBlocksToOpenAIMessages(role string, blocks []interface{}) []map[string]interface{} {
	if role == "assistant" {
		return convertAssistantBlocks(blocks)
	}
	if role == "user" {
		return convertUserBlocks(blocks)
	}
	return nil
}

func convertAssistantBlocks(blocks []interface{}) []map[string]interface{} {
	var textParts []string
	var thinkingParts []string
	var toolCalls []map[string]interface{}

	for _, b := range blocks {
		blk, ok := b.(map[string]interface{})
		if !ok {
			continue
		}
		switch blk["type"] {
		case "text":
			if t, ok := blk["text"].(string); ok && t != "" {
				textParts = append(textParts, t)
			}
		case "thinking":
			if t, ok := blk["thinking"].(string); ok && t != "" {
				thinkingParts = append(thinkingParts, t)
			}
		case "redacted_thinking":
			// 丢弃
		case "tool_use":
			id, _ := blk["id"].(string)
			name, _ := blk["name"].(string)
			argsBytes, _ := json.Marshal(blk["input"])
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   id,
				"type": "function",
				"function": map[string]interface{}{
					"name":      name,
					"arguments": string(argsBytes),
				},
			})
		case "compaction":
			if ct, ok := blk["content"].(string); ok && ct != "" {
				textParts = append(textParts, ct)
			}
		}
	}

	text := strings.Join(textParts, "\n")
	hasTools := len(toolCalls) > 0
	hasThinking := len(thinkingParts) > 0

	if text == "" && !hasTools {
		return nil
	}

	oMsg := map[string]interface{}{
		"role":    "assistant",
		"content": text,
	}
	if hasTools {
		oMsg["tool_calls"] = toolCalls
	}
	if hasThinking {
		oMsg["reasoning_content"] = strings.Join(thinkingParts, "\n")
	}

	return []map[string]interface{}{oMsg}
}

func convertUserBlocks(blocks []interface{}) []map[string]interface{} {
	var textParts []string
	var toolMessages []map[string]interface{}

	for _, b := range blocks {
		blk, ok := b.(map[string]interface{})
		if !ok {
			continue
		}
		switch blk["type"] {
		case "text":
			if t, ok := blk["text"].(string); ok && t != "" {
				textParts = append(textParts, t)
			}
		case "tool_result":
			toolCallID, _ := blk["tool_use_id"].(string)
			var resultContent string
			switch ct := blk["content"].(type) {
			case string:
				resultContent = ct
			case []interface{}:
				var parts []string
				for _, item := range ct {
					if m, ok := item.(map[string]interface{}); ok {
						if t, ok := m["text"].(string); ok && t != "" {
							parts = append(parts, t)
						}
					}
				}
				resultContent = strings.Join(parts, "\n")
			}
			if toolCallID != "" {
				toolMessages = append(toolMessages, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": toolCallID,
					"content":      resultContent,
				})
			} else {
				textParts = append(textParts, resultContent)
			}
		case "compaction":
			if ct, ok := blk["content"].(string); ok && ct != "" {
				textParts = append(textParts, ct)
			}
		}
	}

	var result []map[string]interface{}
	if text := strings.Join(textParts, "\n"); text != "" {
		result = append(result, map[string]interface{}{
			"role":    "user",
			"content": text,
		})
	}
	result = append(result, toolMessages...)
	return result
}

func extractSystemText(system interface{}) string {
	if system == nil {
		return ""
	}
	switch s := system.(type) {
	case string:
		return strings.TrimSpace(s)
	case []interface{}:
		var sb strings.Builder
		for _, item := range s {
			if m, ok := item.(map[string]interface{}); ok && m["type"] == "text" {
				if text, ok := m["text"].(string); ok {
					if sb.Len() > 0 {
						sb.WriteString("\n")
					}
					sb.WriteString(text)
				}
			}
		}
		return strings.TrimSpace(sb.String())
	}
	return ""
}
