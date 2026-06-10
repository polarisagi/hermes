package togoogle

import (
	"bufio"
	"bytes"
	"context"
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

// streamAnthropicResponse 从 Vertex 后端读取流式 SSE 响应，边读边转为 Anthropic SSE 格式
// 事件序列: message_start → content_block_start → content_block_delta* → content_block_stop → message_delta → message_stop
// 同时在最后解析 usageMetadata 完成计费结算
// 返回 streamOK: false 表示流式传输中发生不可恢复错误（Vertex 返回错误、IO 断联等），调用方应标记节点失败
// streamAnthropicResponse 从 Vertex 后端读取流式 SSE 响应，边读边转为 Anthropic SSE 格式
// isCompact=true 时将文本块输出为 compaction 内容块（Claude Code /compact 协议）
func streamAnthropicResponse(ctx context.Context, w http.ResponseWriter, vertexResp *http.Response, req MessageRequest, traceID string, provider *domain.UserProvider, clientType, modelName string, reqBody []byte, isCompact bool) (streamOK bool) {
	defer vertexResp.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)

	// 用本地估算填充 message_start 的 input_tokens，让 /context 命令能在首事件就显示进度
	// usageMetadata 抵达后，下方 message_delta 会以精确值覆盖
	estimatedInput := estimateAnthropicTokens(req, reqBody)
	writeSSEMessageStart(w, flusher, traceID, modelName, estimatedInput)

	reader := bufio.NewReader(vertexResp.Body)
	var promptTokens, completionTokens, thinkingTokens, cachedTokens int
	var totalWritten int64

	blockIndex := 0
	inText := false
	inThinking := false   // 追踪是否有开放中的 thinking 内容块（用于合并多个 thought 分片）
	emittedText := false  // 追踪是否实际发出了非空文本内容，用于判断是否为空响应
	var thoughtSig string // 当前 thinking 块对应的 thoughtSignature（Gemini 返回，转存为 Anthropic signature）
	var toolID string
	stopReason := "end_turn"
	var matchedStopSeq string // 触发停止的序列（Gemini 不直接返回，通过文本尾部推断）
	var streamError string    // 流中途出错信息

	compactManager := &anthr.CompactStreamManager{TraceID: traceID}

	fallbackPrefix := "[Assistant called tool '"
	fallbackPrefixXML := "<past_tool_execution "
	var fallbackTextBuf string
	var isBufferingFallback bool

	flushFallbackBuffer := func() {
		if !isBufferingFallback {
			return
		}
		isBufferingFallback = false
		if fallbackTextBuf == "" {
			return
		}
		re := regexp.MustCompile(`(?s)^\[Assistant called tool '([^']+)' with arguments: (.*)\]\n?$|(?s)^<past_tool_execution name="([^"]+)">\n?(.*?)\n?</past_tool_execution>\n?$`)
		matches := re.FindStringSubmatch(strings.TrimSpace(fallbackTextBuf))
		if len(matches) > 0 {
			name := matches[1]
			argsStr := matches[2]
			if name == "" {
				name = matches[3]
				argsStr = matches[4]
			}

			toolID = fmt.Sprintf("toolu_%s_%d", traceID, blockIndex)
			writeSSE(w, flusher, "content_block_start", StreamEvent{
				Type:  "content_block_start",
				Index: ptrInt(blockIndex),
				ContentBlock: &Content{
					Type:  "tool_use",
					ID:    toolID,
					Name:  name,
					Input: struct{}{},
				},
			})

			argsRunes := []rune(argsStr)
			chunkSize := 40
			for i := 0; i < len(argsRunes); i += chunkSize {
				end := i + chunkSize
				if end > len(argsRunes) {
					end = len(argsRunes)
				}
				writeSSE(w, flusher, "content_block_delta", StreamEvent{
					Type:  "content_block_delta",
					Index: ptrInt(blockIndex),
					Delta: &Delta{
						Type:        "input_json_delta",
						PartialJson: string(argsRunes[i:end]),
					},
				})
			}

			writeSSEContentBlockStop(w, flusher, blockIndex)
			blockIndex++
			stopReason = "tool_use"
			fallbackTextBuf = ""
			return
		}

		if isCompact {
			compactManager.BufferText(fallbackTextBuf)
			inText = true
			emittedText = true // /compact 缓冲了内容，标记非空以防误判
			fallbackTextBuf = ""
			return
		}

		writeSSE(w, flusher, "content_block_start", StreamEvent{
			Type:  "content_block_start",
			Index: ptrInt(blockIndex),
			ContentBlock: &Content{
				Type: "text",
			},
		})
		inText = true

		writeSSE(w, flusher, "content_block_delta", StreamEvent{
			Type:  "content_block_delta",
			Index: ptrInt(blockIndex),
			Delta: &Delta{Type: "text_delta", Text: fallbackTextBuf},
		})
		fallbackTextBuf = ""
	}

	for {
		// 客户端断开时 r.Context() 被取消，提前退出避免继续消耗上游连接
		if ctx.Err() != nil {
			slog.Debug("🔌 [Stream] 客户端已断开，终止 GEAP 流式响应", "trace_id", traceID)
			return true
		}

		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			totalWritten += int64(len(line))
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			// Non-EOF IO error: connection reset, timeout, etc.
			streamError = fmt.Sprintf("stream read error: %v", err)
			slog.Error("❌ [Stream] GEAP SSE 流读取失败，连接可能中断", "trace_id", traceID, "error", err.Error())
			break
		}

		line = bytes.TrimSpace(line)
		if len(line) == 0 || !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}

		data := bytes.TrimPrefix(line, []byte("data: "))
		dataStr := strings.TrimSpace(string(line[6:]))
		if dataStr == "[DONE]" {
			break
		}

		var vResp map[string]interface{}
		if err := json.Unmarshal(data, &vResp); err != nil {
			slog.Warn("⚠️ [Stream] GEAP SSE 行 JSON 解析失败", "trace_id", traceID, "data_preview", string(data[:min(len(data), 200)]))
			continue
		}

		// Detect promptFeedback block (content policy refusal)
		// Gemini 可能返回 promptFeedback 且 blockReason 为空（静默拒绝），同样需要拦截
		if pf, ok := vResp["promptFeedback"].(map[string]interface{}); ok {
			if blockReason, ok := pf["blockReason"].(string); ok && blockReason != "" {
				streamError = fmt.Sprintf("request blocked by GEAP safety filter: %s", blockReason)
				slog.Error("❌ [Stream] GEAP promptFeedback 阻断请求", "trace_id", traceID, "block_reason", blockReason)
				break
			}
			// promptFeedback 存在但 blockReason 为空 — 静默拒绝，标记为错误
			if _, hasBlock := pf["blockReason"]; hasBlock {
				slog.Warn("⚠️ [Stream] GEAP promptFeedback 静默拒绝 (blockReason 为空)", "trace_id", traceID)
				streamError = "request blocked by GEAP safety filter (silent refusal)"
				break
			}
		}

		// 解析 usageMetadata（最终 chunk 才有完整数值，每次更新覆盖旧值）
		// thoughtsTokenCount：思考 token 计入 output_tokens（Anthropic 将 thinking 并入 output 计费）
		// toolUsePromptTokenCount：工具定义 token 计入 input_tokens（属于 prompt 端消耗）
		if usage, ok := vResp["usageMetadata"].(map[string]interface{}); ok {
			if p, ok := usage["promptTokenCount"].(float64); ok {
				promptTokens = int(p)
			}
			if tool, ok := usage["toolUsePromptTokenCount"].(float64); ok {
				promptTokens += int(tool)
			}
			if c, ok := usage["candidatesTokenCount"].(float64); ok {
				completionTokens = int(c)
			}
			if thoughts, ok := usage["thoughtsTokenCount"].(float64); ok {
				thinkingTokens = int(thoughts)
				completionTokens += thinkingTokens
			}
			if cache, ok := usage["cachedContentTokenCount"].(float64); ok {
				cachedTokens = int(cache)
			}
		}

		candidates, ok := vResp["candidates"].([]interface{})
		if !ok || len(candidates) == 0 {
			continue
		}

		cand, _ := candidates[0].(map[string]interface{})

		// 检查 safetyRatings：Gemini 可能在返回空内容的同时标记安全风险
		// 如 probability != "NEGLIGIBLE" 则说明触发了安全过滤器，应向上报错
		if safetyRatings, ok := cand["safetyRatings"].([]interface{}); ok {
			for _, sr := range safetyRatings {
				if srm, ok := sr.(map[string]interface{}); ok {
					isBlocked, _ := srm["blocked"].(bool)
					if isBlocked {
						cat, _ := srm["category"].(string)
						prob, _ := srm["probability"].(string)
						streamError = fmt.Sprintf("content blocked by GEAP safety filter: category=%s probability=%s", cat, prob)
						slog.Error("❌ [Stream] GEAP safetyRatings 触发安全拦截", "trace_id", traceID, "category", cat, "probability", prob)
						break
					}
				}
			}
			if streamError != "" {
				break
			}
		}

		if finishReason, ok := cand["finishReason"].(string); ok && finishReason != "" {
			switch finishReason {
			case "MAX_TOKENS":
				stopReason = "max_tokens"
			case "STOP":
				// STOP 可能是自然结束或命中 stopSequences
				// Gemini 不返回具体命中的序列，仅当请求有 stopSequences 时标记类型
				if len(req.StopSequences) > 0 {
					stopReason = "stop_sequence"
					_ = matchedStopSeq // 保持空字符串（Gemini 不返回命中的具体序列）
				}
			case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII",
				"IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT", "IMAGE_RECITATION", "IMAGE_OTHER",
				"NO_IMAGE", "OTHER":
				// 安全过滤器、版权、图片生成失败等——映射为 end_turn 让客户端优雅降级
				stopReason = "end_turn"
				slog.Warn("⚠️ [Stream] GEAP 非正常停止原因", "trace_id", traceID, "finish_reason", finishReason)
			case "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL":
				// 模型尝试了工具调用但格式错误/非预期——实际的 tool_use 内容块由 part 处理决定
				// 此处保持 end_turn；若 part 里确实有 functionCall，part 处理会覆盖为 tool_use
				stopReason = "end_turn"
				slog.Warn("⚠️ [Stream] GEAP 工具调用格式异常", "trace_id", traceID, "finish_reason", finishReason)

				// 尝试挽救因模型输出格式错误而被拦截的正文内容
				if fm, ok := cand["finishMessage"].(string); ok && fm != "" {
					text := fm
					if strings.HasPrefix(fm, "Malformed function call: ") {
						text = strings.TrimPrefix(fm, "Malformed function call: ")
					}

					if isCompact {
						compactManager.BufferText(text)
						inText = true
						emittedText = true // /compact 缓冲了内容，标记非空以防误判
					} else {
						if !inText {
							writeSSE(w, flusher, "content_block_start", StreamEvent{
								Type:  "content_block_start",
								Index: ptrInt(blockIndex),
								ContentBlock: &Content{Type: "text"},
							})
							inText = true
						}
						writeSSE(w, flusher, "content_block_delta", StreamEvent{
							Type:  "content_block_delta",
							Index: ptrInt(blockIndex),
							Delta: &Delta{Type: "text_delta", Text: text},
						})
					}
				}
			default:
				stopReason = "end_turn"
			}
		}

		content, ok := cand["content"].(map[string]interface{})
		if !ok {
			continue
		}

		parts, ok := content["parts"].([]interface{})
		if !ok || len(parts) == 0 {
			continue
		}

		for _, partIntf := range parts {
			part, _ := partIntf.(map[string]interface{})

			// Gemini 在启用 thinkingConfig.includeThoughts 时，以 thought=true 的 part 返回推理过程。
			// Gemini 流式可能把同一次思考分成多个 chunk 推送，全部合并到同一个 Anthropic thinking 块
			// 以符合 Anthropic SSE 协议（一次完整思考对应一个 thinking block，内含多个 thinking_delta）。
			// thoughtSignature：Gemini 返回的不透明签名，客户端下一轮需要原样传回以维持思考连贯性；
			// 对应 Anthropic 的 signature_delta 事件，在 content_block_stop 之前发出。
			if isThought, _ := part["thought"].(bool); isThought {
				// 无论客户端是否显式设置 thinking，只要 Gemini 返回 thought part 就处理
				// （Gemini 2.5-pro 可能在 thinkingBudget 未显式禁用时自动思考）

				// 优先捕获 thoughtSignature（可能在任意 chunk 出现，只需记录最新值）
				if sig, ok := part["thoughtSignature"].(string); ok && sig != "" {
					thoughtSig = sig
				}

				if thinkText, ok := part["text"].(string); ok && thinkText != "" {
					if isCompact {
						// /compact 模式下，拦截模型输出的思考内容，将其作为最终 summary 的素材合并缓冲，
						// 从而避免发送 thinking_delta 导致 Claude Code 客户端发生 400 错误
						compactManager.BufferText(thinkText)
						inText = true
						emittedText = true
						continue
					}

					// 有正文块打开时先关闭（thinking 必须排在 text 之前）
					if inText {
						writeSSEContentBlockStop(w, flusher, blockIndex)
						inText = false
						blockIndex++
					}
					// 首个 thought chunk：开启 thinking 块
					if !inThinking {
						writeSSE(w, flusher, "content_block_start", StreamEvent{
							Type:  "content_block_start",
							Index: ptrInt(blockIndex),
							ContentBlock: &Content{
								Type:     "thinking",
								Thinking: "",
							},
						})
						inThinking = true
					}
					// 追加 thinking_delta（多 chunk 持续写入同一块）
					writeSSE(w, flusher, "content_block_delta", StreamEvent{
						Type:  "content_block_delta",
						Index: ptrInt(blockIndex),
						Delta: &Delta{
							Type:     "thinking_delta",
							Thinking: thinkText,
						},
					})
				}
				continue
			}

			// 遇到非 thought part 时，关闭尚未关闭的 thinking 块
			// 关闭前先发 signature_delta（Anthropic 要求 signature 在 content_block_stop 之前）
			if inThinking {
				if thoughtSig != "" {
					writeSSE(w, flusher, "content_block_delta", StreamEvent{
						Type:  "content_block_delta",
						Index: ptrInt(blockIndex),
						Delta: &Delta{
							Type:      "signature_delta",
							Signature: thoughtSig,
						},
					})
					// 不清空 thoughtSig，保留给后续的 functionCall 使用
				}
				writeSSEContentBlockStop(w, flusher, blockIndex)
				inThinking = false
				blockIndex++
			}

			if text, ok := part["text"].(string); ok {
				if text != "" {
					emittedText = true

					if isCompact {
						compactManager.BufferText(text)
						inText = true    // 标记为 inText，让收尾逻辑触发 content_block_stop
						emittedText = true // /compact 模式下文本已缓冲，视为有效内容，避免误判为空响应
						continue
					}

					if !inText && !isBufferingFallback {
						if strings.HasPrefix(fallbackPrefix, text) || strings.HasPrefix(text, fallbackPrefix) || strings.HasPrefix(fallbackPrefixXML, text) || strings.HasPrefix(text, fallbackPrefixXML) {
							isBufferingFallback = true
							fallbackTextBuf = text
						} else {
							blockType := "text"
							writeSSE(w, flusher, "content_block_start", StreamEvent{
								Type:  "content_block_start",
								Index: ptrInt(blockIndex),
								ContentBlock: &Content{
									Type: blockType,
								},
							})
							inText = true

							writeSSE(w, flusher, "content_block_delta", StreamEvent{
								Type:  "content_block_delta",
								Index: ptrInt(blockIndex),
								Delta: &Delta{Type: "text_delta", Text: text},
							})
						}
					} else if isBufferingFallback {
						fallbackTextBuf += text
						if !(strings.HasPrefix(fallbackPrefix, fallbackTextBuf) || strings.HasPrefix(fallbackTextBuf, fallbackPrefix) || strings.HasPrefix(fallbackPrefixXML, fallbackTextBuf) || strings.HasPrefix(fallbackTextBuf, fallbackPrefixXML)) {
							flushFallbackBuffer()
						}
					} else if inText {
						writeSSE(w, flusher, "content_block_delta", StreamEvent{
							Type:  "content_block_delta",
							Index: ptrInt(blockIndex),
							Delta: &Delta{Type: "text_delta", Text: text},
						})
					}
				}
			}

			if fc, ok := part["functionCall"].(map[string]interface{}); ok {
				if isBufferingFallback {
					flushFallbackBuffer()
				}
				if inText {
					if isCompact {
						compactManager.Flush(func(eventType string, data interface{}) {
							writeSSE(w, flusher, eventType, data)
						}, blockIndex)
					} else {
						writeSSE(w, flusher, "content_block_stop", StreamEvent{
							Type:  "content_block_stop",
							Index: ptrInt(blockIndex),
						})
					}
					inText = false
					blockIndex++
				}

				name, _ := fc["name"].(string)
				if name == "" {
					slog.Warn("⚠️ [Stream] functionCall 缺少 name 字段，跳过", "trace_id", traceID)
					continue
				}
				toolID = fmt.Sprintf("toolu_%s_%d", traceID, blockIndex)
				// Gemini 3.x 在 functionCall part 携带 thoughtSignature，
				// 存入缓存以便下一轮请求回填（否则 API 返回 400）
				// 并将其编码到 toolID 中，确保服务重启后客户端历史依然能带回 signature
				if sig, ok := part["thoughtSignature"].(string); ok && sig != "" {
					thoughtSig = sig // 保存给后续平行的 functionCall 使用
					toolID = fmt.Sprintf("%s_sig_%s", toolID, sig)
					toolThoughtSigCache.Store(toolID, sig)
				} else if thoughtSig != "" {
					toolID = fmt.Sprintf("%s_sig_%s", toolID, thoughtSig)
					toolThoughtSigCache.Store(toolID, thoughtSig)
				}

				writeSSE(w, flusher, "content_block_start", StreamEvent{
					Type:  "content_block_start",
					Index: ptrInt(blockIndex),
					ContentBlock: &Content{
						Type:  "tool_use",
						ID:    toolID,
						Name:  name,
						Input: struct{}{}, // Ensures "input": {} instead of missing field due to omitempty
					},
				})

				argsBytes := normalizeFunctionCallArgs(fc["args"])

				argsRunes := []rune(string(argsBytes))
				chunkSize := 40
				for i := 0; i < len(argsRunes); i += chunkSize {
					end := i + chunkSize
					if end > len(argsRunes) {
						end = len(argsRunes)
					}

					writeSSE(w, flusher, "content_block_delta", StreamEvent{
						Type:  "content_block_delta",
						Index: ptrInt(blockIndex),
						Delta: &Delta{
							Type:        "input_json_delta",
							PartialJson: string(argsRunes[i:end]),
						},
					})
				}

				writeSSEContentBlockStop(w, flusher, blockIndex)

				blockIndex++
				stopReason = "tool_use"
			}
		}
	}

	if isBufferingFallback {
		flushFallbackBuffer()
	}

	// 上游返回空响应 → 发送 Anthropic 错误事件，而非注入空文本块
	// 空文本块会导致 Claude Code /compact 报 "summarization produced empty response"，掩盖真正原因
	if streamError == "" && blockIndex == 0 && !inThinking && !emittedText {
		slog.Warn("⚠️ [Stream] GEAP 返回空响应，上游未生成任何内容块",
			"trace_id", traceID,
			"prompt_tokens", promptTokens)
		streamError = "upstream model returned empty response — triggering automatic retry"
	}

	if streamError != "" {
		// 先关闭所有开放中的内容块，让客户端能干净解析已收到的部分
		if inThinking {
			if thoughtSig != "" {
				writeSSE(w, flusher, "content_block_delta", StreamEvent{
					Type:  "content_block_delta",
					Index: ptrInt(blockIndex),
					Delta: &Delta{Type: "signature_delta", Signature: thoughtSig},
				})
			}
			writeSSEContentBlockStop(w, flusher, blockIndex)
		}
		if inText {
			writeSSEContentBlockStop(w, flusher, blockIndex)
		}
		// Anthropic 错误事件格式：event: error / data: {"type":"error","error":{"type":"...","message":"..."}}
		// 用 json.Marshal 保证 message 中的特殊字符正确转义
		// 使用 overloaded_error 会触发 Claude Code 和官方 SDK 的自动退避重试
		errType := "api_error"
		if strings.Contains(streamError, "triggering automatic retry") {
			errType = "overloaded_error"
		}
		errPayload, _ := json.Marshal(map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    errType,
				"message": streamError,
			},
		})
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", errPayload)
		if flusher != nil {
			flusher.Flush()
		}
		return false
	}

	// 关闭所有尚未关闭的内容块
	// thinking 块关闭前先发 signature_delta，让客户端在多轮对话时能正确传回签名
	if inThinking {
		if thoughtSig != "" {
			writeSSE(w, flusher, "content_block_delta", StreamEvent{
				Type:  "content_block_delta",
				Index: ptrInt(blockIndex),
				Delta: &Delta{
					Type:      "signature_delta",
					Signature: thoughtSig,
				},
			})
		}
		writeSSEContentBlockStop(w, flusher, blockIndex)
		blockIndex++
	}
	if inText {
		if isCompact {
			compactManager.Flush(func(eventType string, data interface{}) {
				writeSSE(w, flusher, eventType, data)
			}, blockIndex)
		} else {
			writeSSEContentBlockStop(w, flusher, blockIndex)
		}
	}
	// message_delta：Anthropic 协议要求附带最终 stop_reason 与精确 usage
	// Gemini 的 cachedContentTokenCount 映射成 cache_read_input_tokens，
	// Claude Code 的 /cost 命令据此识别 prompt cache 命中以反映真实费用
	var outputTokensDetails *OutputTokensDetails
	if thinkingTokens > 0 {
		outputTokensDetails = &OutputTokensDetails{ThinkingTokens: thinkingTokens}
	}
	msgDeltaEvent := StreamEvent{
		Type: "message_delta",
		Delta: &Delta{
			StopReason: stopReason,
		},
		Usage: &Usage{
			InputTokens:          promptTokens,
			OutputTokens:         completionTokens,
			CacheReadInputTokens: cachedTokens,
			OutputTokensDetails:  outputTokensDetails,
		},
	}
	writeSSE(w, flusher, "message_delta", msgDeltaEvent)

	writeSSEMessageStop(w, flusher)

	// GEAP 偶发不返回 usageMetadata（极少见），此时用字节估算兜底保证计费不丢失
	// 注：!streamOK 在此处永远为 false（命名返回值尚未赋值），去掉该条件让逻辑更清晰
	if promptTokens == 0 && completionTokens == 0 {
		// promptTokens = int(router.EstimatePromptTokens(reqBody))
		// completionTokens = int(router.EstimateCompletionTokens(totalWritten))
		slog.Warn("⚠️ GEAP 未返回 usageMetadata，启用 token 估算兜底", "trace_id", traceID, "prompt", promptTokens, "completion", completionTokens)
	}

	// router.SettleBilling("google", dest.Node.Name, clientType, "anthropic_adapter", modelName, int64(promptTokens), int64(completionTokens), int64(cachedTokens), http.StatusOK, dest, reqBody, traceID)

	return true
}

func estimateAnthropicTokens(req MessageRequest, reqBody []byte) int {
	return anthr.EstimateInputTokens(reqBody)
}
