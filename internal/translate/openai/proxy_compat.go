package openai

import (
	"encoding/json"
	"net/http"
	"strings"
)

// writeOpenAIError 写入符合 OpenAI 2026 标准错误格式的响应
// 格式：{"error":{"message":"...","type":"...","param":null,"code":null}}
func writeOpenAIError(w http.ResponseWriter, statusCode int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	b, _ := json.Marshal(map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    errType,
			"param":   nil,
			"code":    nil,
		},
	})
	_, _ = w.Write(b)
}

// HandleClientProbes 拦截并处理客户端（如 Codex）特有的探测与预检请求。
// 如果返回 true，说明请求已被拦截处理，调用方应直接 return。
func HandleClientProbes(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}

	path := r.URL.Path

	// 1. 模拟 /models 接口
	if strings.HasSuffix(path, "/models") {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object": "list", "data": [{"id": "gpt-4o", "object": "model", "created": 1687882411, "owned_by": "openai"}]}`))
		return true
	}

	// 2. 拒绝 WebSockets
	if strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
		writeOpenAIError(w, http.StatusNotImplemented, "invalid_request_error", "WebSockets are not supported by this proxy")
		return true
	}

	// 3. 拒绝核心操作的 GET 请求，强制 fallback 到 POST
	if strings.Contains(path, "/responses") || strings.Contains(path, "/chat/completions") {
		writeOpenAIError(w, http.StatusNotFound, "invalid_request_error", "Endpoint does not support GET requests")
		return true
	}

	// 4. 其他未知 GET 请求，安全兜底返回 200 OK
	w.WriteHeader(http.StatusOK)
	return true
}
