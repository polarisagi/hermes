package anthropic

import (
	"encoding/json"
	"strings"

	"github.com/polarisagi/hermes/internal/billing"
)

// IsCompactRequest 判断当前请求是否为 Claude Code 发出的 /compact 上下文截断请求。
// 客户端会在此时清理旧思考并让模型总结历史。
func IsCompactRequest(req *MessageRequest) bool {
	if req == nil || len(req.Messages) == 0 {
		return false
	}

	lastMsg := req.Messages[len(req.Messages)-1]
	if lastMsg.Role != "user" {
		return false
	}

	lastMsgBytes, err := json.Marshal(lastMsg.Content)
	if err != nil {
		return false
	}
	lastMsgStr := string(lastMsgBytes)
	
	features := 0
	if strings.Contains(lastMsgStr, "TEXT ONLY") {
		features++
	}
	if strings.Contains(strings.ToLower(lastMsgStr), "summary") {
		features++
	}
	if strings.Contains(lastMsgStr, "Do NOT call any tools") {
		features++
	}
	if strings.Contains(lastMsgStr, "<analysis>") {
		features++
	}
	if strings.Contains(lastMsgStr, "<summary>") {
		features++
	}
	if req.ContextManagement != nil {
		for _, edit := range req.ContextManagement.Edits {
			if strings.HasPrefix(edit.Type, "clear_thinking_") {
				features++
			}
			if strings.HasPrefix(edit.Type, "compact_") {
				features++
			}
		}
	}

	return features >= 2
}

// EstimateInputTokens 从请求体字节估算输入 Token 数，用于流式首事件填充，支撑 /context 命令
func EstimateInputTokens(reqBody []byte) int {
	if len(reqBody) == 0 {
		return 0
	}
	return int(billing.EstimatePromptTokens(reqBody))
}

// WrapCompactText 用于 /compact 请求：如果生成的文本缺失 <summary>，自动补全外层结构
func WrapCompactText(text string) string {
	if !strings.Contains(text, "<summary>") {
		return "<analysis>\nGateway manually wrapped this context compaction.\n</analysis>\n<summary>\n" + strings.TrimSpace(text) + "\n</summary>"
	}
	return text
}

// FillMessageStartUsage 在 message_start 事件中填入预估的输入 Token，支持 /context 命令进度展示
func FillMessageStartUsage(chunk map[string]interface{}, estimatedTokens int) {
	if estimatedTokens <= 0 {
		return
	}
	if msg, ok := chunk["message"].(map[string]interface{}); ok {
		if usage, ok := msg["usage"].(map[string]interface{}); ok {
			if it, _ := usage["input_tokens"].(float64); it == 0 {
				usage["input_tokens"] = estimatedTokens
			}
		}
	}
}
