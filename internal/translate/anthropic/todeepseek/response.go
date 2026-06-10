package todeepseek

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/polarisagi/hermes/internal/translate"
	anthr "github.com/polarisagi/hermes/internal/translate/anthropic"
)

func handleDeepSeekNonStream(w http.ResponseWriter, r *http.Request, resp *http.Response) {
	var isCompact bool
	if originalBody, ok := r.Context().Value(translate.OriginalReqBodyKey{}).([]byte); ok {
		var req anthr.MessageRequest
		_ = json.Unmarshal(originalBody, &req)
		isCompact = anthr.IsCompactRequest(&req)
	}

	body, _ := io.ReadAll(resp.Body)
	var aResp map[string]interface{}
	_ = json.Unmarshal(body, &aResp)

	if isCompact {
		if contents, ok := aResp["content"].([]interface{}); ok {
			anthr.ProcessCompactNonStream(contents)
		}
	}

	if usage, ok := aResp["usage"].(map[string]interface{}); ok {
		if it, _ := usage["input_tokens"].(float64); it == 0 {
			if originalBody, ok := r.Context().Value(translate.OriginalReqBodyKey{}).([]byte); ok {
				usage["input_tokens"] = anthr.EstimateInputTokens(originalBody)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(aResp)
}

func handleDeepSeekStream(w http.ResponseWriter, r *http.Request, resp *http.Response) {
	var isCompact bool
	var estimatedInputTokens int
	if originalBody, ok := r.Context().Value(translate.OriginalReqBodyKey{}).([]byte); ok {
		var req anthr.MessageRequest
		_ = json.Unmarshal(originalBody, &req)
		isCompact = anthr.IsCompactRequest(&req)
		estimatedInputTokens = anthr.EstimateInputTokens(originalBody)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, _ := w.(http.Flusher)
	reader := bufio.NewReader(resp.Body)

	compactManager := &anthr.CompactStreamManager{}
	var inCompactBlock bool
	var compactBlockIndex float64

	writeEv := func(eventType string, data interface{}) {
		anthr.WriteSSE(w, flusher, eventType, data)
	}

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

		eventType, _ := chunk["type"].(string)

		if eventType == "message_start" {
			anthr.FillMessageStartUsage(chunk, estimatedInputTokens)
			writeEv("message_start", chunk)
			continue
		}

		if isCompact {
			switch eventType {
			case "content_block_start":
				if cb, ok := chunk["content_block"].(map[string]interface{}); ok {
					if t, _ := cb["type"].(string); t == "text" {
						inCompactBlock = true
						compactBlockIndex, _ = chunk["index"].(float64)
						continue
					}
				}
			case "content_block_delta":
				if inCompactBlock {
					if delta, ok := chunk["delta"].(map[string]interface{}); ok {
						if text, ok := delta["text"].(string); ok {
							compactManager.BufferText(text)
							// Buffer it instead of sending
							continue
						}
					}
				}
			case "content_block_stop":
				if inCompactBlock && chunk["index"].(float64) == compactBlockIndex {
					compactManager.Flush(w, flusher, writeEv, int(compactBlockIndex))
					inCompactBlock = false
					continue
				}
			case "message_delta", "message_stop":
				if inCompactBlock {
					compactManager.Flush(w, flusher, writeEv, int(compactBlockIndex))
					inCompactBlock = false
				}
			}
		}

		// Write unhandled events directly
		writeEv(eventType, chunk)
	}

	if inCompactBlock {
		compactManager.Flush(w, flusher, writeEv, int(compactBlockIndex))
	}
}
