// Package togoogle 实现 Google Gemini API → Google Gemini API 的直通转换。
//
// 客户端发送 Gemini API 格式请求，路由到 Google 官方后端（generativelanguage.googleapis.com
// 或 Vertex AI aiplatform.googleapis.com）。主要职责：
//   - 替换请求体中的模型名（targetModel 覆盖客户端携带的模型）
//   - 重写 URL 路径中的模型名段（/models/{old_model}:generateContent → /models/{targetModel}:...）
//   - 透传流式响应（alt=sse 或 event-stream）
package togoogle

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/polarisagi/hermes/internal/domain"
	"github.com/polarisagi/hermes/internal/translate"
)

type GoogleToGoogleTranslator struct{}

func NewTranslator() *GoogleToGoogleTranslator {
	return &GoogleToGoogleTranslator{}
}

func (t *GoogleToGoogleTranslator) TranslateRequest(
	r *http.Request,
	bodyBytes []byte,
	provider *domain.UserProvider,
	targetEndpoint *domain.SysAccessEndpoint,
	targetModel string,
) ([]byte, string, error) {
	var gReq map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &gReq); err != nil {
		return nil, "", fmt.Errorf("invalid Gemini JSON: %w", err)
	}

	// 移除请求体中的 model 字段，因为 Vertex AI 严格校验它必须为完整路径，
	// 而原生 Google API 和 Vertex AI 均可通过 URL Path 自动推断模型。
	delete(gReq, "model")
	newBodyBytes, _ := json.Marshal(gReq)

	// 重建 URL 路径：替换路径中的模型名段
	subPath := strings.TrimPrefix(r.URL.Path, "/v1/google")
	if targetModel != "" {
		subPath = replaceModelInPath(subPath, targetModel)
	}

	// 携带 alt=sse 查询参数（流式请求）
	if strings.Contains(r.URL.RawQuery, "alt=sse") && !strings.Contains(subPath, "alt=sse") {
		if strings.Contains(subPath, "?") {
			subPath += "&alt=sse"
		} else {
			subPath += "?alt=sse"
		}
	}

	return newBodyBytes, subPath, nil
}

func (t *GoogleToGoogleTranslator) TranslateResponse(w http.ResponseWriter, r *http.Request, resp *http.Response) error {
	translate.CopyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	isStream := strings.Contains(r.URL.RawQuery, "alt=sse") ||
		strings.Contains(r.URL.Path, "stream") ||
		strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
	if isStream {
		translate.ForwardStreamBody(r.Context(), w, resp.Body)
	} else {
		_, _ = io.Copy(w, resp.Body)
	}
	return nil
}

// replaceModelInPath 将 URL 路径中 /models/{model}: 段的模型名替换为 targetModel。
//
// 典型路径格式：
//   - /v1/models/gemini-2.5-pro:generateContent
//   - /v1beta/models/gemini-2.5-flash:streamGenerateContent
//   - /models/gemini-2.5-pro-001:generateContent
//
// 策略：找到最后一个 ":" 之前的最后一个 "/" 到 ":" 之间的段即为模型名，替换之。
func replaceModelInPath(path, targetModel string) string {
	colonIdx := strings.LastIndex(path, ":")
	if colonIdx <= 0 {
		// 无 `:` 则直接追加（罕见情况）
		return path
	}
	prefix := path[:colonIdx]
	suffix := path[colonIdx:] // 包含 ':'

	slashIdx := strings.LastIndex(prefix, "/")
	if slashIdx < 0 {
		return targetModel + suffix
	}
	return prefix[:slashIdx+1] + targetModel + suffix
}
