package togoogle

// mapFinishReason 将 Gemini finishReason 映射到 OpenAI finish_reason
func mapFinishReason(fr string) string {
	switch fr {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY", "RECITATION", "PROHIBITED_CONTENT", "SPII":
		return "content_filter"
	case "FUNCTION_CALL":
		return "tool_calls"
	case "MALFORMED_FUNCTION_CALL":
		return "tool_calls" // 尽量不中断，让客户端处理
	default:
		return "stop"
	}
}
