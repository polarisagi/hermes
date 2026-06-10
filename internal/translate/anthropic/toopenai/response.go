package toopenai

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/polarisagi/hermes/internal/translate"
	anthr "github.com/polarisagi/hermes/internal/translate/anthropic"
)

// handleNonStream 将 OpenAI 非流式响应转换为 Anthropic MessageResponse
//
// 响应字段差异：
//   - OpenAI 官方：message.content → text block（无 reasoning_content）
//   - DeepSeek：message.reasoning_content → thinking block，message.content → text block
//   - tool_calls：转为 Anthropic tool_use content blocks
func handleNonStream(w http.ResponseWriter, r *http.Request, resp *http.Response, kind translate.BackendKind) {
	var isCompact bool
	if originalBody, ok := r.Context().Value(translate.OriginalReqBodyKey{}).([]byte); ok {
		var req anthr.MessageRequest
		_ = json.Unmarshal(originalBody, &req)
		isCompact = anthr.IsCompactRequest(&req)
	}

	body, _ := io.ReadAll(resp.Body)
	var oResp map[string]interface{}
	_ = json.Unmarshal(body, &oResp)

	targetModel, _ := oResp["model"].(string)
	if targetModel == "" {
		targetModel = "unknown"
	}

	var contents []map[string]interface{}
	stopReason := "end_turn"

	if choices, ok := oResp["choices"].([]interface{}); ok && len(choices) > 0 {
		choice, _ := choices[0].(map[string]interface{})
		if choice == nil {
			choice = map[string]interface{}{}
		}

		if msg, ok := choice["message"].(map[string]interface{}); ok {
			// 所有后端都可能返回 reasoning_content（DeepSeek、OpenAI GPT-5.x、通用兼容）
			if rc, ok := msg["reasoning_content"].(string); ok && rc != "" {
				contents = append(contents, map[string]interface{}{
					"type":      "thinking",
					"thinking":  rc,
					"signature": "",
				})
			}

			if c, ok := msg["content"].(string); ok && c != "" {
				contents = append(contents, map[string]interface{}{
					"type": "text",
					"text": c,
				})
			}

			if tcs, ok := msg["tool_calls"].([]interface{}); ok && len(tcs) > 0 {
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
					contents = append(contents, map[string]interface{}{
						"type":  "tool_use",
						"id":    tcMap["id"],
						"name":  name,
						"input": input,
					})
				}
			}
		}

		if fr, ok := choice["finish_reason"].(string); ok {
			switch fr {
			case "length":
				stopReason = "max_tokens"
			case "tool_calls":
				stopReason = "tool_use"
			case "content_filter":
				stopReason = "end_turn"
			}
		}
	}

	// compact 模式：检测是否有真实内容，没有则不补空块（避免发出空 compaction 块）
	// 普通模式：若 contents 为空补一个空 text 块保持协议兼容
	if len(contents) == 0 {
		if !isCompact {
			contents = append(contents, map[string]interface{}{"type": "text", "text": ""})
		}
	}

	if isCompact && anthr.HasRealContentMapSlice(contents) {
		anthr.ProcessCompactNonStream(contents)
	}

	id := "msg_unknown"
	if rid, ok := oResp["id"].(string); ok && rid != "" {
		id = rid
	}

	var inputTokens, outputTokens int
	if usage, ok := oResp["usage"].(map[string]interface{}); ok {
		if pt, ok := usage["prompt_tokens"].(float64); ok {
			inputTokens = int(pt)
		}
		if ct, ok := usage["completion_tokens"].(float64); ok {
			outputTokens = int(ct)
		}
	}
	if inputTokens == 0 {
		if originalBody, ok := r.Context().Value(translate.OriginalReqBodyKey{}).([]byte); ok {
			inputTokens = anthr.EstimateInputTokens(originalBody)
		}
	}

	if isCompact {
		anthr.ProcessCompactNonStream(contents)
	}

	aResp := map[string]interface{}{
		"id":            id,
		"type":          "message",
		"role":          "assistant",
		"content":       contents,
		"model":         targetModel,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage": map[string]int{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(aResp)
}

// handleStream 将 OpenAI SSE 流转换为 Anthropic SSE 格式
//
// 处理以下 delta 类型：
//   - delta.reasoning_content → thinking_delta（仅 DeepSeek）
//   - delta.content           → text_delta
//   - delta.tool_calls[i]     → 累积后 → tool_use block + input_json_delta
func handleStream(w http.ResponseWriter, r *http.Request, resp *http.Response, kind translate.BackendKind) {
	var isCompact bool
	var estimatedInputTokens int
	if originalBody, ok := r.Context().Value(translate.OriginalReqBodyKey{}).([]byte); ok {
		var req anthr.MessageRequest
		_ = json.Unmarshal(originalBody, &req)
		isCompact = anthr.IsCompactRequest(&req)
		estimatedInputTokens = anthr.EstimateInputTokens(originalBody)
	}

	// 流式场景无法预先知道 model，从首个 chunk 中读取
	targetModel := "unknown"
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, _ := w.(http.Flusher)
	reader := bufio.NewReader(resp.Body)

	writeEv := func(eventType string, data interface{}) {
		anthr.WriteSSE(w, flusher, eventType, data)
	}

	var msgID string
	sentMessageStart := false
	blockIndex := 0
	inThinking := false
	inText := false
	stopReason := "end_turn"
	compactManager := &anthr.CompactStreamManager{}

	type toolCallAcc struct {
		id        string
		name      string
		arguments strings.Builder
		started   bool
	}
	toolCalls := make(map[int]*toolCallAcc)

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

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
			continue
		}

		if id, ok := chunk["id"].(string); ok && id != "" {
			msgID = id
		}
		if m, ok := chunk["model"].(string); ok && m != "" && targetModel == "unknown" {
			targetModel = m
		}

		if !sentMessageStart {
			msgStartChunk := map[string]interface{}{
				"type": "message_start",
				"message": map[string]interface{}{
					"id":      msgID,
					"type":    "message",
					"role":    "assistant",
					"model":   targetModel,
					"content": []interface{}{},
					"usage":   map[string]interface{}{"input_tokens": 0, "output_tokens": 0},
				},
			}
			anthr.FillMessageStartUsage(msgStartChunk, estimatedInputTokens)
			writeEv("message_start", msgStartChunk)
			sentMessageStart = true
		}

		choices, ok := chunk["choices"].([]interface{})
		if !ok || len(choices) == 0 {
			continue
		}
		choice, ok := choices[0].(map[string]interface{})
		if !ok {
			continue
		}

		if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
			switch fr {
			case "length":
				stopReason = "max_tokens"
			case "tool_calls":
				stopReason = "tool_use"
			case "content_filter":
				stopReason = "end_turn"
			default:
				stopReason = "end_turn"
			}
		}

		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			continue
		}

		// reasoning_content → thinking block（所有后端都可能返回，包括 DeepSeek、OpenAI GPT-5.x）
		if rc, ok := delta["reasoning_content"].(string); ok && rc != "" {
			if isCompact {
				// /compact 模式下，拦截思考内容，不发射 thinking 块
				compactManager.BufferText(rc)
				inText = true
				continue
			}

			if inText {
				writeEv("content_block_stop", map[string]interface{}{"type": "content_block_stop", "index": blockIndex})
				inText = false
				blockIndex++
			}
			if !inThinking {
				writeEv("content_block_start", map[string]interface{}{
					"type": "content_block_start", "index": blockIndex,
					"content_block": map[string]interface{}{"type": "thinking", "thinking": ""},
				})
				inThinking = true
			}
			writeEv("content_block_delta", map[string]interface{}{
				"type": "content_block_delta", "index": blockIndex,
				"delta": map[string]interface{}{"type": "thinking_delta", "thinking": rc},
			})
			continue
		}

		// content → text block
		if content, ok := delta["content"].(string); ok && content != "" {
			if inThinking {
				writeEv("content_block_stop", map[string]interface{}{"type": "content_block_stop", "index": blockIndex})
				inThinking = false
				blockIndex++
			}
			if isCompact {
				compactManager.BufferText(content)
				inText = true    // 确保流结束时触发 compact flush 路径
				continue
			}

			if !inText {
				writeEv("content_block_start", map[string]interface{}{
					"type": "content_block_start", "index": blockIndex,
					"content_block": map[string]interface{}{"type": "text", "text": ""},
				})
				inText = true
			}
			writeEv("content_block_delta", map[string]interface{}{
				"type": "content_block_delta", "index": blockIndex,
				"delta": map[string]interface{}{"type": "text_delta", "text": content},
			})
		}

		// tool_calls → tool_use blocks
		if tcsRaw, ok := delta["tool_calls"].([]interface{}); ok && len(tcsRaw) > 0 {
			if inThinking {
				writeEv("content_block_stop", map[string]interface{}{"type": "content_block_stop", "index": blockIndex})
				inThinking = false
				blockIndex++
			}
			if isCompact && compactManager.HasData() {
				compactManager.Flush(func(eventType string, data interface{}) {
					writeEv(eventType, data)
				}, blockIndex)
				blockIndex++
			} else if inText {
				writeEv("content_block_stop", map[string]interface{}{"type": "content_block_stop", "index": blockIndex})
				inText = false
				blockIndex++
			}

			for _, tcRaw := range tcsRaw {
				tc, ok := tcRaw.(map[string]interface{})
				if !ok {
					continue
				}
				tcIdxF, _ := tc["index"].(float64)
				tcIdx := int(tcIdxF)

				if toolCalls[tcIdx] == nil {
					toolCalls[tcIdx] = &toolCallAcc{}
				}
				acc := toolCalls[tcIdx]

				if id, ok := tc["id"].(string); ok && id != "" {
					acc.id = id
				}
				if fn, ok := tc["function"].(map[string]interface{}); ok {
					if name, ok := fn["name"].(string); ok && name != "" {
						acc.name = name
					}
					if !acc.started && acc.name != "" {
						toolBlockIndex := blockIndex + tcIdx
						writeEv("content_block_start", map[string]interface{}{
							"type": "content_block_start", "index": toolBlockIndex,
							"content_block": map[string]interface{}{
								"type":  "tool_use",
								"id":    acc.id,
								"name":  acc.name,
								"input": map[string]interface{}{},
							},
						})
						acc.started = true
					}
					if args, ok := fn["arguments"].(string); ok && args != "" {
						toolBlockIndex := blockIndex + tcIdx
						acc.arguments.WriteString(args)
						writeEv("content_block_delta", map[string]interface{}{
							"type": "content_block_delta", "index": toolBlockIndex,
							"delta": map[string]interface{}{
								"type":         "input_json_delta",
								"partial_json": args,
							},
						})
					}
				}
			}
		}
	}

	// 流结束：关闭所有开放的块
	if inThinking {
		writeEv("content_block_stop", map[string]interface{}{"type": "content_block_stop", "index": blockIndex})
		blockIndex++
	}
	if isCompact && compactManager.HasData() {
		compactManager.Flush(func(eventType string, data interface{}) {
			writeEv(eventType, data)
		}, blockIndex)
		blockIndex++
	} else if inText {
		writeEv("content_block_stop", map[string]interface{}{"type": "content_block_stop", "index": blockIndex})
		blockIndex++
	}
	for tcIdx, acc := range toolCalls {
		if acc.started {
			writeEv("content_block_stop", map[string]interface{}{
				"type":  "content_block_stop",
				"index": blockIndex + tcIdx,
			})
		}
	}

	if !sentMessageStart {
		msgStartChunk := map[string]interface{}{
			"type": "message_start",
			"message": map[string]interface{}{
				"id": "msg_unknown", "type": "message",
				"role": "assistant", "model": targetModel,
				"content": []interface{}{},
				"usage":   map[string]interface{}{"input_tokens": 0, "output_tokens": 0},
			},
		}
		anthr.FillMessageStartUsage(msgStartChunk, estimatedInputTokens)
		writeEv("message_start", msgStartChunk)
	}

	writeEv("message_delta", map[string]interface{}{
		"type": "message_delta",
		"delta": map[string]interface{}{
			"stop_reason": stopReason, "stop_sequence": nil,
		},
		"usage": map[string]int{"output_tokens": 0},
	})
	writeEv("message_stop", map[string]interface{}{"type": "message_stop"})
}
