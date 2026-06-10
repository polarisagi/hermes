// Package todeepseek 实现 Anthropic Messages API → DeepSeek Anthropic Messages API 的协议转换。
//
// 专门处理 DeepSeek Anthropic API 与官方 Anthropic API 的主要差异（2026年5月官方文档）：
//   - thinking.type 只支持 "enabled"/"disabled"，不支持 "adaptive"（Claude 2026 新增）
//   - budget_tokens 被忽略（DeepSeek 用 output_config.effort 控制强度）
//   - 顶层 effort 字段不被识别，需转为 output_config: {effort: "high"/"max"}
//   - redacted_thinking 不支持，历史消息中出现时需过滤
//   - image、document 等多模态 content 不支持
//   - context_management 等 Claude Code 专属字段需清理（DeepSeek 不识别会报错）
//   - anthropic-beta header 被忽略（无害）
package todeepseek

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/polarisagi/hermes/internal/domain"
	anthr "github.com/polarisagi/hermes/internal/translate/anthropic"
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
	var aReq anthr.MessageRequest
	if err := json.Unmarshal(bodyBytes, &aReq); err != nil {
		return nil, "", fmt.Errorf("invalid JSON payload: %w", err)
	}
	
	if targetModel != "" {
		aReq.Model = targetModel
	}
	
	adaptForDeepSeek(&aReq)
	
	newBodyBytes, _ := json.Marshal(aReq)
	slog.Debug("[todeepseek] 协议适配完成",
		"model", aReq.Model,
		"thinking_type", thinkingType(aReq.Thinking),
		"output_config_effort", outputConfigEffort(aReq.OutputConfig),
	)

	return newBodyBytes, "/messages", nil
}

func (t *Translator) TranslateResponse(w http.ResponseWriter, r *http.Request, resp *http.Response) error {
	stream := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")

	if stream {
		handleDeepSeekStream(w, r, resp)
	} else {
		handleDeepSeekNonStream(w, r, resp)
	}
	return nil
}
