package togoogle

import (
	"encoding/json"
	"strings"

	gcommon "github.com/polarisagi/hermes/internal/translate/google"
	"github.com/polarisagi/hermes/internal/translate/openai"
)

// buildGeminiRequest 将 OpenAI 请求体转换为 Gemini 原生格式
func buildGeminiRequest(oReq map[string]interface{}, model string) map[string]interface{} {
	gReq := make(map[string]interface{})

	// 消息转换（需先收集 tool ID→name 映射，供 tool result 使用）
	msgs, _ := oReq["messages"].([]interface{})
	contents, sysInstruction := convertMessages(msgs)
	gReq["contents"] = contents
	if sysInstruction != "" {
		gReq["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]interface{}{{"text": sysInstruction}},
		}
	}

	// 工具定义转换
	if tools, ok := oReq["tools"].([]interface{}); ok && len(tools) > 0 {
		gReq["tools"] = convertToolsToGemini(tools)
	}
	if tc, ok := oReq["tool_choice"]; ok {
		gReq["toolConfig"] = convertToolChoiceToGemini(tc)
	}

	// generationConfig 聚合参数
	gConfig := buildGenerationConfig(oReq)
	if len(gConfig) > 0 {
		gReq["generationConfig"] = gConfig
	}

	// 思考参数：reasoning_effort → thinkingConfig
	thinkingSet := false
	if effort, ok := oReq["reasoning_effort"].(string); ok && effort != "" {
		gReq["generationConfig"] = mergeThinkingConfig(gConfig, effort, model)
		thinkingSet = true
	}
	// 也支持 reasoning: {effort} 格式（OpenAI 官方格式）
	if reasoning, ok := oReq["reasoning"].(map[string]interface{}); ok {
		if effort, ok := reasoning["effort"].(string); ok && effort != "" {
			gReq["generationConfig"] = mergeThinkingConfig(gConfig, effort, model)
			thinkingSet = true
		}
	}

	// Gemini 3.x 在未设置 thinkingConfig 时默认内部思考但不返回 thought parts
	// 设置 includeThoughts: true + MEDIUM 以接收 thought parts 供流式处理器转换
	if !thinkingSet && gcommon.IsGemini3Model(model) {
		if cfg, ok := gReq["generationConfig"].(map[string]interface{}); ok {
			if _, hasTC := cfg["thinkingConfig"]; !hasTC {
				cfg["thinkingConfig"] = map[string]interface{}{
					"includeThoughts": true,
					"thinkingLevel":   "MEDIUM",
				}
				gReq["generationConfig"] = cfg
			}
		} else {
			gReq["generationConfig"] = map[string]interface{}{
				"thinkingConfig": map[string]interface{}{
					"includeThoughts": true,
					"thinkingLevel":   "MEDIUM",
				},
			}
		}
	}

	return gReq
}

// convertMessages 将 OpenAI messages 转换为 Gemini contents 和 systemInstruction
func convertMessages(msgs []interface{}) ([]map[string]interface{}, string) {
	var contents []map[string]interface{}
	var sysLines []string

	// Gemini functionResponse.name 必须与 functionCall.name 一致，而非 tool_call_id。
	// 先从 assistant 消息收集 tool_call_id → function_name 的映射。
	toolIDToName := make(map[string]string)
	for _, m := range msgs {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		if role, _ := msg["role"].(string); role == "assistant" {
			if tcs, ok := msg["tool_calls"].([]interface{}); ok {
				for _, tc := range tcs {
					tcMap, ok := tc.(map[string]interface{})
					if !ok {
						continue
					}
					id, _ := tcMap["id"].(string)
					if fn, ok := tcMap["function"].(map[string]interface{}); ok {
						name, _ := fn["name"].(string)
						if id != "" && name != "" {
							toolIDToName[id] = name
						}
					}
				}
			}
		}
	}

	for _, m := range msgs {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)

		switch role {
		case "system":
			// system → systemInstruction（Gemini 格式）
			text := openai.ExtractTextFromContent(msg["content"])
			if text != "" {
				sysLines = append(sysLines, text)
			}

		case "tool":
			// tool result → Gemini functionResponse（user 角色）
			// functionResponse.name 必须是函数名，而非 tool_call_id
			toolCallID, _ := msg["tool_call_id"].(string)
			funcName := toolIDToName[toolCallID]
			if funcName == "" {
				funcName = toolCallID // 无法找到映射时兜底
			}
			content := openai.ExtractTextFromContent(msg["content"])
			contents = append(contents, map[string]interface{}{
				"role": "user",
				"parts": []map[string]interface{}{
					{
						"functionResponse": map[string]interface{}{
							"name":     funcName,
							"response": map[string]interface{}{"result": content},
						},
					},
				},
			})

		case "user":
			parts := extractParts(msg["content"])
			if len(parts) > 0 {
				contents = append(contents, map[string]interface{}{
					"role":  "user",
					"parts": parts,
				})
			}

		case "assistant":
			parts, toolCalls := extractAssistantParts(msg)
			var allParts []map[string]interface{}
			allParts = append(allParts, parts...)
			// tool_calls → functionCall parts
			allParts = append(allParts, toolCalls...)
			if len(allParts) > 0 {
				contents = append(contents, map[string]interface{}{
					"role":  "model",
					"parts": allParts,
				})
			}
		}
	}

	return contents, strings.TrimSpace(strings.Join(sysLines, "\n"))
}

// extractParts 将 OpenAI content 转换为 Gemini parts（支持文本和图片）
func extractParts(content interface{}) []map[string]interface{} {
	switch v := content.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []map[string]interface{}{{"text": v}}

	case []interface{}:
		var parts []map[string]interface{}
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			switch m["type"] {
			case "text":
				if t, ok := m["text"].(string); ok && t != "" {
					parts = append(parts, map[string]interface{}{"text": t})
				}
			case "image_url":
				// image_url → Gemini inlineData 或 fileData
				if urlObj, ok := m["image_url"].(map[string]interface{}); ok {
					url, _ := urlObj["url"].(string)
					if strings.HasPrefix(url, "data:") {
						// base64 内嵌图片：data:image/jpeg;base64,....
						parts = append(parts, convertBase64Image(url))
					} else {
						// 外部 URL → fileData（Gemini 支持从 URL 读取）
						parts = append(parts, map[string]interface{}{
							"fileData": map[string]interface{}{
								"fileUri":  url,
								"mimeType": "image/jpeg",
							},
						})
					}
				}
			}
		}
		return parts
	}
	return nil
}

// extractAssistantParts 从 assistant 消息提取文本 parts 和工具调用 parts
func extractAssistantParts(msg map[string]interface{}) ([]map[string]interface{}, []map[string]interface{}) {
	var textParts []map[string]interface{}
	var toolCallParts []map[string]interface{}

	// 文本内容
	if content, ok := msg["content"].(string); ok && content != "" {
		textParts = append(textParts, map[string]interface{}{"text": content})
	}

	// tool_calls → Gemini functionCall parts
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
			var args interface{}
			if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
				args = map[string]interface{}{}
			}
			toolCallParts = append(toolCallParts, map[string]interface{}{
				"functionCall": map[string]interface{}{
					"name": name,
					"args": args,
				},
			})
		}
	}

	return textParts, toolCallParts
}

// convertBase64Image 将 base64 data URL 转换为 Gemini inlineData
func convertBase64Image(dataURL string) map[string]interface{} {
	// data:image/jpeg;base64,<data>
	parts := strings.SplitN(dataURL, ",", 2)
	mimeType := "image/jpeg"
	if len(parts) == 2 {
		meta := parts[0] // data:image/jpeg;base64
		if idx := strings.Index(meta, ":"); idx != -1 {
			if idx2 := strings.Index(meta[idx+1:], ";"); idx2 != -1 {
				mimeType = meta[idx+1 : idx+1+idx2]
			}
		}
		return map[string]interface{}{
			"inlineData": map[string]interface{}{
				"mimeType": mimeType,
				"data":     parts[1],
			},
		}
	}
	return map[string]interface{}{"text": "[unsupported image]"}
}

// convertToolsToGemini 将 OpenAI tools 转换为 Gemini functionDeclarations
func convertToolsToGemini(tools []interface{}) []map[string]interface{} {
	var fnDecls []map[string]interface{}
	for _, tool := range tools {
		t, ok := tool.(map[string]interface{})
		if !ok {
			continue
		}
		if t["type"] != "function" {
			continue
		}
		fn, ok := t["function"].(map[string]interface{})
		if !ok {
			continue
		}
		decl := map[string]interface{}{
			"name": fn["name"],
		}
		if desc, ok := fn["description"].(string); ok {
			decl["description"] = desc
		}
		if params, ok := fn["parameters"]; ok {
			decl["parameters"] = params
		}
		fnDecls = append(fnDecls, decl)
	}
	if len(fnDecls) == 0 {
		return nil
	}
	return []map[string]interface{}{
		{"functionDeclarations": fnDecls},
	}
}

// convertToolChoiceToGemini 将 OpenAI tool_choice 转换为 Gemini toolConfig
func convertToolChoiceToGemini(tc interface{}) map[string]interface{} {
	switch v := tc.(type) {
	case string:
		switch v {
		case "auto":
			return map[string]interface{}{"functionCallingConfig": map[string]interface{}{"mode": "AUTO"}}
		case "none":
			return map[string]interface{}{"functionCallingConfig": map[string]interface{}{"mode": "NONE"}}
		case "required":
			return map[string]interface{}{"functionCallingConfig": map[string]interface{}{"mode": "ANY"}}
		}
	case map[string]interface{}:
		// {type:"function", function:{name:"..."}}
		if fn, ok := v["function"].(map[string]interface{}); ok {
			if name, ok := fn["name"].(string); ok {
				return map[string]interface{}{
					"functionCallingConfig": map[string]interface{}{
						"mode":                 "ANY",
						"allowedFunctionNames": []string{name},
					},
				}
			}
		}
	}
	return nil
}

// buildGenerationConfig 从 OpenAI 请求提取 Gemini generationConfig 参数
func buildGenerationConfig(oReq map[string]interface{}) map[string]interface{} {
	gConfig := make(map[string]interface{})
	if temp, ok := oReq["temperature"]; ok {
		gConfig["temperature"] = temp
	}
	if topP, ok := oReq["top_p"]; ok {
		gConfig["topP"] = topP
	}
	if maxTokens, ok := oReq["max_tokens"].(float64); ok && maxTokens > 0 {
		gConfig["maxOutputTokens"] = int(maxTokens)
	}
	if maxCT, ok := oReq["max_completion_tokens"].(float64); ok && maxCT > 0 {
		gConfig["maxOutputTokens"] = int(maxCT)
	}
	if stops, ok := oReq["stop"].([]interface{}); ok && len(stops) > 0 {
		var stopSeqs []string
		for _, s := range stops {
			if str, ok := s.(string); ok {
				stopSeqs = append(stopSeqs, str)
			}
		}
		if len(stopSeqs) > 0 {
			gConfig["stopSequences"] = stopSeqs
		}
	}
	// response_format → Gemini responseMimeType / responseSchema（结构化输出）
	if rf, ok := oReq["response_format"].(map[string]interface{}); ok {
		rfType, _ := rf["type"].(string)
		switch rfType {
		case "json_object":
			gConfig["responseMimeType"] = "application/json"
		case "json_schema":
			gConfig["responseMimeType"] = "application/json"
			if js, ok := rf["json_schema"].(map[string]interface{}); ok {
				if schema, ok := js["schema"]; ok {
					gConfig["responseSchema"] = schema
				}
			}
		}
	}
	return gConfig
}

// mergeThinkingConfig 将思考参数合并到 generationConfig
//
// Gemini 2.5：thinkingConfig.thinkingBudget（整数 token 数，-1=动态，0=禁用）
// Gemini 3.x：thinkingConfig.thinkingLevel 枚举（MINIMAL/LOW/MEDIUM/HIGH），不接受 thinkingBudget
//
// OpenAI effort → Gemini 2.5 thinkingBudget 映射：
//
//	none/low   → 0（禁用）
//	medium     → 8192
//	high       → 16384
//	xhigh/max  → -1（动态）
//
// OpenAI effort → Gemini 3.x thinkingLevel 映射：
//
//	none/low   → MINIMAL
//	medium     → MEDIUM
//	high       → HIGH
//	xhigh/max  → HIGH
func mergeThinkingConfig(gConfig map[string]interface{}, effort string, model string) map[string]interface{} {
	if gConfig == nil {
		gConfig = make(map[string]interface{})
	}
	e := strings.ToLower(effort)
	if gcommon.IsGemini3Model(model) {
		// Gemini 3.x：使用 thinkingLevel 枚举
		var level string
		switch e {
		case "none":
			level = "MINIMAL"
		case "minimal", "low":
			level = "MINIMAL"
		case "medium":
			level = "MEDIUM"
		case "high", "xhigh", "max":
			level = "HIGH"
		default:
			level = "MEDIUM"
		}
		gConfig["thinkingConfig"] = map[string]interface{}{
			"includeThoughts": true,
			"thinkingLevel":   level,
		}
	} else {
		// Gemini 2.5：使用 thinkingBudget 整数
		// 注意：Gemini 2.5 Pro 不可靠地支持 budget=0 禁用思考，
		// low/minimal 映射到最小值 1024 而非 0
		var budget int
		switch e {
		case "none":
			budget = 0
		case "minimal", "low":
			budget = 1024
		case "medium":
			budget = 8192
		case "high":
			budget = 16384
		case "xhigh", "max":
			budget = -1
		default:
			budget = 8192
		}
		gConfig["thinkingConfig"] = map[string]interface{}{
			"includeThoughts": budget != 0,
			"thinkingBudget":  budget,
		}
	}
	return gConfig
}
