package togoogle

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/polarisagi/hermes/internal/domain"
	anthr "github.com/polarisagi/hermes/internal/translate/anthropic"
)

// handleAnthropicNonStreamResponse 处理 Google Agent Platform 非流式响应，提取文本和用量，转为 Anthropic JSON 格式返回
// isCompact=true 时将文本块转换为 compaction 内容块（Claude Code /compact 协议）
func handleAnthropicNonStreamResponse(w http.ResponseWriter, vertexResp *http.Response, req MessageRequest, traceID string, provider *domain.UserProvider, clientType, modelName string, reqBody []byte, isCompact bool) {
	defer vertexResp.Body.Close()
	bodyBytes, err := io.ReadAll(vertexResp.Body)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"api_error","message":"Failed to read response"}}`))
		return
	}

	var vResp map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &vResp); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"api_error","message":"Invalid response from upstream"}}`))
		return
	}

	var promptTokens, completionTokens, thinkingTokens, cachedTokens int
	if usage, ok := vResp["usageMetadata"].(map[string]interface{}); ok {
		if p, ok := usage["promptTokenCount"].(float64); ok {
			promptTokens = int(p)
		}
		if tool, ok := usage["toolUsePromptTokenCount"].(float64); ok {
			promptTokens += int(tool) // 工具定义 token 属于 prompt 端消耗
		}
		if c, ok := usage["candidatesTokenCount"].(float64); ok {
			completionTokens = int(c)
		}
		if thoughts, ok := usage["thoughtsTokenCount"].(float64); ok {
			thinkingTokens = int(thoughts)
			completionTokens += thinkingTokens // 思考 token 并入 output_tokens（Anthropic 计费方式）
		}
		if cache, ok := usage["cachedContentTokenCount"].(float64); ok {
			cachedTokens = int(cache)
		}
	}

	// Detect promptFeedback block (safety refusal before any candidates)
	// Gemini 可能返回 promptFeedback 且 blockReason 为空（静默拒绝），同样需要拦截
	if pf, ok := vResp["promptFeedback"].(map[string]interface{}); ok {
		if blockReason, ok := pf["blockReason"].(string); ok && blockReason != "" {
			slog.Warn("⚠️ [NonStream] GEAP 提示被阻断", "trace_id", traceID, "block_reason", blockReason)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"type": "error",
				"error": map[string]interface{}{
					"type":    "api_error",
					"message": fmt.Sprintf("request blocked by GEAP safety filter: %s", blockReason),
				},
			})
			return
		}
		if _, hasBlock := pf["blockReason"]; hasBlock {
			slog.Warn("⚠️ [NonStream] GEAP 提示被静默阻断 (blockReason 为空)", "trace_id", traceID)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"type": "error",
				"error": map[string]interface{}{
					"type":    "api_error",
					"message": "request blocked by GEAP safety filter (silent refusal)",
				},
			})
			return
		}
	}

	contents := []Content{}
	stopReason := "end_turn"

	if candidates, ok := vResp["candidates"].([]interface{}); ok && len(candidates) > 0 {
		if cand, ok := candidates[0].(map[string]interface{}); ok {
			// 检查 safetyRatings，非 NEGLIGIBLE 表示触发安全过滤器
			if safetyRatings, ok := cand["safetyRatings"].([]interface{}); ok {
				for _, sr := range safetyRatings {
					if srm, ok := sr.(map[string]interface{}); ok {
						isBlocked, _ := srm["blocked"].(bool)
						if isBlocked {
							cat, _ := srm["category"].(string)
							prob, _ := srm["probability"].(string)
							slog.Warn("⚠️ [NonStream] GEAP 内容被安全阻断", "trace_id", traceID, "category", cat, "probability", prob)
							w.Header().Set("Content-Type", "application/json")
							w.WriteHeader(http.StatusBadGateway)
							_ = json.NewEncoder(w).Encode(map[string]interface{}{
								"type": "error",
								"error": map[string]interface{}{
									"type":    "api_error",
									"message": fmt.Sprintf("content blocked by GEAP safety filter: category=%s probability=%s", cat, prob),
								},
							})
							return
						}
					}
				}
			}
			// finishReason 映射与流式分支保持一致，避免客户端 stop_reason 检查不一致
			if finishReason, ok := cand["finishReason"].(string); ok && finishReason != "" {
				switch finishReason {
				case "MAX_TOKENS":
					stopReason = "max_tokens"
				case "STOP":
					if len(req.StopSequences) > 0 {
						stopReason = "stop_sequence"
					}
				case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII",
					"IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT", "IMAGE_RECITATION", "IMAGE_OTHER",
					"NO_IMAGE", "OTHER":
					stopReason = "end_turn"
					slog.Warn("⚠️ [NonStream] GEAP 非正常停止原因", "trace_id", traceID, "finish_reason", finishReason)
				case "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL":
					stopReason = "end_turn"
					slog.Warn("⚠️ [NonStream] GEAP 工具调用格式异常", "trace_id", traceID, "finish_reason", finishReason)

					// 尝试挽救因模型输出格式错误而被拦截的正文内容
					if fm, ok := cand["finishMessage"].(string); ok && fm != "" {
						text := fm
						if strings.HasPrefix(fm, "Malformed function call: ") {
							text = strings.TrimPrefix(fm, "Malformed function call: ")
						}
						if isCompact {
							contents = append(contents, Content{
								Type:    "compaction",
								Content: text,
							})
						} else {
							contents = append(contents, Content{
								Type: "text",
								Text: text,
							})
						}
					}
				}
			}
			if content, ok := cand["content"].(map[string]interface{}); ok {
				if parts, ok := content["parts"].([]interface{}); ok && len(parts) > 0 {
					toolIdx := 0
					var lastSig string
					for _, partIntf := range parts {
						part, _ := partIntf.(map[string]interface{})
						// Gemini thought 部分 → Anthropic thinking 内容块
						// 无论客户端是否显式设置 thinking，只要 Gemini 返回 thought part 就转换
						// thoughtSignature 对应 Anthropic thinking.signature，客户端下一轮须原样传回
						if isThought, _ := part["thought"].(bool); isThought {
							thinkText, _ := part["text"].(string)
							sig, _ := part["thoughtSignature"].(string)
							if sig != "" {
								lastSig = sig
							}
							if thinkText != "" {
								if isCompact {
									// /compact 模式：思考内容合并为普通文本参与摘要，丢弃 signature
									// 压缩后旧对话全部丢弃，无需回传 thoughtSignature
									contents = append(contents, Content{
										Type: "text",
										Text: thinkText,
									})
								} else {
									// Gemini 未返回 thoughtSignature 时用 bypass 值填充：
									// 确保 Claude Code 存入历史的 thinking 块始终有非空签名，
									// 下一轮回传时 mapper 能正确带回合法的 thoughtSignature。
									effectiveSig := sig
									if effectiveSig == "" {
										effectiveSig = skipThoughtSigValidator
										slog.Warn("⚠️ [NonStream] thinking 块缺少 thoughtSignature，使用 bypass 值",
											"trace_id", traceID,
										)
									}
									contents = append(contents, Content{
										Type:      "thinking",
										Thinking:  thinkText,
										Signature: effectiveSig,
									})
								}
							}
							continue
						}
						if t, ok := part["text"].(string); ok {
							re := regexp.MustCompile(`(?s)^\[Assistant called tool '([^']+)' with arguments: (.*)\]\n?$|(?s)^<past_tool_execution name="([^"]+)">\n?(.*?)\n?</past_tool_execution>\n?$`)
							matches := re.FindStringSubmatch(strings.TrimSpace(t))
							if len(matches) > 0 {
								name := matches[1]
								argsStr := matches[2]
								if name == "" {
									name = matches[3]
									argsStr = matches[4]
								}
								var args map[string]interface{}
								if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
									args = make(map[string]interface{})
								}
								toolID := fmt.Sprintf("toolu_%s_%d", traceID, toolIdx)
								contents = append(contents, Content{
									Type:  "tool_use",
									ID:    toolID,
									Name:  name,
									Input: args,
								})
								stopReason = "tool_use"
								toolIdx++
								continue
							}

							if isCompact {
								// /compact 请求：将文本转为 compaction 内容块
								// Anthropic 协议要求响应含 compaction 块才能触发真正的上下文截断
								contents = append(contents, Content{
									Type:    "compaction",
									Content: t,
								})
							} else {
								contents = append(contents, Content{
									Type: "text",
									Text: t,
								})
							}
						}
						if fc, ok := part["functionCall"].(map[string]interface{}); ok {
							name, _ := fc["name"].(string)
							if name == "" {
								slog.Warn("⚠️ [NonStream] functionCall 缺少 name 字段，跳过", "trace_id", traceID)
								continue
							}
							// 读取 Gemini 返回的原生调用 ID（Gemini 2.5+/3.x 必须在 functionResponse 中回传）
							geminiCallID, _ := fc["id"].(string)

							argsBytes := normalizeFunctionCallArgs(fc["args"])
							var args map[string]interface{}
							if err := json.Unmarshal(argsBytes, &args); err != nil {
								args = make(map[string]interface{})
							}
							toolID := fmt.Sprintf("toolu_%s_%d", traceID, toolIdx)
							// 将 Gemini 原生调用 ID 编码到 toolID 中
							if geminiCallID != "" {
								toolID = fmt.Sprintf("%s_fcid_%s", toolID, geminiCallID)
							}
							// Gemini 3.x 在 functionCall part 携带 thoughtSignature
							// 仅存入内存缓存，不拼入 toolID（sig 超长会破坏协议）
							// 服务重启后缓存失效时以 skipThoughtSigValidator 兜底
							if sig, ok := part["thoughtSignature"].(string); ok && sig != "" {
								toolThoughtSigCache.Store(toolID, sig)
							} else if lastSig != "" {
								toolThoughtSigCache.Store(toolID, lastSig)
							}
							// 记录 Gemini 原生调用 ID 到全局缓存
							if geminiCallID != "" {
								toolGeminiCallIDCache.Store(toolID, geminiCallID)
							}
							slog.Info("🔧 [NonStream] Gemini functionCall 检测",
								"trace_id", traceID,
								"tool_name", name,
								"tool_id", toolID,
								"gemini_call_id", geminiCallID,
								"tool_idx", toolIdx,
							)
							contents = append(contents, Content{
								Type:  "tool_use",
								ID:    toolID,
								Name:  name,
								Input: args,
							})
							stopReason = "tool_use"
							toolIdx++
						}
					}
				}
			}
		}
	}

	// 判断是否存在真正有意义的内容块（使用公共辅助函数，支持 text/tool_use/thinking/compaction）
	hasRealContent := anthr.HasRealContent(contents)
	if isCompact && hasRealContent {
		anthr.ProcessCompactNonStream(contents)
	}

	if !hasRealContent {
		if stopReason == "max_tokens" {
			// MAX_TOKENS + 无内容：thinking 消耗了全部 maxOutputTokens，正文为空。
			// 返回合法的 max_tokens 响应而非 overloaded_error——
			// 重试无法改变 token 上限，触发重试只会浪费配额。
			// 客户端收到 stop_reason=max_tokens 后可选择扩大 max_tokens 再试。
			slog.Warn("⚠️ [NonStream] GEAP MAX_TOKENS 时内容为空（thinking 耗尽 token 预算）",
				"trace_id", traceID,
				"prompt_tokens", promptTokens)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(MessageResponse{
				ID:         fmt.Sprintf("msg_%s", traceID),
				Type:       "message",
				Role:       "assistant",
				Model:      modelName,
				StopReason: "max_tokens",
				Content:    []Content{},
				Usage: Usage{
					InputTokens:          promptTokens,
					OutputTokens:         completionTokens,
					CacheReadInputTokens: cachedTokens,
				},
			})
			return
		}

		// 其他空响应情况（如 STOP + 空 text）→ 返回 overloaded_error 触发自动重试
		slog.Warn("⚠️ [NonStream] GEAP 返回空响应，上游未生成任何内容块",
			"trace_id", traceID,
			"geap_resp_preview", string(bodyBytes[:min(len(bodyBytes), 500)]))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    "overloaded_error",
				"message": "Upstream model returned empty response — triggering automatic retry",
			},
		})
		return
	}

	anthropicResp := MessageResponse{
		ID:           fmt.Sprintf("msg_%s", traceID),
		Type:         "message",
		Role:         "assistant",
		Model:        modelName,
		StopReason:   stopReason,
		StopSequence: "",
		Usage: Usage{
			InputTokens:          promptTokens,
			OutputTokens:         completionTokens,
			CacheReadInputTokens: cachedTokens,
			OutputTokensDetails:  buildOutputTokensDetails(thinkingTokens),
		},
		Content: contents,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(vertexResp.StatusCode)
	_ = json.NewEncoder(w).Encode(anthropicResp)
}

// buildOutputTokensDetails 当思考 token 数 > 0 时返回 Anthropic 2025-2026 标准的 output_tokens_details
func buildOutputTokensDetails(thinkingTokens int) *OutputTokensDetails {
	if thinkingTokens <= 0 {
		return nil
	}
	return &OutputTokensDetails{ThinkingTokens: thinkingTokens}
}
