// Package togoogle 实现 OpenAI Chat Completions API → Google Gemini 原生 API 的协议转换。
//
// 选用原生 Gemini API（而非 Gemini 的 OpenAI 兼容层），理由：
//   - Gemini OpenAI 兼容接口功能严重滞后（不支持思考参数、部分工具调用特性）
//   - 原生 Gemini API 支持 thinkingConfig.thinkingBudget、完整工具调用、token 统计等
package togoogle

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/polarisagi/hermes/internal/domain"
)

// OpenAIToGoogleTranslator 实现从 OpenAI 到 Google Gemini 的协议翻译
type OpenAIToGoogleTranslator struct{}

func NewTranslator() *OpenAIToGoogleTranslator {
	return &OpenAIToGoogleTranslator{}
}

func (t *OpenAIToGoogleTranslator) TranslateRequest(
	r *http.Request,
	bodyBytes []byte,
	provider *domain.UserProvider,
	targetEndpoint *domain.SysAccessEndpoint,
	targetModel string,
) ([]byte, string, error) {
	// 1. 解析 OpenAI 请求
	var oReq map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &oReq); err != nil {
		return nil, "", fmt.Errorf("invalid OpenAI JSON payload: %w", err)
	}

	// 2. 确定模型名
	if targetModel == "" {
		if m, ok := oReq["model"].(string); ok && m != "" {
			targetModel = m
		} else {
			targetModel = "gemini-2.5-pro" // 2026年默认旗舰模型
		}
	}

	// 3. 判断是否流式
	isStream := false
	if s, ok := oReq["stream"].(bool); ok {
		isStream = s
	}

	// 4. 转换为 Gemini 请求格式（传入模型名以区分 thinkingBudget/thinkingLevel）
	gReq := buildGeminiRequest(oReq, targetModel)
	gReqBytes, _ := json.Marshal(gReq)

	// 5. 确定目标路径
	suffix := ":generateContent"
	if isStream {
		suffix = ":streamGenerateContent"
	}
	subpath := fmt.Sprintf("/models/%s%s", targetModel, suffix)

	if isStream {
		// 流式请求需要在 URL 后面加 ?alt=sse。我们可以在网关统一处理，但既然这是一个特殊的 path 需要，
		// 我们可以把 alt=sse 作为 path 的一部分。
		subpath += "?alt=sse"
	}

	slog.Debug("→ [openai/togoogle] 翻译请求",
		"model", targetModel,
		"stream", isStream,
	)

	return gReqBytes, subpath, nil
}

func (t *OpenAIToGoogleTranslator) TranslateResponse(w http.ResponseWriter, r *http.Request, resp *http.Response) error {
	// 判断是否是流式
	// 网关并不在翻译器里判断 HTTP Status 429。只要这里被调用，通常说明网关已经获得了 200 或者需要原样返回。
	isStream := false
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") || strings.Contains(r.URL.Path, "streamGenerateContent") {
		isStream = true
	}

	// targetModel 我们通过推断获取，如果没传可以用临时替代
	targetModel := "gemini_model"

	if isStream {
		handleStream(w, resp, targetModel)
	} else {
		handleNonStream(w, resp, targetModel)
	}
	return nil
}
