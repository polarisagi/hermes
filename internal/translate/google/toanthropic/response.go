package toanthropic

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func handleNonStream(w http.ResponseWriter, resp *http.Response) {
	body, _ := io.ReadAll(resp.Body)
	var aResp map[string]interface{}
	_ = json.Unmarshal(body, &aResp)

	model, _ := aResp["model"].(string)
	var parts []map[string]interface{}
	var finishReason string
	var inputTokens, outputTokens int

	if contents, ok := aResp["content"].([]interface{}); ok {
		for _, c := range contents {
			blk, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			switch blk["type"] {
			case "text":
				text, _ := blk["text"].(string)
				parts = append(parts, map[string]interface{}{"text": text})
			case "thinking":
				thinking, _ := blk["thinking"].(string)
				sig, _ := blk["signature"].(string)
				tp := map[string]interface{}{
					"thought": true,
					"text":    thinking,
				}
				if sig != "" {
					tp["thoughtSignature"] = sig
				}
				parts = append(parts, tp)
			case "tool_use":
				id, _ := blk["id"].(string)
				name, _ := blk["name"].(string)
				parts = append(parts, map[string]interface{}{
					"functionCall": map[string]interface{}{
						"name": name,
						"args": blk["input"],
					},
					"_tool_use_id": id, // 内部字段，流式时用于 functionResponse 匹配
				})
			}
		}
	}

	if sr, ok := aResp["stop_reason"].(string); ok {
		finishReason = mapStopReasonToFinishReason(sr)
	}
	if usage, ok := aResp["usage"].(map[string]interface{}); ok {
		if it, ok := usage["input_tokens"].(float64); ok {
			inputTokens = int(it)
		}
		if ot, ok := usage["output_tokens"].(float64); ok {
			outputTokens = int(ot)
		}
	}

	if len(parts) == 0 {
		parts = append(parts, map[string]interface{}{"text": ""})
	}
	// 清理内部字段
	for _, p := range parts {
		delete(p, "_tool_use_id")
	}

	gResp := map[string]interface{}{
		"candidates": []map[string]interface{}{
			{
				"content": map[string]interface{}{
					"role":  "model",
					"parts": parts,
				},
				"finishReason": finishReason,
				"index":        0,
			},
		},
		"usageMetadata": map[string]interface{}{
			"promptTokenCount":     inputTokens,
			"candidatesTokenCount": outputTokens,
			"totalTokenCount":      inputTokens + outputTokens,
		},
		"modelVersion": model,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(gResp)
}

func handleStream(w http.ResponseWriter, r *http.Request, resp *http.Response) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, _ := w.(http.Flusher)
	reader := bufio.NewReader(resp.Body)

	writeChunk := func(data interface{}) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}

	var model string
	var inputTokens, outputTokens int
	finishReason := ""
	// 流式工具调用累积：Anthropic 发送 input_json_delta 增量，需拼接后完整发射
	type toolAccum struct {
		name     string
		jsonBuf  string
		index    int
	}
	var curTool *toolAccum
	toolIndex := 0

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
				if m, ok := msg["model"].(string); ok && m != "" {
					model = m
				}
				if usage, ok := msg["usage"].(map[string]interface{}); ok {
					if it, ok := usage["input_tokens"].(float64); ok {
						inputTokens = int(it)
					}
				}
			}

		case "content_block_start":
			blk, _ := event["content_block"].(map[string]interface{})
			if blk == nil {
				continue
			}
			blkType, _ := blk["type"].(string)
			switch blkType {
			case "thinking":
				writeChunk(buildGeminiChunk(model, []map[string]interface{}{
					{"thought": true, "text": ""},
				}, ""))
			case "tool_use":
				curTool = &toolAccum{
					name: blk["name"].(string),
					index: toolIndex,
				}
				toolIndex++
			}

		case "content_block_stop":
			// 工具调用结束时发送累积的完整参数
			if curTool != nil && curTool.jsonBuf != "" {
				var args interface{}
				if err := json.Unmarshal([]byte(curTool.jsonBuf), &args); err == nil {
					writeChunk(buildGeminiChunk(model, []map[string]interface{}{
						{"functionCall": map[string]interface{}{"name": curTool.name, "args": args}},
					}, ""))
				}
			}
			curTool = nil

		case "content_block_delta":
			delta, ok := event["delta"].(map[string]interface{})
			if !ok {
				continue
			}
			deltaType, _ := delta["type"].(string)
			switch deltaType {
			case "text_delta":
				text, _ := delta["text"].(string)
				writeChunk(buildGeminiChunk(model, []map[string]interface{}{
					{"text": text},
				}, ""))
			case "thinking_delta":
				thinking, _ := delta["thinking"].(string)
				writeChunk(buildGeminiChunk(model, []map[string]interface{}{
					{"thought": true, "text": thinking},
				}, ""))
			case "signature_delta":
				sig, _ := delta["signature"].(string)
				writeChunk(buildGeminiChunk(model, []map[string]interface{}{
					{"thought": true, "text": "", "thoughtSignature": sig},
				}, ""))
			case "input_json_delta":
				partial, _ := delta["partial_json"].(string)
				if curTool != nil {
					curTool.jsonBuf += partial
				}
			}

		case "message_delta":
			delta, _ := event["delta"].(map[string]interface{})
			if delta != nil {
				if sr, ok := delta["stop_reason"].(string); ok {
					finishReason = mapStopReasonToFinishReason(sr)
				}
			}
			if usage, ok := event["usage"].(map[string]interface{}); ok {
				if ot, ok := usage["output_tokens"].(float64); ok {
					outputTokens = int(ot)
				}
			}
		}
	}

	// 最终使用量 chunk
	writeChunk(map[string]interface{}{
		"candidates": []map[string]interface{}{
			{
				"content":      map[string]interface{}{"role": "model", "parts": []map[string]interface{}{}},
				"finishReason": finishReason,
				"index":        0,
			},
		},
		"usageMetadata": map[string]interface{}{
			"promptTokenCount":     inputTokens,
			"candidatesTokenCount": outputTokens,
			"totalTokenCount":      inputTokens + outputTokens,
		},
		"modelVersion": model,
	})
}

func buildGeminiChunk(model string, parts []map[string]interface{}, finishReason string) map[string]interface{} {
	cand := map[string]interface{}{
		"content": map[string]interface{}{
			"role":  "model",
			"parts": parts,
		},
		"index": 0,
	}
	if finishReason != "" {
		cand["finishReason"] = finishReason
	}
	return map[string]interface{}{
		"candidates":   []map[string]interface{}{cand},
		"modelVersion": model,
	}
}

func mapStopReasonToFinishReason(stopReason string) string {
	switch stopReason {
	case "end_turn":
		return "STOP"
	case "max_tokens":
		return "MAX_TOKENS"
	case "tool_use":
		return "TOOL_CODE"
	case "stop_sequence":
		return "STOP"
	default:
		return "OTHER"
	}
}
