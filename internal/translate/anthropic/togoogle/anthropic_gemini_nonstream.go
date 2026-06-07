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
)

// handleAnthropicNonStreamResponse 处理 Google Agent Platform 非流式响应，提取文本和用量，转为 Anthropic JSON 格式返回
// isCompact=true 时将文本块转换为 compaction 内容块（Claude Code /compact 协议）
func handleAnthropicNonStreamResponse(w http.ResponseWriter, vertexResp *http.Response, req MessageRequest, traceID string, provider *domain.UserProvider, clientType, modelName string, reqBody []byte, isCompact bool) {
	defer vertexResp.Body.Close()
	bodyBytes, err := io.ReadAll(vertexResp.Body)
	if err != nil {
		http.Error(w, "Failed to read response", http.StatusInternalServerError)
		return
	}

	var vResp map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &vResp); err != nil {
		http.Error(w, "Invalid response from Vertex", http.StatusBadGateway)
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
								contents = append(contents, Content{
									Type:      "thinking",
									Thinking:  thinkText,
									Signature: sig,
								})
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
							argsBytes := normalizeFunctionCallArgs(fc["args"])
							var args map[string]interface{}
							if err := json.Unmarshal(argsBytes, &args); err != nil {
								args = make(map[string]interface{})
							}
							toolID := fmt.Sprintf("toolu_%s_%d", traceID, toolIdx)
							// Gemini 3.x 在 functionCall part 携带 thoughtSignature，
							// 存入缓存以便下一轮请求回填（否则 API 返回 400）
							// 并将其编码到 toolID 中，确保服务重启后客户端历史依然能带回 signature
							if sig, ok := part["thoughtSignature"].(string); ok && sig != "" {
								toolID = fmt.Sprintf("%s_sig_%s", toolID, sig)
								toolThoughtSigCache.Store(toolID, sig)
							} else if lastSig != "" {
								toolID = fmt.Sprintf("%s_sig_%s", toolID, lastSig)
								toolThoughtSigCache.Store(toolID, lastSig)
							}
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

	// 判断是否存在真正有意义的内容块：
	// tool_use / thinking 视为有效内容；空文本块（Text == ""）不算有效内容
	// 因为空文本块会导致 Claude Code /compact 报 "summarization produced empty response"
	hasRealContent := false
	for _, c := range contents {
		if c.Type == "tool_use" || c.Type == "thinking" {
			hasRealContent = true
			break
		}
		if c.Type == "text" && c.Text != "" {
			hasRealContent = true
			break
		}
	}
	if isCompact && hasRealContent {
		for i, c := range contents {
			if c.Type == "text" {
				if !strings.Contains(c.Text, "<summary>") {
					cleanText := strings.TrimSpace(c.Text)
					contents[i].Text = "<analysis>\nGateway manually wrapped this context compaction.\n</analysis>\n<summary>\n" + cleanText + "\n</summary>"
					slog.Info("🔍 [DEBUG] /compact 响应缺失 <summary> 标签，网关已自动补全", "trace_id", traceID)
				}
				break // 只处理第一个 text 块
			}
		}
	}

	if !hasRealContent {
		// 上游返回空响应 → 返回 Anthropic 错误而非空 content
		// 这里返回 529 overloaded_error 以触发 Claude Code 的自动重试机制
		slog.Warn("⚠️ [NonStream] GEAP 返回空响应，上游未生成任何内容块",
			"trace_id", traceID,
			"geap_resp_preview", string(bodyBytes[:min(len(bodyBytes), 500)]))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests) // 也可以使用 529，但 429 和 529 都会触发重试
		// Anthropic SDK 遇到 429 或 529 会自动退避重试
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
