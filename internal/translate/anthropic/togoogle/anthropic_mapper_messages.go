package togoogle

import (
	"encoding/json"
	"fmt"
	"strings"
)

// skipThoughtSigValidator 是 Google 官方提供的占位签名，用于跳过 Gemini 3 的 thoughtSignature 验证。
// 适用场景：历史记录来自其他模型、签名丢失、或我们自己的兜底签名需要替换。
// 参见：https://ai.google.dev/gemini-api/docs/thought-signatures#faqs
const skipThoughtSigValidator = "skip_thought_signature_validator"

// mapMessages 转换历史对话
func mapMessages(messages []Message, model string) ([]map[string]interface{}, error) {
	if lastIdx := findLastCompactionIndex(messages); lastIdx >= 0 {
		messages = messages[lastIdx:]
	}

	toolMap := make(map[string]string)
	flattenedTools := make(map[string]bool)
	buildToolMap(messages, toolMap)

	var contents []map[string]interface{}
	for _, msg := range messages {
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}

		var parts []map[string]interface{}

		switch v := msg.Content.(type) {
		case string:
			parts = append(parts, map[string]interface{}{"text": v})
		case []interface{}:
			for _, item := range v {
				if m, ok := item.(map[string]interface{}); ok {
					switch m["type"] {
					case "text":
						// 不再将 thoughtSignature 挂到 text part——签名应属于 thought part
						parts = append(parts, map[string]interface{}{"text": m["text"]})
					case "thinking":
						// Gemini 3.x 历史回放要求：保留 thought=true part 并携带 thoughtSignature。
						// 之前的做法是丢弃 thinking 内容并把签名挂到下一个 text part，位置错误。
						// 正确做法：重建为原生 thought part，让 Gemini 识别并恢复思考连贯性。
						// 签名缺失时使用官方 bypass 值通知 Gemini 跳过签名验证。
						sig, _ := m["signature"].(string)
						if sig == "" {
							sig = skipThoughtSigValidator
						}
						thinkText, _ := m["thinking"].(string)
						parts = append(parts, map[string]interface{}{
							"thought":          true,
							"text":             thinkText,
							"thoughtSignature": sig,
						})
					case "compaction":
						if content, ok := m["content"].(string); ok && content != "" {
							parts = append(parts, map[string]interface{}{"text": content})
						}
					case "redacted_thinking":
						// 无内容可映射，跳过
						continue
					case "image", "audio", "video", "media":
						if source, ok := m["source"].(map[string]interface{}); ok {
							if part := convertMediaSourceToVertexPart(source, ""); part != nil {
								parts = append(parts, part)
							}
						}
					case "document":
						if source, ok := m["source"].(map[string]interface{}); ok {
							if part := convertMediaSourceToVertexPart(source, "application/pdf"); part != nil {
								parts = append(parts, part)
							}
						}
					case "tool_use":
						// lastSignature 参数已废弃（thinking 块现在内联转换为 thought part），传空字符串
						parsedParts := parseToolUseBlock(m, model, "", flattenedTools)
						parts = append(parts, parsedParts...)
					case "tool_result":
						parts = append(parts, parseToolResultBlock(m, toolMap, flattenedTools)...)
					}
				}
			}
		}

		if len(parts) == 0 {
			// 消息全为 redacted_thinking 等无法映射的块，跳过。
			// 向 Gemini 发送空 turn 会产生噪音并可能引发空响应，直接忽略比注入 {"text":""} 更安全。
			continue
		}

		contents = append(contents, map[string]interface{}{
			"role":  role,
			"parts": parts,
		})
	}

	return enforceAlternatingRoles(contents), nil
}

func buildToolMap(messages []Message, toolMap map[string]string) {
	for _, msg := range messages {
		if arr, ok := msg.Content.([]interface{}); ok {
			for _, item := range arr {
				if m, ok := item.(map[string]interface{}); ok {
					if m["type"] == "tool_use" {
						if id, ok := m["id"].(string); ok {
							if name, ok := m["name"].(string); ok {
								toolMap[id] = name
							}
						}
					}
				}
			}
		}
	}
}

func parseToolUseBlock(m map[string]interface{}, model, lastSignature string, flattenedTools map[string]bool) []map[string]interface{} {
	fc := map[string]interface{}{
		"name": m["name"],
		"args": m["input"],
	}
	partObj := map[string]interface{}{
		"functionCall": fc,
	}

	var thoughtSig string
	toolUseID, _ := m["id"].(string)
	if toolUseID != "" {
		// 从全局缓存或 toolID 编码中提取 Gemini 原生调用 ID
		if callID, ok := toolGeminiCallIDCache.Load(toolUseID); ok {
			fc["id"] = callID.(string)
		} else if idx := strings.Index(toolUseID, "_fcid_"); idx != -1 {
			// 从 toolID 编码中解码（支持 _sig_ 后缀可能在 _fcid_ 之后）
			remaining := toolUseID[idx+6:]
			// _fcid_ 之后可能还有 _sig_ 尾缀
			if sigIdx := strings.Index(remaining, "_sig_"); sigIdx != -1 {
				fc["id"] = remaining[:sigIdx]
			} else {
				fc["id"] = remaining
			}
		}
		// 提取 thoughtSignature
		if sig, ok := toolThoughtSigCache.Load(toolUseID); ok {
			thoughtSig = sig.(string)
		} else if idx := strings.Index(toolUseID, "_sig_"); idx != -1 {
			thoughtSig = toolUseID[idx+5:]
		}
	}

	if thoughtSig == "" && lastSignature != "" {
		thoughtSig = lastSignature
	}

	// 始终填写 thoughtSignature：
	// - Gemini 3 在 functionCall part 上强制要求签名，缺失返回 400。
	// - 有真实 Gemini 签名时直接使用；签名为空或为旧占位符时，改用官方 bypass 值
	//   "skip_thought_signature_validator"，通知 Gemini 跳过验证（官方 FAQ 明确支持）。
	if thoughtSig == "" || thoughtSig == "fallback-no-sig" {
		thoughtSig = skipThoughtSigValidator
	}
	partObj["thoughtSignature"] = thoughtSig

	return []map[string]interface{}{partObj}
}

func parseToolResultBlock(m map[string]interface{}, toolMap map[string]string, flattenedTools map[string]bool) []map[string]interface{} {
	toolUseID, _ := m["tool_use_id"].(string)
	name := toolMap[toolUseID]
	if name == "" {
		name = "unknown_function"
	}

	isError, _ := m["is_error"].(bool)
	var parts []map[string]interface{}
	var respContent map[string]interface{}

	if contentStr, ok := m["content"].(string); ok {
		if isError {
			contentStr = fmt.Sprintf("Error: %s", contentStr)
		}
		respContent = map[string]interface{}{"content": contentStr}
	} else if contentArr, ok := m["content"].([]interface{}); ok {
		var textContents []string
		for _, cItem := range contentArr {
			if cMap, ok := cItem.(map[string]interface{}); ok {
				if cMap["type"] == "text" {
					if textStr, ok := cMap["text"].(string); ok {
						textContents = append(textContents, textStr)
					}
				} else if t, ok := cMap["type"].(string); ok && (t == "image" || t == "document" || t == "audio" || t == "video" || t == "media") {
					if source, ok := cMap["source"].(map[string]interface{}); ok {
						if part := convertMediaSourceToVertexPart(source, ""); part != nil {
							parts = append(parts, part)
						}
					}
				}
			}
		}

		combinedText := strings.Join(textContents, "\n")
		if isError {
			combinedText = fmt.Sprintf("Error: %s", combinedText)
		}
		respContent = map[string]interface{}{"content": combinedText}
	} else {
		rawBytes, _ := json.Marshal(m["content"])
		contentStr := string(rawBytes)
		if isError {
			contentStr = fmt.Sprintf("Error: %s", contentStr)
		}
		respContent = map[string]interface{}{"content": contentStr}
	}

	if flattenedTools[toolUseID] {
		textPart := fmt.Sprintf("<past_tool_result name=\"%s\">\n%s\n</past_tool_result>", name, respContent["content"])
		parts = append(parts, map[string]interface{}{"text": textPart})
	} else {
		// 构建 functionResponse：将 Gemini 原生调用 ID 回传（Gemini 2.5+/3.x 必须）
		// 缺失此 ID 会导致 Gemini 无法匹配工具结果和调用，产生错乱或 400 错误
		funcResp := map[string]interface{}{
			"name":     name,
			"response": respContent,
		}
		// 从全局缓存或 toolID 编码中提取 Gemini 原生调用 ID
		var geminiCallID string
		if toolUseID != "" {
			if callID, ok := toolGeminiCallIDCache.Load(toolUseID); ok {
				geminiCallID = callID.(string)
			} else if idx := strings.Index(toolUseID, "_fcid_"); idx != -1 {
				remaining := toolUseID[idx+6:]
				if sigIdx := strings.Index(remaining, "_sig_"); sigIdx != -1 {
					geminiCallID = remaining[:sigIdx]
				} else {
					geminiCallID = remaining
				}
			}
		}
		if geminiCallID != "" {
			funcResp["id"] = geminiCallID
		}
		parts = append(parts, map[string]interface{}{
			"functionResponse": funcResp,
		})
	}
	return parts
}

func enforceAlternatingRoles(contents []map[string]interface{}) []map[string]interface{} {
	if len(contents) > 1 {
		merged := []map[string]interface{}{contents[0]}
		for i := 1; i < len(contents); i++ {
			last := merged[len(merged)-1]
			curr := contents[i]
			if last["role"] == curr["role"] {
				lastParts, _ := last["parts"].([]map[string]interface{})
				currParts, _ := curr["parts"].([]map[string]interface{})
				last["parts"] = append(lastParts, currParts...)
			} else {
				merged = append(merged, curr)
			}
		}
		contents = merged
	}

	if len(contents) > 0 {
		if contents[0]["role"] == "model" {
			contents = append([]map[string]interface{}{
				{"role": "user", "parts": []map[string]interface{}{{"text": ""}}},
			}, contents...)
		}
	}

	if len(contents) > 0 {
		if contents[len(contents)-1]["role"] == "model" {
			contents = append(contents, map[string]interface{}{
				"role":  "user",
				"parts": []map[string]interface{}{{"text": ""}},
			})
		}
	}
	return contents
}
