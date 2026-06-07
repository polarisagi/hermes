package anthropic

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func PtrInt(i int) *int { return &i }

// WriteSSE 向 HTTP 响应流写入一条 Anthropic SSE 事件
func WriteSSE(w http.ResponseWriter, flusher http.Flusher, eventType string, data interface{}) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, b)
	if flusher != nil {
		flusher.Flush()
	}
}

// WriteSSEMessageStart 发送 message_start 事件
func WriteSSEMessageStart(w http.ResponseWriter, flusher http.Flusher, traceID, modelName string, estimatedInputTokens int) {
	WriteSSE(w, flusher, "message_start", StreamEvent{
		Type: "message_start",
		Message: &MessageResponse{
			ID:      fmt.Sprintf("msg_%s", traceID),
			Type:    "message",
			Role:    "assistant",
			Content: []Content{},
			Model:   modelName,
			Usage:   Usage{InputTokens: estimatedInputTokens},
		},
	})
}

// WriteSSEContentBlockStop 发送 content_block_stop 事件
func WriteSSEContentBlockStop(w http.ResponseWriter, flusher http.Flusher, index int) {
	WriteSSE(w, flusher, "content_block_stop", StreamEvent{
		Type:  "content_block_stop",
		Index: PtrInt(index),
	})
}

// WriteSSEMessageStop 发送 message_stop 事件
func WriteSSEMessageStop(w http.ResponseWriter, flusher http.Flusher) {
	WriteSSE(w, flusher, "message_stop", StreamEvent{
		Type: "message_stop",
	})
}
