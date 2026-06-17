package toanthropic

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
	var aResp map[string]interface{}
	_ = json.Unmarshal(body, &aResp)

	id, _ := aResp["id"].(string)
	if id == "" {
		id = fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	}
	// 从 Anthropic 响应中提取实际模型名
	if m, ok := aResp["model"].(string); ok && m != "" {
		model = m
	}

	var textParts []string
	var reasoningParts []string
	var toolCalls []map[string]interface{}

	if content, ok := aResp["content"].([]interface{}); ok {
		for _, bRaw := range content {
			b, ok := bRaw.(map[string]interface{})
			if !ok {
				continue
			}
			switch b["type"] {
			case "text":
				if text, ok := b["text"].(string); ok {
					textParts = append(textParts, text)
				}
			case "thinking":
				if text, ok := b["thinking"].(string); ok {
					reasoningParts = append(reasoningParts, text)
				}
			case "tool_use":
				tcID, _ := b["id"].(string)
				name, _ := b["name"].(string)
				inputBytes, _ := json.Marshal(b["input"])
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   tcID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      name,
						"arguments": string(inputBytes),
					},
				})
			}
		}
	}

	finishReason := "stop"
	if sr, ok := aResp["stop_reason"].(string); ok {
		switch sr {
		case "max_tokens":
			finishReason = "length"
		case "tool_use":
			finishReason = "tool_calls"
		}
	}

	var inTokens, outTokens, thinkingTokens int
	if usage, ok := aResp["usage"].(map[string]interface{}); ok {
		if it, ok := usage["input_tokens"].(float64); ok {
			inTokens = int(it)
		}
		if ot, ok := usage["output_tokens"].(float64); ok {
			outTokens = int(ot)
		}
		// Anthropic 2025-2026 将思考 token 放在 output_tokens_details.thinking_tokens
		if otd, ok := usage["output_tokens_details"].(map[string]interface{}); ok {
			if tt, ok := otd["thinking_tokens"].(float64); ok {
				thinkingTokens = int(tt)
			}
		}
	}

	msg := map[string]interface{}{
		"role":    "assistant",
		"content": strings.Join(textParts, "\n"),
	}
	if len(reasoningParts) > 0 {
		msg["reasoning_content"] = strings.Join(reasoningParts, "\n")
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
		if len(textParts) == 0 {
			msg["content"] = nil
		}
	}

	oResp := map[string]interface{}{
		"id":      id,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       msg,
				"finish_reason": finishReason,
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     inTokens,
			"completion_tokens": outTokens,
			"total_tokens":      inTokens + outTokens,
			// completion_tokens_details 供上游 Responses API 转换层提取 reasoning_tokens
			"completion_tokens_details": map[string]interface{}{
				"reasoning_tokens": thinkingTokens,
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(oResp)
}

// ── 流式响应处理 ───────────────────────────────────────────────────────────────

func handleStream(w http.ResponseWriter, r *http.Request, resp *http.Response, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, _ := w.(http.Flusher)
	reader := bufio.NewReader(resp.Body)
	created := time.Now().Unix()

	var msgID string
	var currentToolIndex int = -1

	// 用于在流结束时发送完整 usage chunk（含 reasoning_tokens）
	var finalInTokens, finalOutTokens, finalThinkingTokens int

	writeChunk := func(data interface{}) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}

	for {
		if r.Context().Err() != nil {
			break
		}
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

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)
		switch eventType {
		case "message_start":
			if msg, ok := event["message"].(map[string]interface{}); ok {
				if id, ok := msg["id"].(string); ok && id != "" {
					msgID = id
				}
				// 从 Anthropic 流式响应提取实际模型名
				if m, ok := msg["model"].(string); ok && m != "" {
					model = m
				}
				// 捕获 message_start 中的初始 usage（input_tokens）
				if usage, ok := msg["usage"].(map[string]interface{}); ok {
					if it, ok := usage["input_tokens"].(float64); ok {
						finalInTokens = int(it)
					}
				}
				writeChunk(map[string]interface{}{
					"id": msgID, "object": "chat.completion.chunk", "created": created, "model": model,
					"choices": []map[string]interface{}{{"index": 0, "delta": map[string]string{"role": "assistant"}}},
				})
			}

		case "content_block_start":
			if block, ok := event["content_block"].(map[string]interface{}); ok {
				if block["type"] == "tool_use" {
					currentToolIndex++
					id, _ := block["id"].(string)
					name, _ := block["name"].(string)
					writeChunk(map[string]interface{}{
						"id": msgID, "object": "chat.completion.chunk", "created": created, "model": model,
						"choices": []map[string]interface{}{
							{
								"index": 0,
								"delta": map[string]interface{}{
									"tool_calls": []map[string]interface{}{
										{
											"index": currentToolIndex,
											"id":    id, "type": "function",
											"function": map[string]interface{}{"name": name, "arguments": ""},
										},
									},
								},
							},
						},
					})
				}
			}

		case "content_block_delta":
			if delta, ok := event["delta"].(map[string]interface{}); ok {
				deltaType, _ := delta["type"].(string)
				switch deltaType {
				case "text_delta":
					text, _ := delta["text"].(string)
					writeChunk(map[string]interface{}{
						"id": msgID, "object": "chat.completion.chunk", "created": created, "model": model,
						"choices": []map[string]interface{}{{"index": 0, "delta": map[string]string{"content": text}}},
					})
				case "thinking_delta":
					// Anthropic 思考内容 → reasoning_content（与 DeepSeek/Gemini 流式格式对齐）
					thinking, _ := delta["thinking"].(string)
					writeChunk(map[string]interface{}{
						"id": msgID, "object": "chat.completion.chunk", "created": created, "model": model,
						"choices": []map[string]interface{}{{"index": 0, "delta": map[string]interface{}{"reasoning_content": thinking}}},
					})
				case "input_json_delta":
					jsonStr, _ := delta["partial_json"].(string)
					writeChunk(map[string]interface{}{
						"id": msgID, "object": "chat.completion.chunk", "created": created, "model": model,
						"choices": []map[string]interface{}{
							{
								"index": 0,
								"delta": map[string]interface{}{
									"tool_calls": []map[string]interface{}{
										{
											"index":    currentToolIndex,
											"function": map[string]interface{}{"arguments": jsonStr},
										},
									},
								},
							},
						},
					})
				}
			}

		case "message_delta":
			// message_delta 包含 stop_reason 以及最终 usage（output_tokens + thinking_tokens）
			if usage, ok := event["usage"].(map[string]interface{}); ok {
				if ot, ok := usage["output_tokens"].(float64); ok {
					finalOutTokens = int(ot)
				}
				// Anthropic 2025-2026 思考 token 在 output_tokens_details.thinking_tokens
				if otd, ok := usage["output_tokens_details"].(map[string]interface{}); ok {
					if tt, ok := otd["thinking_tokens"].(float64); ok {
						finalThinkingTokens = int(tt)
					}
				}
			}
			if delta, ok := event["delta"].(map[string]interface{}); ok {
				if stopReason, ok := delta["stop_reason"].(string); ok && stopReason != "" {
					reason := "stop"
					switch stopReason {
					case "max_tokens":
						reason = "length"
					case "tool_use":
						reason = "tool_calls"
					}
					writeChunk(map[string]interface{}{
						"id": msgID, "object": "chat.completion.chunk", "created": created, "model": model,
						"choices": []map[string]interface{}{{"index": 0, "delta": map[string]interface{}{}, "finish_reason": reason}},
					})
				}
			}
		}
	}

	// 发送最终 usage chunk（含 reasoning_tokens，供上游 Responses API 转换层统计）
	if msgID != "" {
		writeChunk(map[string]interface{}{
			"id": msgID, "object": "chat.completion.chunk", "created": created, "model": model,
			"choices": []map[string]interface{}{},
			"usage": map[string]interface{}{
				"prompt_tokens":     finalInTokens,
				"completion_tokens": finalOutTokens,
				"total_tokens":      finalInTokens + finalOutTokens,
				"completion_tokens_details": map[string]interface{}{
					"reasoning_tokens": finalThinkingTokens,
				},
			},
		})
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}
