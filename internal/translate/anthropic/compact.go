package anthropic

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// IsCompactRequest 判断当前请求是否为 Claude Code 发出的 /compact 上下文截断请求。
//
// Claude Code 在发出 /compact 请求时会有以下特征（满足 ≥2 个即判定）：
//   - 消息内容含 "TEXT ONLY"（提示模型仅返回文本）
//   - 消息内容含 "summary"（大小写不敏感）
//   - 消息内容含 "Do NOT call any tools"
//   - 消息内容含 <analysis> 或 <summary> 标签
//   - context_management.edits 中含 clear_thinking_* 或 compact_* 类型策略
//
// Beta Header: anthropic-beta: compact-2026-01-12
func IsCompactRequest(req *MessageRequest) bool {
	if req == nil || len(req.Messages) == 0 {
		return false
	}

	lastMsg := req.Messages[len(req.Messages)-1]
	if lastMsg.Role != "user" {
		return false
	}

	lastMsgBytes, err := json.Marshal(lastMsg.Content)
	if err != nil {
		return false
	}
	lastMsgStr := string(lastMsgBytes)

	features := 0
	if strings.Contains(lastMsgStr, "TEXT ONLY") {
		features++
	}
	if strings.Contains(strings.ToLower(lastMsgStr), "summary") {
		features++
	}
	if strings.Contains(lastMsgStr, "Do NOT call any tools") {
		features++
	}
	if strings.Contains(lastMsgStr, "<analysis>") {
		features++
	}
	if strings.Contains(lastMsgStr, "<summary>") {
		features++
	}
	if req.ContextManagement != nil {
		for _, edit := range req.ContextManagement.Edits {
			if strings.HasPrefix(edit.Type, "clear_thinking_") {
				features++
			}
			if strings.HasPrefix(edit.Type, "compact_") {
				features++
			}
		}
	}

	return features >= 2
}

// WrapCompactText 用于 /compact 请求：如果生成的文本缺失 <summary> 标签，自动补全外层结构。
//
// 官方格式：
//
//	<analysis>...分析...</analysis>
//	<summary>...压缩后的上下文摘要...</summary>
func WrapCompactText(text string) string {
	if !strings.Contains(text, "<summary>") {
		return "<analysis>\nGateway manually wrapped this context compaction.\n</analysis>\n<summary>\n" + strings.TrimSpace(text) + "\n</summary>"
	}
	return text
}

// HasRealContent 判断 []Content 切片中是否包含真正有意义的内容块。
//
// 有效内容定义：
//   - type=tool_use 或 type=thinking：无论内容是否为空，均视为有效
//   - type=text：Text 字段非空时视为有效
//   - type=compaction：Content 字段（string）非空时视为有效（/compact 模式）
//
// 用途：togoogle 非流式响应的有效内容检测，防止将空响应误判为成功
func HasRealContent(contents []Content) bool {
	for _, c := range contents {
		switch c.Type {
		case "tool_use", "thinking":
			return true
		case "text":
			if c.Text != "" {
				return true
			}
		case "compaction":
			// /compact 模式下文本被转为 compaction 类型，Content 字段存储文本
			if str, ok := c.Content.(string); ok && str != "" {
				return true
			}
		}
	}
	return false
}

// HasRealContentRaw 判断 []interface{} 内容块切片是否包含真正有意义的内容。
//
// 与 HasRealContent 相同逻辑，但处理 map[string]interface{} 形式的内容块
// （toopenai 等使用动态 map 而非强类型结构体的场景）
func HasRealContentRaw(contents []interface{}) bool {
	for _, item := range contents {
		c, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		t, _ := c["type"].(string)
		switch t {
		case "tool_use", "thinking":
			return true
		case "text":
			if text, _ := c["text"].(string); text != "" {
				return true
			}
		case "compaction":
			// /compact 模式下内容在 "content" 字段
			if content, _ := c["content"].(string); content != "" {
				return true
			}
		}
	}
	return false
}

// HasRealContentMapSlice 判断 []map[string]interface{} 内容块切片是否包含真正有意义的内容。
//
// 与 HasRealContentRaw 相同逻辑，但处理 []map[string]interface{} 类型
// （toopenai handleNonStream 使用此类型构建 contents）
func HasRealContentMapSlice(contents []map[string]interface{}) bool {
	for _, c := range contents {
		t, _ := c["type"].(string)
		switch t {
		case "tool_use", "thinking":
			return true
		case "text":
			if text, _ := c["text"].(string); text != "" {
				return true
			}
		case "compaction":
			if content, _ := c["content"].(string); content != "" {
				return true
			}
		}
	}
	return false
}

// ProcessCompactNonStream 处理非流式响应的 /compact 逻辑。
//
// 将响应内容块中第一个 text 或 compaction 块转换为标准的 compaction 格式：
//   - type 设为 "compaction"
//   - content 字段存放摘要文本（如缺失 <summary> 标签则自动补全）
//   - text 字段清空
//
// 支持 []Content（togoogle 强类型）和 []interface{}（toopenai 动态 map）两种输入
func ProcessCompactNonStream(contents interface{}) {
	switch v := contents.(type) {
	case []Content:
		for i, c := range v {
			if c.Type == "text" || c.Type == "compaction" {
				var contentStr string
				if str, ok := c.Content.(string); ok {
					contentStr = str
				} else {
					contentStr = c.Text
				}
				wrappedText := WrapCompactText(contentStr)
				if wrappedText != contentStr {
					slog.Info("🔍 [DEBUG] /compact 响应缺失 <summary> 标签，网关已自动补全")
				}
				v[i].Type = "compaction"
				v[i].Content = wrappedText
				v[i].Text = "" // 清空 text 字段
				break
			}
		}
	case []interface{}:
		for _, contentRaw := range v {
			if content, ok := contentRaw.(map[string]interface{}); ok {
				t, _ := content["type"].(string)
				if t == "text" || t == "compaction" {
					var contentStr string
					if str, ok := content["content"].(string); ok {
						contentStr = str
					} else if str, ok := content["text"].(string); ok {
						contentStr = str
					}
					wrappedText := WrapCompactText(contentStr)
					if wrappedText != contentStr {
						slog.Info("🔍 [DEBUG] /compact 响应缺失 <summary> 标签，网关已自动补全")
					}
					content["type"] = "compaction"
					content["content"] = wrappedText
					delete(content, "text")
					break
				}
			}
		}
	case []map[string]interface{}:
		for _, content := range v {
			t, _ := content["type"].(string)
			if t == "text" || t == "compaction" {
				var contentStr string
				if str, ok := content["content"].(string); ok {
					contentStr = str
				} else if str, ok := content["text"].(string); ok {
					contentStr = str
				}
				wrappedText := WrapCompactText(contentStr)
				if wrappedText != contentStr {
					slog.Info("🔍 [DEBUG] /compact 响应缺失 <summary> 标签，网关已自动补全")
				}
				content["type"] = "compaction"
				content["content"] = wrappedText
				delete(content, "text")
				break
			}
		}
	}
}

// CompactStreamManager 管理流式 /compact 响应的缓冲与发射。
//
// 官方协议（compact-2026-01-12）：compaction_delta 一次性投递完整内容，
// 而非像 text_delta 那样逐字符流式传输。因此本管理器先将所有文本缓冲，
// 再在流结束时一次性发出三个事件：content_block_start → content_block_delta → content_block_stop
type CompactStreamManager struct {
	TraceID        string
	compactTextBuf string
}

// BufferText 向缓冲区追加文本（流式接收到的每个文本分片）
func (m *CompactStreamManager) BufferText(text string) {
	slog.Info("🔍 [CompactDebug] BufferText 追加文本", "trace_id", m.TraceID, "length", len(text), "text_preview", text[:min(len(text), 100)])
	m.compactTextBuf += text
}

// HasData 返回缓冲区是否有待发送的内容
func (m *CompactStreamManager) HasData() bool {
	return m.compactTextBuf != ""
}

// Flush 将缓冲区内容以标准 compaction SSE 事件序列发出。
//
// 发出事件序列（符合官方 compact-2026-01-12 规范）：
//
//	content_block_start  → content_block.type = "compaction"
//	content_block_delta  → delta.type = "compaction_delta", delta.content = 完整摘要
//	content_block_stop
//
// 参数：
//   - writeSSEFunc: SSE 事件写入函数（由各子翻译器的 writeEv/writeSSE 闭包提供）
//   - blockIndex: 当前内容块的索引
func (m *CompactStreamManager) Flush(writeSSEFunc func(eventType string, data interface{}), blockIndex int) {
	if m.compactTextBuf == "" {
		slog.Warn("⚠️ [CompactDebug] Flush 被调用但缓冲区为空，无法发出 compaction 块！", "trace_id", m.TraceID)
		return
	}
	slog.Info("🔍 [CompactDebug] Flush 开始发出 compaction 块", "trace_id", m.TraceID, "total_length", len(m.compactTextBuf))

	finalText := WrapCompactText(m.compactTextBuf)
	if finalText != m.compactTextBuf {
		slog.Info("🔍 [DEBUG] /compact 响应缺失 <summary> 标签，网关已自动补全 (Stream)", "trace_id", m.TraceID)
	}

	writeSSEFunc("content_block_start", map[string]interface{}{
		"type":  "content_block_start",
		"index": blockIndex,
		"content_block": map[string]interface{}{
			"type": "text",
			"text": "",
		},
	})

	writeSSEFunc("content_block_delta", map[string]interface{}{
		"type":  "content_block_delta",
		"index": blockIndex,
		"delta": map[string]interface{}{
			"type": "text_delta",
			"text": finalText,
		},
	})

	writeSSEFunc("content_block_stop", map[string]interface{}{
		"type":  "content_block_stop",
		"index": blockIndex,
	})

	m.compactTextBuf = ""
}

// FlattenSystemPrompt 将 Anthropic System Prompt（字符串或数组格式）扁平化为纯文本。
// 用于 BuildCompactPrompt 提取 system 内容。
func FlattenSystemPrompt(sys interface{}) string {
	if sys == nil {
		return ""
	}
	switch v := sys.(type) {
	case string:
		return v
	case []interface{}:
		var sb strings.Builder
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok {
					sb.WriteString(t)
					sb.WriteString("\n")
				}
			}
		}
		return strings.TrimSpace(sb.String())
	}
	return ""
}

// BuildCompactPrompt 将复杂的 /compact 请求（含工具调用、思考块、历史等）压缩为单一纯文本 Prompt。
//
// 通过将所有消息历史序列化为 XML 结构后，以单条 user 消息发给目标模型（Google/OpenAI/DeepSeek），
// 避免这些模型因无法处理 Anthropic 专属内容块（thinking、tool_result 等）而出错。
// 目标模型返回的摘要文本将被 ProcessCompactNonStream 或 CompactStreamManager 转换为
// 标准的 compaction content block 返回给 Claude Code。
func BuildCompactPrompt(req *MessageRequest) string {
	var sb strings.Builder
	for _, msg := range req.Messages {
		sb.WriteString(fmt.Sprintf("<turn role=\"%s\">\n", msg.Role))
		switch c := msg.Content.(type) {
		case string:
			sb.WriteString(c)
		case []interface{}:
			for _, block := range c {
				if blk, ok := block.(map[string]interface{}); ok {
					switch blk["type"] {
					case "text", "compaction":
						if t, ok := blk["text"].(string); ok && t != "" {
							sb.WriteString(t)
						} else if ct, ok := blk["content"].(string); ok && ct != "" {
							sb.WriteString(ct)
						}
					case "tool_use":
						name, _ := blk["name"].(string)
						input, _ := json.Marshal(blk["input"])
						sb.WriteString(fmt.Sprintf("[Tool Use: %s, Args: %s]\n", name, string(input)))
					case "tool_result":
						content, _ := json.Marshal(blk["content"])
						sb.WriteString(fmt.Sprintf("[Tool Result: %s]\n", string(content)))
					}
				}
			}
		}
		sb.WriteString("\n</turn>\n")
	}
	historyXML := sb.String()
	systemPrompt := FlattenSystemPrompt(req.System)
	return fmt.Sprintf(
		"System Context: %s\n\n<conversation_history>\n%s\n</conversation_history>\n\n"+
			"System Task: Context Compaction (Session State Summarization)\n"+
			"Goal: Distill the entire conversation history into a dense, highly compressed summary that preserves critical facts, the user's intent, and established rules.\n\n"+
			"CRITICAL CONSTRAINTS:\n"+
			"1. FOCUS: Discard all conversational fluff, routine tool outputs, and redundant debugging steps. Extract ONLY the core architectural decisions and current project state.\n"+
			"2. LENGTH & FORMAT: Your FINAL output must be as concise as possible and strictly adhere to the exact XML structure below. Do NOT output any text outside of these tags.\n\n"+
			"<analysis>\n[Briefly state the current task status and the key technical decisions made]\n</analysis>\n"+
			"<summary>\n[Dense, highly compressed summary preserving all critical context and constraints]\n</summary>",
		systemPrompt, historyXML,
	)
}
