package toopenai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/polarisagi/hermes/internal/translate"
	gcommon "github.com/polarisagi/hermes/internal/translate/google"
)

// ── 请求转换 ─────────────────────────────────────────────────────────────────

func buildOpenAIRequest(gReq map[string]interface{}, targetModel string, isStream bool, kind translate.BackendKind) map[string]interface{} {
	oReq := make(map[string]interface{})
	oReq["model"] = targetModel

	if isStream {
		oReq["stream"] = true
		if kind == translate.BackendOpenAI {
			oReq["stream_options"] = map[string]interface{}{"include_usage": true}
		}
	}

	// systemInstruction → system message
	msgs := convertContentsToMessages(gReq, kind)
	oReq["messages"] = msgs

	// generationConfig 字段映射
	genCfg, _ := gReq["generationConfig"].(map[string]interface{})
	mapGenerationConfig(genCfg, oReq, kind)

	// thinkingConfig → reasoning 参数
	mapThinkingConfig(genCfg, oReq, kind)

	// tools: functionDeclarations → OpenAI function tools
	if tools := convertToolsToOpenAI(gReq["tools"]); len(tools) > 0 {
		oReq["tools"] = tools
	}

	// toolConfig → tool_choice
	if tc := convertToolChoiceToOpenAI(gReq["toolConfig"]); tc != nil {
		oReq["tool_choice"] = tc
	}

	return oReq
}

func convertContentsToMessages(gReq map[string]interface{}, kind translate.BackendKind) []map[string]interface{} {
	var msgs []map[string]interface{}

	// system message
	if sys := gcommon.ExtractSystemInstruction(gReq); sys != "" {
		msgs = append(msgs, map[string]interface{}{
			"role":    "system",
			"content": sys,
		})
	}

	contents, ok := gReq["contents"].([]interface{})
	if !ok {
		return msgs
	}

	// 先建立 functionCall name → callID 映射，供 functionResponse 使用
	callCounter := map[string]int{}
	// 记录每次 functionCall 的 ID（用于 functionResponse 匹配）
	callIDByName := map[string][]string{}
	// callIDCursor：functionCall 消费游标（赋值 ID 时递增）
	// respIDCursor：functionResponse 消费游标（查找 ID 时递增）
	// 两者必须独立，否则 functionCall 递增后 functionResponse 读到错误下标
	callIDCursor := map[string]int{}
	respIDCursor := map[string]int{}

	for _, c := range contents {
		content, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if parts, ok := content["parts"].([]interface{}); ok {
			for _, p := range parts {
				part, ok := p.(map[string]interface{})
				if !ok {
					continue
				}
				if fc, ok := part["functionCall"].(map[string]interface{}); ok {
					name, _ := fc["name"].(string)
					callCounter[name]++
					id := fmt.Sprintf("call_%s_%d", name, callCounter[name])
					callIDByName[name] = append(callIDByName[name], id)
				}
			}
		}
	}

	for _, c := range contents {
		content, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		role := "user"
		if r, ok := content["role"].(string); ok && r == "model" {
			role = "assistant"
		}
		parts, _ := content["parts"].([]interface{})

		converted := convertPartsToMessages(parts, role, callIDByName, callIDCursor, respIDCursor, kind)
		msgs = append(msgs, converted...)
	}
	return msgs
}

func convertPartsToMessages(parts []interface{}, role string, callIDByName map[string][]string, callIDCursor map[string]int, respIDCursor map[string]int, kind translate.BackendKind) []map[string]interface{} {
	var textParts []string
	var reasoningParts []string
	var toolCalls []map[string]interface{}
	var toolResults []map[string]interface{}

	for _, p := range parts {
		part, ok := p.(map[string]interface{})
		if !ok {
			continue
		}

		// thought=true → reasoning content（DeepSeek/通用格式）
		if isThought, _ := part["thought"].(bool); isThought {
			if text, ok := part["text"].(string); ok && text != "" {
				reasoningParts = append(reasoningParts, text)
			}
			continue
		}

		// text part
		if text, ok := part["text"].(string); ok {
			textParts = append(textParts, text)
			continue
		}

		// functionCall → tool_calls
		if fc, ok := part["functionCall"].(map[string]interface{}); ok {
			name, _ := fc["name"].(string)
			idx := callIDCursor[name]
			callIDCursor[name]++
			id := ""
			if ids, ok := callIDByName[name]; ok && idx < len(ids) {
				id = ids[idx]
			} else {
				id = fmt.Sprintf("call_%s_%d", name, idx+1)
			}
			argsBytes, _ := json.Marshal(fc["args"])
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   id,
				"type": "function",
				"function": map[string]interface{}{
					"name":      name,
					"arguments": string(argsBytes),
				},
			})
			continue
		}

		// functionResponse → tool message
		if fr, ok := part["functionResponse"].(map[string]interface{}); ok {
			name, _ := fr["name"].(string)
			// 使用独立的 respIDCursor，不受 callIDCursor 影响
			idx := respIDCursor[name]
			respIDCursor[name]++
			id := ""
			if ids, ok := callIDByName[name]; ok && idx < len(ids) {
				id = ids[idx]
			} else {
				id = fmt.Sprintf("call_%s_%d", name, idx+1)
			}
			respContent := ""
			if resp, ok := fr["response"].(map[string]interface{}); ok {
				if ct, ok := resp["content"].(string); ok {
					respContent = ct
				} else {
					b, _ := json.Marshal(resp)
					respContent = string(b)
				}
			}
			toolResults = append(toolResults, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": id,
				"content":      respContent,
			})
			continue
		}

		// inlineData → 多模态 content（仅 OpenAI 官方支持）
		if inlineData, ok := part["inlineData"].(map[string]interface{}); ok && kind == translate.BackendOpenAI {
			mimeType, _ := inlineData["mimeType"].(string)
			data, _ := inlineData["data"].(string)
			if data != "" && strings.HasPrefix(mimeType, "image/") {
				textParts = append(textParts, fmt.Sprintf("[image: base64 %s data omitted]", mimeType))
			}
		}
	}

	var result []map[string]interface{}

	// 拼装 assistant 消息（文本 + 推理 + 工具调用）
	if role == "assistant" {
		msg := map[string]interface{}{"role": "assistant"}
		text := strings.Join(textParts, "")
		if text != "" || len(toolCalls) == 0 {
			msg["content"] = text
		} else {
			msg["content"] = nil
		}
		if len(reasoningParts) > 0 && kind != translate.BackendOpenAI {
			msg["reasoning_content"] = strings.Join(reasoningParts, "\n")
		}
		if len(toolCalls) > 0 {
			msg["tool_calls"] = toolCalls
		}
		result = append(result, msg)
	} else {
		// user role：普通文本消息
		text := strings.Join(textParts, "")
		if text != "" {
			result = append(result, map[string]interface{}{
				"role":    "user",
				"content": text,
			})
		}
		// tool results 独立消息
		result = append(result, toolResults...)
	}

	return result
}

func mapGenerationConfig(genCfg map[string]interface{}, oReq map[string]interface{}, kind translate.BackendKind) {
	if genCfg == nil {
		return
	}
	if v, ok := genCfg["maxOutputTokens"].(float64); ok && v > 0 {
		if kind == translate.BackendOpenAI {
			oReq["max_completion_tokens"] = int(v)
		} else {
			oReq["max_tokens"] = int(v)
		}
	}
	if v, ok := genCfg["temperature"].(float64); ok {
		oReq["temperature"] = v
	}
	if v, ok := genCfg["topP"].(float64); ok {
		oReq["top_p"] = v
	}
	if v, ok := genCfg["stopSequences"].([]interface{}); ok && len(v) > 0 {
		var seqs []string
		for _, s := range v {
			if str, ok := s.(string); ok {
				seqs = append(seqs, str)
			}
		}
		if len(seqs) > 0 {
			oReq["stop"] = seqs
		}
	}
	// response_format（Gemini responseMimeType → OpenAI response_format）
	if mime, ok := genCfg["responseMimeType"].(string); ok && mime == "application/json" {
		if schema, ok := genCfg["responseSchema"]; ok {
			oReq["response_format"] = map[string]interface{}{
				"type":        "json_schema",
				"json_schema": map[string]interface{}{"name": "response", "schema": schema, "strict": true},
			}
		} else {
			oReq["response_format"] = map[string]interface{}{"type": "json_object"}
		}
	}
}

// mapThinkingConfig 将 Gemini thinkingConfig 映射到 OpenAI reasoning 参数
func mapThinkingConfig(genCfg map[string]interface{}, oReq map[string]interface{}, kind translate.BackendKind) {
	if genCfg == nil {
		return
	}
	tc, ok := genCfg["thinkingConfig"].(map[string]interface{})
	if !ok {
		return
	}

	// thinkingLevel（Gemini 3.x 新格式）
	if level, ok := tc["thinkingLevel"].(string); ok {
		effort := gcommon.ThinkingLevelToEffort(level)
		applyReasoningEffort(oReq, effort, kind)
		return
	}

	// thinkingBudget（Gemini 2.5 格式）
	if budget, ok := tc["thinkingBudget"].(float64); ok {
		effort := gcommon.ThinkingBudgetToEffort(int(budget))
		applyReasoningEffort(oReq, effort, kind)
	}
}

// applyReasoningEffort 根据后端类型填入对应的推理参数字段
func applyReasoningEffort(oReq map[string]interface{}, effort string, kind translate.BackendKind) {
	if effort == "none" {
		switch kind {
		case translate.BackendOpenAI:
			oReq["reasoning"] = map[string]interface{}{"effort": "none"}
		default:
			// 通用后端无 reasoning 字段，不设置
		}
		return
	}

	switch kind {
	case translate.BackendOpenAI:
		openAIEffort := translate.MapEffortToOpenAI(effort)
		oReq["reasoning"] = map[string]interface{}{"effort": openAIEffort}
	case translate.BackendDeepSeek:
		oReq["thinking"] = map[string]interface{}{"type": "enabled"}
		oReq["reasoning_effort"] = translate.MapEffortToDeepSeek(effort)
	default:
		oReq["reasoning_effort"] = translate.MapEffortToDeepSeek(effort)
	}
}
