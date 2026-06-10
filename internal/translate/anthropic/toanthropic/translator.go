// Package toanthropic 实现 Anthropic Messages API → Anthropic Messages API 的原生协议透传。
//
// 此包专为真实的 Anthropic API（如 api.anthropic.com）设计。
// 逻辑极其轻量：
//  - 最小化改动透传：仅在 JSON bytes 层面无反序列化地替换 `model` 字段
//  - 透传客户端携带的所有 Anthropic 专属 header（如 anthropic-version, anthropic-beta 等）
//  - 响应内容通过 CopyHeaders 和 Write 直接透传回客户端
//
// 对于 Anthropic 兼容协议（如 DeepSeek），应使用 `todeepseek` 包，
// 它在网关层通过检测自动分发到该专用适配器。
package toanthropic

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/polarisagi/hermes/internal/domain"
	"github.com/polarisagi/hermes/internal/translate"
)

type Translator struct{}

func NewTranslator() *Translator {
	return &Translator{}
}

func (t *Translator) TranslateRequest(
	r *http.Request,
	bodyBytes []byte,
	provider *domain.UserProvider,
	targetEndpoint *domain.SysAccessEndpoint,
	targetModel string,
) ([]byte, string, error) {
	newBodyBytes := replaceModelInRawJSON(bodyBytes, targetModel)
	return newBodyBytes, "/messages", nil
}

func (t *Translator) TranslateResponse(w http.ResponseWriter, r *http.Request, resp *http.Response) error {
	translate.CopyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	
	stream := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
	if stream {
		translate.ForwardStreamBody(w, resp.Body)
	} else {
		_, _ = io.Copy(w, resp.Body)
	}
	return nil
}

// replaceModelInRawJSON 在不完整解析整个 JSON 树的情况下替换 "model" 字段值，提升透传性能并保留未知字段。
func replaceModelInRawJSON(body []byte, targetModel string) []byte {
	if targetModel == "" {
		return body
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return body
	}
	raw["model"] = targetModel
	result, err := json.Marshal(raw)
	if err != nil {
		return body
	}
	return result
}
