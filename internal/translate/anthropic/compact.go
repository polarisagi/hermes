package anthropic

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// IsCompactRequest 判断当前请求是否为 Claude Code 发出的 /compact 上下文截断请求。
// 客户端会在此时清理旧思考并让模型总结历史。
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

// WrapCompactText 用于 /compact 请求：如果生成的文本缺失 <summary>，自动补全外层结构
func WrapCompactText(text string) string {
	if !strings.Contains(text, "<summary>") {
		return "<analysis>\nGateway manually wrapped this context compaction.\n</analysis>\n<summary>\n" + strings.TrimSpace(text) + "\n</summary>"
	}
	return text
}

// ProcessCompactNonStream 处理非流式响应的 /compact 逻辑。
// 支持 []Content 和 []interface{}（底层为 map[string]interface{}）
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

// CompactStreamManager 管理流式 /compact 响应的缓冲与发射
type CompactStreamManager struct {
	TraceID        string
	compactTextBuf string
}

func (m *CompactStreamManager) BufferText(text string) {
	m.compactTextBuf += text
}

func (m *CompactStreamManager) Flush(w http.ResponseWriter, flusher http.Flusher, writeSSEFunc func(eventType string, data interface{}), blockIndex int) {
	if m.compactTextBuf == "" {
		return
	}

	writeSSEFunc("content_block_start", map[string]interface{}{
		"type": "content_block_start", "index": blockIndex,
		"content_block": map[string]interface{}{"type": "compaction"},
	})

	finalText := WrapCompactText(m.compactTextBuf)
	if finalText != m.compactTextBuf {
		slog.Info("🔍 [DEBUG] /compact 响应缺失 <summary> 标签，网关已自动补全 (Stream)", "trace_id", m.TraceID)
	}

	writeSSEFunc("content_block_delta", map[string]interface{}{
		"type": "content_block_delta", "index": blockIndex,
		"delta": map[string]interface{}{"type": "compaction_delta", "content": finalText},
	})

	writeSSEFunc("content_block_stop", map[string]interface{}{
		"type": "content_block_stop", "index": blockIndex,
	})
	m.compactTextBuf = ""
}

func (m *CompactStreamManager) HasData() bool {
	return m.compactTextBuf != ""
}

// FlattenSystemPrompt 将 Anthropic System Prompt (字符串或数组) 扁平化为纯文本
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

// BuildCompactPrompt 将一个饱含历史、工具调用、思考的复杂 /compact 请求提取压缩为单一纯文本 Prompt。
// 这样可以确保将其发给不支持这些复杂类型或在压缩模式下容易出错的模型（Google, OpenAI, DeepSeek等）时，能获取到极其稳定一致的压缩响应。
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
	promptInjection := fmt.Sprintf("System Context: %s\n\n<conversation_history>\n%s\n</conversation_history>\n\nSystem Task: You are performing a context compaction. Please distill the conversation history above into a highly compressed, concise summary. Focus strictly on preserving critical facts, the user's main intent, important context, and any established rules or constraints. Discard all conversational fluff, routine tool outputs, and redundant steps.", systemPrompt, historyXML)

	return promptInjection
}
