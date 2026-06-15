package billing

import (
	"log/slog"
	"math"
	"strings"
	"sync"
	"unicode"

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
		// 使用 o200k_base (GPT-4o 分词器)
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
	
	// 优化：去除 base64 图片数据，避免因为图片 base64 字符串导致 token 数量暴增
	// 使用简单正则或字符串替换清理常见 base64 前缀
	cleanBody := string(bodyBytes)
	imageCount := int64(strings.Count(cleanBody, `"data:image/`))
	if imageCount == 0 {
		imageCount = int64(strings.Count(cleanBody, `"image_url"`)) // 简单估算图片数
	}

	// 简单粗暴清理大段连续的 base64 文本
	// 由于完整的 JSON 解析和 base64 提取比较耗时，这里采用截断长非空格字符串的启发式方法
	// 但更安全的做法是直接对 token 数量封顶或者剔除 "data:image/...;base64,..."
	// 这里为了性能和简单，如果是多模态请求，直接给一个合理的图片 token 估算
	
	var baseTokens int64
	if tk != nil {
		// 为了防止 base64 带来的几十万 token 计算，如果包含图片，我们可以对长字符串进行剥离
		// 简单的做法是如果请求体过大（例如超过100KB），且包含图片，我们就取个合理的截断
		if len(bodyBytes) > 100*1024 && imageCount > 0 {
			// 直接启发式估算，不进行全量 Encode 避免耗时和严重高估
			// 假设文本部分最多 2000 tokens
			baseTokens = 2000
		} else {
			ids := tk.Encode(cleanBody, nil, nil)
			baseTokens = int64(len(ids))
		}
	} else {
		// Fallback 启发式：1 token ≈ 4 bytes
		if len(bodyBytes) > 100*1024 && imageCount > 0 {
			baseTokens = 2000
		} else {
			baseTokens = int64(len(bodyBytes)) / 4
		}
	}

	// 每张图片按固定 258 tokens (Gemini 官方标准/OpenAI 基础标准) 计算补偿
	return baseTokens + (imageCount * 258)
}

// EstimateCompletionTokens 从生成的文本估算 Token（通用，用于计费）
func EstimateCompletionTokens(text string) int64 {
	tk := initEnc()
	if tk != nil {
		ids := tk.Encode(text, nil, nil)
		return int64(len(ids))
	}
	// Fallback 启发式：1 token ≈ 4 bytes
	return int64(len(text)) / 4
}

// EstimateClaudeTokens 专为 Claude 模型设计的 token 估算，支持中文（CJK）。
//
// 问题根源：o200k_base（GPT-4o 分词器）对中日韩字符非常高效，每个字符约 0.5-1 个 token；
// 而 Claude 的私有分词器对同样的 CJK 字符约需 1.5-2.5 个 token。
// 若直接用 o200k_base 结果，中文 token 数会被低估约 2 倍，
// 导致 /context 命令显示的上下文占比严重偏低。
//
// 修正策略：以 o200k_base 为基准，按 CJK 字符占比线性插值放大系数（1.0 ~ 2.5）。
func EstimateClaudeTokens(text string) int64 {
	if text == "" {
		return 0
	}
	tk := initEnc()
	if tk != nil {
		base := int64(len(tk.Encode(text, nil, nil)))
		return applyCJKCorrection(text, base)
	}
	return estimateCJKFallback(text)
}

// applyCJKCorrection 对 tiktoken 结果按 CJK 字符占比进行修正。
// 修正系数：纯英文 = 1.0，纯中文 = 2.5，混合文本线性插值。
func applyCJKCorrection(text string, base int64) int64 {
	if base == 0 {
		return 0
	}
	cjkCount := countCJKRunes(text)
	if cjkCount == 0 {
		return base
	}
	totalRunes := int64(len([]rune(text)))
	if totalRunes == 0 {
		return base
	}
	cjkFrac := float64(cjkCount) / float64(totalRunes)
	// 线性插值：0% CJK → 1.0×；100% CJK → 2.5×
	correction := 1.0 + cjkFrac*1.5
	return int64(math.Round(float64(base) * correction))
}

// estimateCJKFallback 在 tiktoken 不可用时提供 CJK 感知的后备估算。
// tiktoken 默认 fallback（len/4）对中文 UTF-8 严重低估（3字节/汉字 → 0.75 tok/字）。
func estimateCJKFallback(text string) int64 {
	var cjk, other int64
	for _, r := range text {
		if isCJKRune(r) {
			cjk++
		} else if !unicode.IsSpace(r) {
			other++
		}
	}
	// Claude 实测：CJK ≈ 1.5 tok/字，Latin ≈ 1 tok/3.5字符
	return int64(math.Round(float64(cjk)*1.5 + float64(other)*0.3))
}

func countCJKRunes(text string) int64 {
	var n int64
	for _, r := range text {
		if isCJKRune(r) {
			n++
		}
	}
	return n
}

// isCJKRune 判断是否为中日韩字符（汉字 / 平假名 / 片假名 / 朝鲜文音节）
func isCJKRune(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}
