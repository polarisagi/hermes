package openai

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ResponsesAPIToChatCompletions 将 Responses API 请求转换为 Chat Completions 请求
func ResponsesAPIToChatCompletions(bodyBytes []byte, targetModel string, isDeepSeek bool) ([]byte, error) {
	var rReq map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &rReq); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}

	cReq := make(map[string]interface{})

	if targetModel != "" {
		cReq["model"] = targetModel
	} else if m, ok := rReq["model"].(string); ok {
		cReq["model"] = m
	}

	if s, ok := rReq["stream"].(bool); ok {
		cReq["stream"] = s
		if s {
			cReq["stream_options"] = map[string]interface{}{"include_usage": true}
		}
	}

	messages := convertResponsesInput(rReq["input"])
	if instructions, ok := rReq["instructions"].(string); ok && instructions != "" {
		messages = append([]map[string]interface{}{{"role": "system", "content": instructions}}, messages...)
	}
	cReq["messages"] = messages

	if reasoning, ok := rReq["reasoning"].(map[string]interface{}); ok {
		if effort, ok := reasoning["effort"].(string); ok && effort != "" {
			// DeepSeek 不支持 OpenAI 的 reasoning_effort 字段，也不支持 Anthropic 的 thinking 字段
			// deepseek-reasoner 会自动输出推理内容，无需额外参数
			if !isDeepSeek {
				cReq["reasoning_effort"] = effort
			}
		}
	}

	if mot, ok := rReq["max_output_tokens"].(float64); ok && mot > 0 {
		cReq["max_tokens"] = int(mot)
	}

	if temp, ok := rReq["temperature"]; ok {
		cReq["temperature"] = temp
	}
	if topP, ok := rReq["top_p"]; ok {
		cReq["top_p"] = topP
	}
	if tools, ok := rReq["tools"]; ok {
		converted := convertResponsesTools(tools)
		if len(converted) > 0 {
			cReq["tools"] = converted
		}
	}
	if tc, ok := rReq["tool_choice"]; ok {
		cReq["tool_choice"] = convertResponsesToolChoice(tc)
	}
	if ptc, ok := rReq["parallel_tool_calls"]; ok {
		cReq["parallel_tool_calls"] = ptc
	}
	// text.format → response_format（结构化输出）
	if textObj, ok := rReq["text"].(map[string]interface{}); ok {
		if format, ok := textObj["format"].(map[string]interface{}); ok {
			converted := convertResponsesTextFormat(format)
			if t, _ := converted["type"].(string); t != "text" {
				cReq["response_format"] = converted
			}
		}
	}

	return json.Marshal(cReq)
}

func convertResponsesInput(input interface{}) []map[string]interface{} {
	if input == nil {
		return []map[string]interface{}{}
	}
	switch v := input.(type) {
	case string:
		return []map[string]interface{}{{"role": "user", "content": v}}
	case []interface{}:
		var msgs []map[string]interface{}
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			itemType, _ := m["type"].(string)
			switch itemType {
			case "message":
				role, _ := m["role"].(string)
				// developer 是 OpenAI 新增角色，等价于 system，第三方后端不支持
				if role == "developer" {
					role = "system"
				}
				content := extractResponsesContent(m["content"])
				if content != "" {
					msgs = append(msgs, map[string]interface{}{"role": role, "content": content})
				}
			case "function_call":
				// 模型工具调用 → Chat Completions assistant message with tool_calls
				callID, _ := m["call_id"].(string)
				name, _ := m["name"].(string)
				arguments, _ := m["arguments"].(string)
				toolCall := map[string]interface{}{
					"id":   callID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      name,
						"arguments": arguments,
					},
				}
				// 尝试合并到前一条 assistant 消息（支持同一回合多个工具调用，或文本+工具调用）
				if len(msgs) > 0 && msgs[len(msgs)-1]["role"] == "assistant" {
					prev := msgs[len(msgs)-1]
					if existing, ok := prev["tool_calls"].([]interface{}); ok {
						prev["tool_calls"] = append(existing, toolCall)
					} else {
						prev["tool_calls"] = []interface{}{toolCall}
					}
				} else {
					msgs = append(msgs, map[string]interface{}{
						"role":       "assistant",
						"content":    nil,
						"tool_calls": []interface{}{toolCall},
					})
				}
			case "function_call_output":
				// 工具执行结果 → Chat Completions tool message
				// output 可以是字符串或内容数组（openai spec 允许两种形式）
				callID, _ := m["call_id"].(string)
				output := extractResponsesContent(m["output"])
				msgs = append(msgs, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": callID,
					"content":      output,
				})
			}
		}
		return msgs
	}
	return []map[string]interface{}{}
}

func extractResponsesContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if text, ok := m["text"].(string); ok && text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// convertResponsesTools 将 Responses API 工具格式转为 Chat Completions 格式。
// Responses API：{"type":"function","name":"...","description":"...","parameters":{}}
// Chat Completions：{"type":"function","function":{"name":"...","description":"...","parameters":{}}}
// 内置工具（file_search、code_interpreter 等）第三方后端不支持，直接过滤。
func convertResponsesTools(toolsRaw interface{}) []interface{} {
	tools, ok := toolsRaw.([]interface{})
	if !ok {
		return nil
	}
	var result []interface{}
	for _, t := range tools {
		m, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		toolType, _ := m["type"].(string)
		if toolType != "function" {
			// file_search / code_interpreter / web_search_preview / computer_use_preview 等内置工具跳过
			continue
		}
		// 已经是 Chat Completions 格式（有 function 子对象）则直接透传
		if _, hasFn := m["function"]; hasFn {
			result = append(result, m)
			continue
		}
		// 从 Responses API 格式转换
		fn := map[string]interface{}{}
		if name, ok := m["name"].(string); ok {
			fn["name"] = name
		}
		if desc, ok := m["description"].(string); ok {
			fn["description"] = desc
		}
		if params, ok := m["parameters"]; ok {
			fn["parameters"] = params
		}
		if strict, ok := m["strict"]; ok {
			fn["strict"] = strict
		}
		result = append(result, map[string]interface{}{
			"type":     "function",
			"function": fn,
		})
	}
	return result
}

// convertResponsesToolChoice 将 Responses API tool_choice 转为 Chat Completions 格式。
func convertResponsesToolChoice(tc interface{}) interface{} {
	switch v := tc.(type) {
	case string:
		return v // "auto" / "required" / "none" 直接兼容
	case map[string]interface{}:
		tcType, _ := v["type"].(string)
		if tcType == "function" {
			name, _ := v["name"].(string)
			return map[string]interface{}{
				"type":     "function",
				"function": map[string]interface{}{"name": name},
			}
		}
		// 其他类型尝试转为字符串，fallback auto
		if tcType != "" {
			return tcType
		}
		return "auto"
	}
	return "auto"
}

// convertResponsesTextFormat 将 Responses API text.format 转为 Chat Completions response_format。
//
// Responses API: {type:"json_schema", name:"...", schema:{...}, strict:true}
// Chat Completions: {type:"json_schema", json_schema:{name:"...", schema:{...}, strict:true}}
func convertResponsesTextFormat(format map[string]interface{}) map[string]interface{} {
	formatType, _ := format["type"].(string)
	switch formatType {
	case "json_schema":
		jsonSchema := map[string]interface{}{}
		if name, ok := format["name"].(string); ok {
			jsonSchema["name"] = name
		}
		if schema, ok := format["schema"]; ok {
			jsonSchema["schema"] = schema
		}
		if strict, ok := format["strict"]; ok {
			jsonSchema["strict"] = strict
		}
		if desc, ok := format["description"].(string); ok && desc != "" {
			jsonSchema["description"] = desc
		}
		return map[string]interface{}{"type": "json_schema", "json_schema": jsonSchema}
	case "json_object":
		return map[string]interface{}{"type": "json_object"}
	default:
		return map[string]interface{}{"type": "text"}
	}
}

// PipeResponseWriter 拦截响应头并将响应体转发到 io.Writer
type PipeResponseWriter struct {
	OriginalW http.ResponseWriter
	Pw        *io.PipeWriter
	header    http.Header
	status    int
	isStream  bool
	headerCh  chan struct{}
	closeOnce sync.Once
}

func NewPipeResponseWriter(w http.ResponseWriter, pw *io.PipeWriter) *PipeResponseWriter {
	return &PipeResponseWriter{
		OriginalW: w,
		Pw:        pw,
		header:    make(http.Header),
		headerCh:  make(chan struct{}),
	}
}

func (p *PipeResponseWriter) Header() http.Header {
	return p.header
}

func (p *PipeResponseWriter) WriteHeader(statusCode int) {
	p.status = statusCode
	p.isStream = strings.Contains(p.header.Get("Content-Type"), "text/event-stream")
	p.closeOnce.Do(func() { close(p.headerCh) })
}

func (p *PipeResponseWriter) Close() {
	p.closeOnce.Do(func() { close(p.headerCh) })
}

func (p *PipeResponseWriter) Write(b []byte) (int, error) {
	return p.Pw.Write(b)
}

func (p *PipeResponseWriter) WaitHeaders() {
	<-p.headerCh
}

func (p *PipeResponseWriter) IsStream() bool {
	return p.isStream
}

func (p *PipeResponseWriter) Status() int {
	return p.status
}

func (p *PipeResponseWriter) Flush() {}

// buildResponseObj 构建 Responses API 规范的 response 对象
func buildResponseObj(id string, createdAt int64, model, status string, output []interface{}, usage interface{}) map[string]interface{} {
	resp := map[string]interface{}{
		"id":                   id,
		"object":               "response",
		"created_at":           createdAt,
		"model":                model,
		"status":               status,
		"error":                nil,
		"incomplete_details":   nil,
		"instructions":         nil,
		"max_output_tokens":    nil,
		"output":               output,
		"parallel_tool_calls":  true,
		"previous_response_id": nil,
		"reasoning":            map[string]interface{}{"effort": nil, "summary": nil},
		"store":                false,
		"temperature":          1.0,
		"text":                 map[string]interface{}{"format": map[string]interface{}{"type": "text"}},
		"tool_choice":          "auto",
		"tools":                []interface{}{},
		"top_p":                1.0,
		"truncation":           "disabled",
		"usage":                usage,
		"user":                 nil,
		"metadata":             map[string]interface{}{},
	}
	return resp
}

func HandleResponsesNonStream(w http.ResponseWriter, reader io.Reader, model string) {
	body, _ := io.ReadAll(reader)
	var cResp map[string]interface{}
	_ = json.Unmarshal(body, &cResp)

	var outputItems []interface{}
	finishReason := "end_turn"

	if choices, ok := cResp["choices"].([]interface{}); ok && len(choices) > 0 {
		choice, _ := choices[0].(map[string]interface{})
		if msg, ok := choice["message"].(map[string]interface{}); ok {
			if rc, ok := msg["reasoning_content"].(string); ok && rc != "" {
				outputItems = append(outputItems, map[string]interface{}{
					"id":      fmt.Sprintf("rs_%d", time.Now().UnixNano()),
					"type":    "reasoning",
					"status":  "completed",
					"summary": []map[string]interface{}{{"type": "summary_text", "text": rc}},
				})
			}
			if content, ok := msg["content"].(string); ok && content != "" {
				outputItems = append(outputItems, map[string]interface{}{
					"id":     fmt.Sprintf("msg_%d", time.Now().UnixNano()),
					"type":   "message",
					"status": "completed",
					"role":   "assistant",
					"content": []map[string]interface{}{{
						"type":        "output_text",
						"text":        content,
						"annotations": []interface{}{},
					}},
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
					outputItems = append(outputItems, map[string]interface{}{
						"id":        fmt.Sprintf("call_%s", tcMap["id"]),
						"type":      "function_call",
						"status":    "completed",
						"call_id":   tcMap["id"],
						"name":      fn["name"],
						"arguments": fn["arguments"],
					})
				}
			}
		}
		if fr, ok := choice["finish_reason"].(string); ok {
			switch fr {
			case "length":
				finishReason = "max_tokens"
			case "tool_calls":
				finishReason = "tool_calls"
			}
		}
	}

	id := fmt.Sprintf("resp_%d", time.Now().UnixNano())
	if rid, ok := cResp["id"].(string); ok && rid != "" {
		id = rid
	}

	var inputTokens, outputTokens, reasoningTokens int
	if usage, ok := cResp["usage"].(map[string]interface{}); ok {
		if pt, ok := usage["prompt_tokens"].(float64); ok {
			inputTokens = int(pt)
		}
		if ct, ok := usage["completion_tokens"].(float64); ok {
			outputTokens = int(ct)
		}
		if details, ok := usage["completion_tokens_details"].(map[string]interface{}); ok {
			if rt, ok := details["reasoning_tokens"].(float64); ok {
				reasoningTokens = int(rt)
			}
		}
	}

	usageObj := map[string]interface{}{
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
		"output_tokens_details": map[string]interface{}{
			"reasoning_tokens": reasoningTokens,
		},
		"total_tokens": inputTokens + outputTokens,
	}

	now := time.Now().Unix()
	rResp := buildResponseObj(id, now, model, "completed", outputItems, usageObj)
	rResp["completed_at"] = now
	rResp["finish_reason"] = finishReason

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(rResp)
}

func HandleResponsesStream(w http.ResponseWriter, streamReader io.Reader, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, _ := w.(http.Flusher)
	reader := bufio.NewReader(streamReader)

	respID := fmt.Sprintf("resp_%d", time.Now().UnixNano())
	created := time.Now().Unix()

	var seqNum int
	// 每个 SSE 事件必须同时发送 event: <type> 和 data: <json>，并附带递增 sequence_number
	writeEv := func(eventType string, data map[string]interface{}) {
		data["sequence_number"] = seqNum
		seqNum++
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, b)
		if flusher != nil {
			flusher.Flush()
		}
	}

	emptyOutput := []interface{}{}

	writeEv("response.created", map[string]interface{}{
		"type":     "response.created",
		"response": buildResponseObj(respID, created, model, "in_progress", emptyOutput, nil),
	})

	writeEv("response.in_progress", map[string]interface{}{
		"type":     "response.in_progress",
		"response": buildResponseObj(respID, created, model, "in_progress", emptyOutput, nil),
	})

	var fullText strings.Builder
	var fullReasoning strings.Builder
	stopReason := "end_turn"

	var reasoningAdded bool
	var reasoningEnded bool
	var reasoningIndex int
	var reasoningItemID string

	var messageAdded bool
	var messageIndex int
	var messageItemID string

	var nextOutputIndex int

	type funcCallAcc struct {
		id        string
		name      string
		arguments strings.Builder
		outputIdx int
	}
	var toolCalls []*funcCallAcc
	var finalUsage map[string]interface{}

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

		if u, ok := chunk["usage"].(map[string]interface{}); ok && u != nil {
			finalUsage = u
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
				stopReason = "tool_calls"
			}
		}

		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			continue
		}

		// reasoning_content → Responses API reasoning item
		if rc, ok := delta["reasoning_content"].(string); ok && rc != "" {
			if !reasoningAdded {
				reasoningAdded = true
				reasoningIndex = nextOutputIndex
				reasoningItemID = fmt.Sprintf("rs_%d", time.Now().UnixNano())
				nextOutputIndex++
				writeEv("response.output_item.added", map[string]interface{}{
					"type":         "response.output_item.added",
					"output_index": reasoningIndex,
					"item": map[string]interface{}{
						"id":      reasoningItemID,
						"type":    "reasoning",
						"status":  "in_progress",
						"summary": []interface{}{},
					},
				})
				writeEv("response.reasoning_summary_part.added", map[string]interface{}{
					"type":          "response.reasoning_summary_part.added",
					"output_index":  reasoningIndex,
					"summary_index": 0,
					"item_id":       reasoningItemID,
					"part":          map[string]interface{}{"type": "summary_text", "text": ""},
				})
			}
			fullReasoning.WriteString(rc)
			writeEv("response.reasoning_summary_text.delta", map[string]interface{}{
				"type":          "response.reasoning_summary_text.delta",
				"output_index":  reasoningIndex,
				"summary_index": 0,
				"item_id":       reasoningItemID,
				"delta":         rc,
			})
		}

		// text content → Responses API message item
		if content, ok := delta["content"].(string); ok && content != "" {
			// 先关闭推理块（如果存在）
			if reasoningAdded && !reasoningEnded {
				reasoningEnded = true
				writeEv("response.reasoning_summary_text.done", map[string]interface{}{
					"type":          "response.reasoning_summary_text.done",
					"output_index":  reasoningIndex,
					"summary_index": 0,
					"item_id":       reasoningItemID,
					"text":          fullReasoning.String(),
				})
				writeEv("response.output_item.done", map[string]interface{}{
					"type":         "response.output_item.done",
					"output_index": reasoningIndex,
					"item": map[string]interface{}{
						"id":     reasoningItemID,
						"type":   "reasoning",
						"status": "completed",
						"summary": []map[string]interface{}{
							{"type": "summary_text", "text": fullReasoning.String()},
						},
					},
				})
			}

			if !messageAdded {
				messageAdded = true
				messageIndex = nextOutputIndex
				messageItemID = fmt.Sprintf("msg_%d", time.Now().UnixNano())
				nextOutputIndex++
				writeEv("response.output_item.added", map[string]interface{}{
					"type":         "response.output_item.added",
					"output_index": messageIndex,
					"item": map[string]interface{}{
						"id":      messageItemID,
						"type":    "message",
						"status":  "in_progress",
						"role":    "assistant",
						"content": []interface{}{},
					},
				})
				writeEv("response.content_part.added", map[string]interface{}{
					"type":          "response.content_part.added",
					"item_id":       messageItemID,
					"output_index":  messageIndex,
					"content_index": 0,
					"part": map[string]interface{}{
						"type":        "output_text",
						"text":        "",
						"annotations": []interface{}{},
					},
				})
			}
			fullText.WriteString(content)
			writeEv("response.output_text.delta", map[string]interface{}{
				"type":          "response.output_text.delta",
				"item_id":       messageItemID,
				"output_index":  messageIndex,
				"content_index": 0,
				"delta":         content,
				"logprobs":      []interface{}{},
			})
		}

		// tool_calls → Responses API function_call items
		if tcsRaw, ok := delta["tool_calls"].([]interface{}); ok {
			for _, tcRaw := range tcsRaw {
				tc, ok := tcRaw.(map[string]interface{})
				if !ok {
					continue
				}
				idxF, _ := tc["index"].(float64)
				idx := int(idxF)

				for len(toolCalls) <= idx {
					toolCalls = append(toolCalls, nil)
				}
				if toolCalls[idx] == nil {
					toolCalls[idx] = &funcCallAcc{outputIdx: nextOutputIndex}
					nextOutputIndex++
				}
				acc := toolCalls[idx]

				if id, ok := tc["id"].(string); ok && id != "" {
					acc.id = id
				}
				if fn, ok := tc["function"].(map[string]interface{}); ok {
					if name, ok := fn["name"].(string); ok && name != "" {
						if acc.name == "" {
							acc.name = name
							writeEv("response.output_item.added", map[string]interface{}{
								"type":         "response.output_item.added",
								"output_index": acc.outputIdx,
								"item": map[string]interface{}{
									"id":      fmt.Sprintf("call_%s", acc.id),
									"type":    "function_call",
									"status":  "in_progress",
									"call_id": acc.id,
									"name":    name,
								},
							})
						}
					}
					if args, ok := fn["arguments"].(string); ok && args != "" {
						acc.arguments.WriteString(args)
						writeEv("response.function_call_arguments.delta", map[string]interface{}{
							"type":         "response.function_call_arguments.delta",
							"output_index": acc.outputIdx,
							"item_id":      fmt.Sprintf("call_%s", acc.id),
							"call_id":      acc.id,
							"delta":        args,
						})
					}
				}
			}
		}
	}

	// 流读取完毕，关闭还未结束的推理块
	if reasoningAdded && !reasoningEnded {
		writeEv("response.reasoning_summary_text.done", map[string]interface{}{
			"type":          "response.reasoning_summary_text.done",
			"output_index":  reasoningIndex,
			"summary_index": 0,
			"item_id":       reasoningItemID,
			"text":          fullReasoning.String(),
		})
		writeEv("response.output_item.done", map[string]interface{}{
			"type":         "response.output_item.done",
			"output_index": reasoningIndex,
			"item": map[string]interface{}{
				"id":     reasoningItemID,
				"type":   "reasoning",
				"status": "completed",
				"summary": []map[string]interface{}{
					{"type": "summary_text", "text": fullReasoning.String()},
				},
			},
		})
	}

	var finalOutputs []interface{}

	if reasoningAdded {
		finalOutputs = append(finalOutputs, map[string]interface{}{
			"id":     reasoningItemID,
			"type":   "reasoning",
			"status": "completed",
			"summary": []map[string]interface{}{
				{"type": "summary_text", "text": fullReasoning.String()},
			},
		})
	}

	if messageAdded {
		msgContent := []map[string]interface{}{
			{"type": "output_text", "text": fullText.String(), "annotations": []interface{}{}},
		}
		writeEv("response.output_text.done", map[string]interface{}{
			"type":          "response.output_text.done",
			"item_id":       messageItemID,
			"output_index":  messageIndex,
			"content_index": 0,
			"text":          fullText.String(),
			"logprobs":      []interface{}{},
		})
		writeEv("response.content_part.done", map[string]interface{}{
			"type":          "response.content_part.done",
			"item_id":       messageItemID,
			"output_index":  messageIndex,
			"content_index": 0,
			"part": map[string]interface{}{
				"type":        "output_text",
				"text":        fullText.String(),
				"annotations": []interface{}{},
			},
		})
		writeEv("response.output_item.done", map[string]interface{}{
			"type":         "response.output_item.done",
			"output_index": messageIndex,
			"item": map[string]interface{}{
				"id":      messageItemID,
				"type":    "message",
				"status":  "completed",
				"role":    "assistant",
				"content": msgContent,
			},
		})
		finalOutputs = append(finalOutputs, map[string]interface{}{
			"id":      messageItemID,
			"type":    "message",
			"status":  "completed",
			"role":    "assistant",
			"content": msgContent,
		})
	}

	for _, acc := range toolCalls {
		if acc == nil {
			continue
		}
		writeEv("response.function_call_arguments.done", map[string]interface{}{
			"type":         "response.function_call_arguments.done",
			"output_index": acc.outputIdx,
			"item_id":      fmt.Sprintf("call_%s", acc.id),
			"call_id":      acc.id,
			"arguments":    acc.arguments.String(),
		})
		callItem := map[string]interface{}{
			"id":        fmt.Sprintf("call_%s", acc.id),
			"type":      "function_call",
			"status":    "completed",
			"call_id":   acc.id,
			"name":      acc.name,
			"arguments": acc.arguments.String(),
		}
		writeEv("response.output_item.done", map[string]interface{}{
			"type":         "response.output_item.done",
			"output_index": acc.outputIdx,
			"item":         callItem,
		})
		finalOutputs = append(finalOutputs, callItem)
	}

	// 构建最终 usage（符合 Responses API 规范）
	var inputTokens, outputTokens, reasoningTokens int
	if finalUsage != nil {
		if pt, ok := finalUsage["prompt_tokens"].(float64); ok {
			inputTokens = int(pt)
		}
		if ct, ok := finalUsage["completion_tokens"].(float64); ok {
			outputTokens = int(ct)
		}
		if details, ok := finalUsage["completion_tokens_details"].(map[string]interface{}); ok {
			if rt, ok := details["reasoning_tokens"].(float64); ok {
				reasoningTokens = int(rt)
			}
		}
	}

	usageObj := map[string]interface{}{
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
		"output_tokens_details": map[string]interface{}{
			"reasoning_tokens": reasoningTokens,
		},
		"total_tokens": inputTokens + outputTokens,
	}

	completedAt := time.Now().Unix()
	completedResp := buildResponseObj(respID, created, model, "completed", finalOutputs, usageObj)
	completedResp["completed_at"] = completedAt
	completedResp["finish_reason"] = stopReason

	writeEv("response.completed", map[string]interface{}{
		"type":     "response.completed",
		"response": completedResp,
	})

	fmt.Fprintf(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}
