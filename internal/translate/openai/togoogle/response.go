package togoogle

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ── 非流式响应处理 ─────────────────────────────────────────────────────────────

func handleNonStream(w http.ResponseWriter, resp *http.Response, model string) {
	body, _ := io.ReadAll(resp.Body)
	var gResp map[string]interface{}
	_ = json.Unmarshal(body, &gResp)

	// 从响应体提取 model
	if model == "gemini_model" {
		if m, ok := gResp["modelVersion"].(string); ok && m != "" {
			model = m
		}
	}

	var textParts []string
	var thoughtParts []string
	var toolCalls []map[string]interface{}
	finishReason := "stop"

	if candidates, ok := gResp["candidates"].([]interface{}); ok && len(candidates) > 0 {
		cand, _ := candidates[0].(map[string]interface{})
		if cand == nil {
			cand = map[string]interface{}{}
		}

		if content, ok := cand["content"].(map[string]interface{}); ok {
			if parts, ok := content["parts"].([]interface{}); ok {
				for _, p := range parts {
					part, ok := p.(map[string]interface{})
					if !ok {
						continue
					}
					// thought=true 的 part 是 Gemini 的推理内容 → 映射为 reasoning_content
					if isThought, _ := part["thought"].(bool); isThought {
						if text, ok := part["text"].(string); ok && text != "" {
							thoughtParts = append(thoughtParts, text)
						}
						continue
					}
					if text, ok := part["text"].(string); ok && text != "" {
						textParts = append(textParts, text)
					}
					// functionCall → tool_calls
					if fc, ok := part["functionCall"].(map[string]interface{}); ok {
						name, _ := fc["name"].(string)
						args := fc["args"]
						argsBytes, _ := json.Marshal(args)
						toolCalls = append(toolCalls, map[string]interface{}{
							"id":   fmt.Sprintf("call_%s_%d", name, time.Now().UnixNano()),
							"type": "function",
							"function": map[string]interface{}{
								"name":      name,
								"arguments": string(argsBytes),
							},
						})
					}
				}
			}
		}

		if fr, ok := cand["finishReason"].(string); ok {
			finishReason = mapFinishReason(fr)
		}
	}

	// token 统计：思考 token 单独记录，以便 Responses API 正确填充 reasoning_tokens
	var promptTokens, completionTokens, reasoningTokens int
	if usage, ok := gResp["usageMetadata"].(map[string]interface{}); ok {
		if pt, ok := usage["promptTokenCount"].(float64); ok {
			promptTokens = int(pt)
		}
		if ct, ok := usage["candidatesTokenCount"].(float64); ok {
			completionTokens = int(ct)
		}
		if tt, ok := usage["thoughtsTokenCount"].(float64); ok {
			reasoningTokens = int(tt)
			completionTokens += reasoningTokens
		}
	}

	msg := map[string]interface{}{
		"role":    "assistant",
		"content": strings.Join(textParts, ""),
	}
	if len(thoughtParts) > 0 {
		msg["reasoning_content"] = strings.Join(thoughtParts, "")
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
		if len(textParts) == 0 {
			msg["content"] = nil
		}
		finishReason = "tool_calls"
	}

	oResp := map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-gemini-%d", time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       msg,
				"finish_reason": finishReason,
				"logprobs":      nil,
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
			"completion_tokens_details": map[string]interface{}{
				"reasoning_tokens": reasoningTokens,
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(oResp)
}

// ── 流式响应处理 ───────────────────────────────────────────────────────────────

func handleStream(w http.ResponseWriter, resp *http.Response, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, _ := w.(http.Flusher)
	reader := bufio.NewReader(resp.Body)
	created := time.Now().Unix()
	chatID := fmt.Sprintf("chatcmpl-gemini-%d", time.Now().UnixNano())

	writeChunk := func(chunk map[string]interface{}) {
		b, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}

	// 流式工具调用累积（Gemini 流式中工具调用可能分多个 chunk）
	toolCallIndex := 0
	sentToolCallStart := make(map[string]bool) // name → 是否已发出首个 chunk

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if dataStr == "[DONE]" || dataStr == "" {
			continue
		}

		var gChunk map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &gChunk); err != nil {
			continue
		}

		candidates, ok := gChunk["candidates"].([]interface{})
		if !ok || len(candidates) == 0 {
			continue
		}
		cand, ok := candidates[0].(map[string]interface{})
		if !ok {
			continue
		}

		finishReason := ""
		if fr, ok := cand["finishReason"].(string); ok && fr != "" && fr != "STOP" {
			// 非正常结束原因在最后的 chunk 处理
			finishReason = mapFinishReason(fr)
		} else if fr == "STOP" {
			finishReason = "stop"
		}

		content, _ := cand["content"].(map[string]interface{})
		parts, _ := content["parts"].([]interface{})

		for _, p := range parts {
			part, ok := p.(map[string]interface{})
			if !ok {
				continue
			}

			// thought=true 的 part → reasoning_content delta（OpenAI 兼容格式）
			if isThought, _ := part["thought"].(bool); isThought {
				if text, ok := part["text"].(string); ok && text != "" {
					writeChunk(map[string]interface{}{
						"id": chatID, "object": "chat.completion.chunk",
						"created": created, "model": model,
						"choices": []map[string]interface{}{
							{
								"index":         0,
								"delta":         map[string]interface{}{"reasoning_content": text},
								"finish_reason": nil,
							},
						},
					})
				}
				continue
			}

			// 文本 part → content delta
			if text, ok := part["text"].(string); ok && text != "" {
				writeChunk(map[string]interface{}{
					"id": chatID, "object": "chat.completion.chunk",
					"created": created, "model": model,
					"choices": []map[string]interface{}{
						{
							"index":         0,
							"delta":         map[string]interface{}{"content": text},
							"finish_reason": nil,
						},
					},
				})
			}

			// functionCall part → tool_calls delta
			if fc, ok := part["functionCall"].(map[string]interface{}); ok {
				name, _ := fc["name"].(string)
				args := fc["args"]
				argsBytes, _ := json.Marshal(args)

				tcID := fmt.Sprintf("call_%s_%d", name, time.Now().UnixNano())

				if !sentToolCallStart[name] {
					// 首次：发出包含 id 和 name 的 chunk
					writeChunk(map[string]interface{}{
						"id": chatID, "object": "chat.completion.chunk",
						"created": created, "model": model,
						"choices": []map[string]interface{}{
							{
								"index": 0,
								"delta": map[string]interface{}{
									"role": "assistant",
									"tool_calls": []map[string]interface{}{
										{
											"index": toolCallIndex,
											"id":    tcID,
											"type":  "function",
											"function": map[string]interface{}{
												"name":      name,
												"arguments": "",
											},
										},
									},
								},
								"finish_reason": nil,
							},
						},
					})
					sentToolCallStart[name] = true
				}

				// 发出 arguments 增量
				writeChunk(map[string]interface{}{
					"id": chatID, "object": "chat.completion.chunk",
					"created": created, "model": model,
					"choices": []map[string]interface{}{
						{
							"index": 0,
							"delta": map[string]interface{}{
								"tool_calls": []map[string]interface{}{
									{
										"index": toolCallIndex,
										"function": map[string]interface{}{
											"arguments": string(argsBytes),
										},
									},
								},
							},
							"finish_reason": nil,
						},
					},
				})
				toolCallIndex++
			}
		}

		// 最后一个 chunk 携带 finish_reason
		if finishReason != "" {
			fr := finishReason
			if len(sentToolCallStart) > 0 {
				fr = "tool_calls"
			}
			writeChunk(map[string]interface{}{
				"id": chatID, "object": "chat.completion.chunk",
				"created": created, "model": model,
				"choices": []map[string]interface{}{
					{"index": 0, "delta": map[string]interface{}{}, "finish_reason": fr},
				},
			})
		}
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}
