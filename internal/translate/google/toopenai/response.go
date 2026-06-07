package toopenai

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/polarisagi/hermes/internal/translate"
)

// ── 响应转换：OpenAI → Gemini 格式 ─────────────────────────────────────────

func handleNonStream(w http.ResponseWriter, resp *http.Response, kind translate.BackendKind) {
	body, _ := io.ReadAll(resp.Body)
	var oResp map[string]interface{}
	_ = json.Unmarshal(body, &oResp)

	model, _ := oResp["model"].(string)
	var parts []map[string]interface{}
	finishReason := "STOP"
	var promptTokens, completionTokens, reasoningTokens int

	if choices, ok := oResp["choices"].([]interface{}); ok && len(choices) > 0 {
		choice, _ := choices[0].(map[string]interface{})
		if choice == nil {
			choice = map[string]interface{}{}
		}

		if msg, ok := choice["message"].(map[string]interface{}); ok {
			// reasoning_content → thought=true part（所有后端都可能返回，包括 DeepSeek、OpenAI GPT-5.x）
			if rc, ok := msg["reasoning_content"].(string); ok && rc != "" {
				parts = append(parts, map[string]interface{}{
					"thought": true,
					"text":    rc,
				})
			}

			// content → text part
			if c, ok := msg["content"].(string); ok && c != "" {
				parts = append(parts, map[string]interface{}{"text": c})
			}

			// tool_calls → functionCall parts
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
					var args interface{}
					_ = json.Unmarshal([]byte(argsStr), &args)
					if args == nil {
						args = map[string]interface{}{}
					}
					parts = append(parts, map[string]interface{}{
						"functionCall": map[string]interface{}{
							"name": name,
							"args": args,
						},
					})
				}
			}
		}

		if fr, ok := choice["finish_reason"].(string); ok {
			finishReason = mapFinishReason(fr)
		}
	}

	if usage, ok := oResp["usage"].(map[string]interface{}); ok {
		if pt, ok := usage["prompt_tokens"].(float64); ok {
			promptTokens = int(pt)
		}
		if ct, ok := usage["completion_tokens"].(float64); ok {
			completionTokens = int(ct)
		}
		// completion_tokens_details.reasoning_tokens
		if ctd, ok := usage["completion_tokens_details"].(map[string]interface{}); ok {
			if rt, ok := ctd["reasoning_tokens"].(float64); ok {
				reasoningTokens = int(rt)
			}
		}
	}

	if len(parts) == 0 {
		parts = append(parts, map[string]interface{}{"text": ""})
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
			"promptTokenCount":     promptTokens,
			"candidatesTokenCount": completionTokens,
			"totalTokenCount":      promptTokens + completionTokens,
			"thoughtsTokenCount":   reasoningTokens,
		},
		"modelVersion": model,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(gResp)
}

func handleStream(w http.ResponseWriter, resp *http.Response, kind translate.BackendKind) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, _ := w.(http.Flusher)
	reader := bufio.NewReader(resp.Body)

	var model string
	var promptTokens, completionTokens, reasoningTokens int
	finishReason := ""

	writeChunk := func(data interface{}) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}

	// 工具调用累积（OpenAI 流式工具调用分片下发）
	type toolCallAcc struct {
		id   string
		name string
		args strings.Builder
	}
	toolCalls := map[int]*toolCallAcc{}

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

		if m, ok := chunk["model"].(string); ok && m != "" && model == "" {
			model = m
		}

		// usage（stream_options: {include_usage: true} 时出现在最后一个 chunk）
		if usage, ok := chunk["usage"].(map[string]interface{}); ok {
			if pt, ok := usage["prompt_tokens"].(float64); ok {
				promptTokens = int(pt)
			}
			if ct, ok := usage["completion_tokens"].(float64); ok {
				completionTokens = int(ct)
			}
			if ctd, ok := usage["completion_tokens_details"].(map[string]interface{}); ok {
				if rt, ok := ctd["reasoning_tokens"].(float64); ok {
					reasoningTokens = int(rt)
				}
			}
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
			finishReason = mapFinishReason(fr)
		}

		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			continue
		}

		// reasoning_content → thought=true part（所有后端都可能返回，包括 DeepSeek、OpenAI GPT-5.x）
		if rc, ok := delta["reasoning_content"].(string); ok && rc != "" {
			writeChunk(buildGeminiStreamChunk(model, []map[string]interface{}{
				{"thought": true, "text": rc},
			}, ""))
			continue
		}

		// content → text part
		if content, ok := delta["content"].(string); ok && content != "" {
			writeChunk(buildGeminiStreamChunk(model, []map[string]interface{}{
				{"text": content},
			}, ""))
		}

		// tool_calls → functionCall parts
		if tcsRaw, ok := delta["tool_calls"].([]interface{}); ok && len(tcsRaw) > 0 {
			for _, tcRaw := range tcsRaw {
				tc, ok := tcRaw.(map[string]interface{})
				if !ok {
					continue
				}
				idxF, _ := tc["index"].(float64)
				idx := int(idxF)
				if toolCalls[idx] == nil {
					toolCalls[idx] = &toolCallAcc{}
				}
				acc := toolCalls[idx]
				if id, ok := tc["id"].(string); ok && id != "" {
					acc.id = id
				}
				if fn, ok := tc["function"].(map[string]interface{}); ok {
					if name, ok := fn["name"].(string); ok && name != "" {
						acc.name = name
					}
					if args, ok := fn["arguments"].(string); ok {
						acc.args.WriteString(args)
					}
				}
			}
		}
	}

	// 流结束：输出累积的工具调用
	for _, acc := range toolCalls {
		if acc.name == "" {
			continue
		}
		var args interface{}
		_ = json.Unmarshal([]byte(acc.args.String()), &args)
		if args == nil {
			args = map[string]interface{}{}
		}
		writeChunk(buildGeminiStreamChunk(model, []map[string]interface{}{
			{"functionCall": map[string]interface{}{"name": acc.name, "args": args}},
		}, ""))
	}

	// 最终使用量 + finish
	writeChunk(map[string]interface{}{
		"candidates": []map[string]interface{}{
			{
				"content":      map[string]interface{}{"role": "model", "parts": []interface{}{}},
				"finishReason": finishReason,
				"index":        0,
			},
		},
		"usageMetadata": map[string]interface{}{
			"promptTokenCount":     promptTokens,
			"candidatesTokenCount": completionTokens,
			"totalTokenCount":      promptTokens + completionTokens,
			"thoughtsTokenCount":   reasoningTokens,
		},
		"modelVersion": model,
	})
}

func buildGeminiStreamChunk(model string, parts []map[string]interface{}, finishReason string) map[string]interface{} {
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

func mapFinishReason(fr string) string {
	switch fr {
	case "stop":
		return "STOP"
	case "length":
		return "MAX_TOKENS"
	case "tool_calls":
		return "TOOL_CODE"
	case "content_filter":
		return "SAFETY"
	default:
		return "OTHER"
	}
}
