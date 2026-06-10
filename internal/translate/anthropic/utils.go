package anthropic

import (
)

// EstimateInputTokens 从请求体字节估算输入 Token 数，用于流式首事件填充，支撑 /context 命令
func EstimateInputTokens(reqBody []byte) int {
	if len(reqBody) == 0 {
		return 0
	}
	return int(EstimateTokens(reqBody))
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
