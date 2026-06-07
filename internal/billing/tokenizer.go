package billing

import (
	"log/slog"
	"sync"

	"github.com/pkoukk/tiktoken-go"
)

var (
	tke     *tiktoken.Tiktoken
	tkeOnce sync.Once
)

func init() {
	// 异步预加载 tiktoken 字典，避免首次请求时的加载延迟
	go initEnc()
}

func initEnc() *tiktoken.Tiktoken {
	tkeOnce.Do(func() {
		var err error
		// 使用 o200k_base (GPT-4o 分词器)，更精确
		tke, err = tiktoken.GetEncoding("o200k_base")
		if err != nil {
			slog.Error("初始化 tiktoken 失败", "error", err)
		}
	})
	return tke
}

// EstimatePromptTokens 从请求体字节估算 Token，如果 encoding 成功，使用精确计算
func EstimatePromptTokens(bodyBytes []byte) int64 {
	tk := initEnc()
	if tk != nil {
		ids := tk.Encode(string(bodyBytes), nil, nil)
		return int64(len(ids))
	}
	// Fallback 启发式：1 token ≈ 4 bytes
	return int64(len(bodyBytes)) / 4
}

// EstimateCompletionTokens 从生成的文本估算 Token
func EstimateCompletionTokens(text string) int64 {
	tk := initEnc()
	if tk != nil {
		ids := tk.Encode(text, nil, nil)
		return int64(len(ids))
	}
	// Fallback 启发式：1 token ≈ 4 bytes
	return int64(len(text)) / 4
}
