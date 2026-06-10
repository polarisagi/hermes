package anthropic

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

// CountTokensResponse Anthropic count_tokens API 标准响应格式
// 参考：https://platform.claude.com/docs/en/build-with-claude/token-counting
type CountTokensResponse struct {
	InputTokens int `json:"input_tokens"`
}

// HandleCountTokens 本地拦截并处理 /v1/messages/count_tokens 请求，
// 完全不转发到后端大模型，用程序计算后直接返回。
//
// 返回格式符合 Anthropic count_tokens API 规范（Claude Code /context 命令所需）：
//
//	{"input_tokens": N}
//
// 支持中文（CJK）：通过 EstimateClaudeTokens 对中日韩字符的低估问题进行修正。
func HandleCountTokens(w http.ResponseWriter, bodyBytes []byte) {
	tokens := EstimateTokens(bodyBytes)

	resp := CountTokensResponse{InputTokens: int(tokens)}
	b, err := json.Marshal(resp)
	if err != nil {
		// EstimateTokens 不会失败到这里，保守处理
		slog.Error("count_tokens 序列化失败", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "%s", b)

	slog.Info("本地 count_tokens 完成", "input_tokens", tokens)
}
