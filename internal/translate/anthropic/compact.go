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
// 合并所有 text 和 compaction 块为单一的 compaction 格式：
//   - type 设为 "compaction"
//   - content 字段存放合并后的摘要文本（如缺失 <summary> 标签则自动补全）
//   - 非文本块（tool_use 等）按原样保留
//
// Gemini 可能返回多个 text/compaction 块（如 thinking 文本 + summary 文本），
// 必须全部合并，否则只取第一个会导致摘要内容不完整。
//
// 支持三种内容切片类型，返回合并后不含重复文本块的新切片。
func ProcessCompactNonStream(contents interface{}) interface{} {
	switch v := contents.(type) {
	case []Content:
		var mergedText strings.Builder
		var compactIdx int = -1
		for i, c := range v {
			if c.Type == "text" || c.Type == "compaction" {
				if compactIdx == -1 {
					compactIdx = i
				}
				if str, ok := c.Content.(string); ok && str != "" {
					if mergedText.Len() > 0 {
						mergedText.WriteString("\n\n")
					}
					mergedText.WriteString(str)
				} else if c.Text != "" {
					if mergedText.Len() > 0 {
						mergedText.WriteString("\n\n")
					}
					mergedText.WriteString(c.Text)
				}
			}
		}
		if compactIdx >= 0 {
			result := make([]Content, 0, len(v))
			for i, c := range v {
				if c.Type == "text" || c.Type == "compaction" {
					if i == compactIdx {
						wrappedText := WrapCompactText(mergedText.String())
						if wrappedText != mergedText.String() {
							slog.Info("🔍 [DEBUG] /compact 响应缺失 <summary> 标签，网关已自动补全")
						}
						result = append(result, Content{
							Type:    "compaction",
							Content: wrappedText,
						})
					}
				} else {
					result = append(result, c)
				}
			}
			return result
		}
	case []interface{}:
		var mergedText strings.Builder
		var compactIdx int = -1
		for i, contentRaw := range v {
			if content, ok := contentRaw.(map[string]interface{}); ok {
				t, _ := content["type"].(string)
				if t == "text" || t == "compaction" {
					if compactIdx == -1 {
						compactIdx = i
					}
					if str, ok := content["content"].(string); ok && str != "" {
						if mergedText.Len() > 0 {
							mergedText.WriteString("\n\n")
						}
						mergedText.WriteString(str)
					} else if str, ok := content["text"].(string); ok && str != "" {
						if mergedText.Len() > 0 {
							mergedText.WriteString("\n\n")
						}
						mergedText.WriteString(str)
					}
				}
			}
		}
		if compactIdx >= 0 {
			result := make([]interface{}, 0, len(v))
			for i, contentRaw := range v {
				if content, ok := contentRaw.(map[string]interface{}); ok {
					if t, _ := content["type"].(string); t == "text" || t == "compaction" {
						if i == compactIdx {
							wrappedText := WrapCompactText(mergedText.String())
							if wrappedText != mergedText.String() {
								slog.Info("🔍 [DEBUG] /compact 响应缺失 <summary> 标签，网关已自动补全")
							}
							result = append(result, map[string]interface{}{
								"type":    "compaction",
								"content": wrappedText,
							})
						}
					} else {
						result = append(result, contentRaw)
					}
				}
			}
			return result
		}
	case []map[string]interface{}:
		var mergedText strings.Builder
		var compactIdx int = -1
		for i, content := range v {
			t, _ := content["type"].(string)
			if t == "text" || t == "compaction" {
				if compactIdx == -1 {
					compactIdx = i
				}
				if str, ok := content["content"].(string); ok && str != "" {
					if mergedText.Len() > 0 {
						mergedText.WriteString("\n\n")
					}
					mergedText.WriteString(str)
				} else if str, ok := content["text"].(string); ok && str != "" {
					if mergedText.Len() > 0 {
						mergedText.WriteString("\n\n")
					}
					mergedText.WriteString(str)
				}
			}
		}
		if compactIdx >= 0 {
			result := make([]map[string]interface{}, 0, len(v))
			for i, content := range v {
				if t, _ := content["type"].(string); t == "text" || t == "compaction" {
					if i == compactIdx {
						wrappedText := WrapCompactText(mergedText.String())
						if wrappedText != mergedText.String() {
							slog.Info("🔍 [DEBUG] /compact 响应缺失 <summary> 标签，网关已自动补全")
						}
						result = append(result, map[string]interface{}{
							"type":    "compaction",
							"content": wrappedText,
						})
					}
				} else {
					result = append(result, content)
				}
			}
			return result
		}
	}
	return contents
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
	slog.Debug("🔍 [Compact] BufferText 追加文本", "trace_id", m.TraceID, "length", len(text))
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
		slog.Warn("⚠️ [Compact] Flush 被调用但缓冲区为空，无法发出 compaction 块！", "trace_id", m.TraceID)
		return
	}
	slog.Debug("🔍 [Compact] Flush 开始发出 compaction 块", "trace_id", m.TraceID, "total_length", len(m.compactTextBuf))

	finalText := WrapCompactText(m.compactTextBuf)
	if finalText != m.compactTextBuf {
		slog.Debug("🔍 [Compact] /compact 响应缺失 <summary> 标签，网关已自动补全 (Stream)", "trace_id", m.TraceID)
	}

	writeSSEFunc("content_block_start", map[string]interface{}{
		"type":  "content_block_start",
		"index": blockIndex,
		"content_block": map[string]interface{}{
			"type":    "compaction",
			"content": "",
		},
	})

	writeSSEFunc("content_block_delta", map[string]interface{}{
		"type":  "content_block_delta",
		"index": blockIndex,
		"delta": map[string]interface{}{
			"type":    "compaction_delta",
			"content": finalText,
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

// GenerateFallbackCompactContent 当上游模型未生成任何文本内容时，生成一个基础的降级 compaction 块。
//
// 返回的 <summary> 告知 Claude Code 此次 compaction 未获得有效摘要，
// 客户端应减小请求规模后重试，或等待片刻让上游恢复。
func GenerateFallbackCompactContent(stopReason string) string {
	if stopReason == "max_tokens" {
		return "<analysis>\nThe model exhausted its output token budget (max_tokens) before producing any content. This indicates the thinking phase consumed the entire output allocation.\n</analysis>\n<summary>\nCompaction summary unavailable — the model output was truncated at max_tokens. Retry with a higher max_tokens value or reduce input context size.\n</summary>"
	}
	return "<analysis>\nThe upstream model did not produce any text content. This may be caused by a transient backend issue or the model choosing to respond with only internal reasoning.\n</analysis>\n<summary>\nCompaction summary unavailable — the model returned an empty response. Please try again or use /compact with a smaller conversation size.\n</summary>"
}

// BuildCompactPrompt 将复杂的 /compact 请求（含工具调用、思考块、历史等）压缩为单一纯文本 Prompt。
//
// 通过将所有消息历史序列化为 XML 结构后，以单条 user 消息发给目标模型（Google/OpenAI/DeepSeek），
// 避免这些模型因无法处理 Anthropic 专属内容块（thinking、tool_result 等）而出错。
// 目标模型返回的摘要文本将被 ProcessCompactNonStream 或 CompactStreamManager 转换为
// 标准的 compaction content block 返回给 Claude Code。
//
// 提示词结构参照 Claude Code 官方源码（2.1.170），保持与官方 /compact 行为的一致性。
//
// 注意：tool_result 内容被完全跳过——工具原始输出（文件内容、命令输出等）
// 极为冗长，直接喂给第三方模型会导致其原样复制这些内容到摘要中。
// tool_use 只保留工具名与关键参数，第三方模型无法像 Claude 一样原生理解工具调用语义。
func BuildCompactPrompt(req *MessageRequest) string {
	var sb strings.Builder
	var additionalInstructions string

	for _, msg := range req.Messages {
		sb.WriteString(fmt.Sprintf("<turn role=\"%s\">\n", msg.Role))
		switch c := msg.Content.(type) {
		case string:
			// 从最后一条 user 消息提取自定义 compact 指令（来自 CLAUDE.md 的 ## Compact Instructions）
			if msg.Role == "user" {
				additionalInstructions = extractCompactInstructions(c)
			}
			sb.WriteString(c)
		case []interface{}:
			for _, block := range c {
				if blk, ok := block.(map[string]interface{}); ok {
					switch blk["type"] {
					case "text", "compaction":
						var t string
						if txt, ok := blk["text"].(string); ok && txt != "" {
							t = txt
						} else if ct, ok := blk["content"].(string); ok && ct != "" {
							t = ct
						}
						if msg.Role == "user" && t != "" {
							additionalInstructions = extractCompactInstructions(t)
						}
						sb.WriteString(t)
					case "tool_use":
						name, _ := blk["name"].(string)
						// 只记录工具名和关键参数，避免完整 JSON 被模型原样抄写
						if input, ok := blk["input"].(map[string]interface{}); ok {
							if filePath, ok := input["file_path"].(string); ok {
								sb.WriteString(fmt.Sprintf("[Tool: %s → %s]\n", name, filePath))
							} else if cmd, ok := input["command"].(string); ok {
								if len(cmd) > 80 {
									cmd = cmd[:80] + "..."
								}
								sb.WriteString(fmt.Sprintf("[Tool: %s → %s]\n", name, cmd))
							} else {
								sb.WriteString(fmt.Sprintf("[Tool: %s]\n", name))
							}
						} else {
							sb.WriteString(fmt.Sprintf("[Tool: %s]\n", name))
						}
					case "tool_result":
						// 跳过：原始工具输出极为冗长，第三方模型会原样复制到摘要中。
						// 上方的 tool_use 记录已足够让模型理解发生了什么操作。
					}
				}
			}
		}
		sb.WriteString("\n</turn>\n")
	}

	historyXML := sb.String()
	systemPrompt := FlattenSystemPrompt(req.System)

	var systemSection string
	if systemPrompt != "" {
		systemSection = fmt.Sprintf("<system_context>\n%s\n</system_context>\n\n", systemPrompt)
	}

	const header = `CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.

- Do NOT use Read, Bash, Grep, Glob, Edit, Write, or ANY other tool.
- You already have all the context you need in the conversation above.
- Tool calls will be REJECTED and will waste your only turn — you will fail the task.
- Your entire response must be plain text: an <analysis> block followed by a <summary> block.

`

	const body = `Your task is to create a detailed summary of the conversation so far, paying close attention to the user's explicit requests and your previous actions.
This summary should be thorough in capturing technical details, code patterns, and architectural decisions that would be essential for continuing development work without losing context.

Before providing your final summary, wrap your analysis in <analysis> tags to organize your thoughts and ensure you've covered all necessary points. In your analysis process:

1. Chronologically analyze each message and section of the conversation. For each section thoroughly identify:
   - The user's explicit requests and intents
   - Your approach to addressing the user's requests
   - Key decisions, technical concepts and code patterns
   - Specific details like:
     - file names
     - full code snippets
     - function signatures
     - file edits
   - Errors that you ran into and how you fixed them
   - Pay special attention to specific user feedback that you received, especially if the user told you to do something differently.
   - Note any security-relevant instructions or constraints the user stated (e.g., sensitive files or data to avoid, operations that must not be performed, credential or secret handling rules). These MUST be preserved verbatim in the summary so they continue to apply after compaction.
2. Double-check for technical accuracy and completeness, addressing each required element thoroughly.

Your summary should include the following sections:

1. Primary Request and Intent: Capture all of the user's explicit requests and intents in detail
2. Key Technical Concepts: List all important technical concepts, technologies, and frameworks discussed.
3. Files and Code Sections: Enumerate specific files and code sections examined, modified, or created. Pay special attention to the most recent messages and include full code snippets where applicable and include a summary of why this file read or edit is important.
4. Errors and fixes: List all errors that you ran into, and how you fixed them. Pay special attention to specific user feedback that you received, especially if the user told you to do something differently.
5. Problem Solving: Document problems solved and any ongoing troubleshooting efforts.
6. All user messages: List ALL user messages that are not tool results. These are critical for understanding the users' feedback and changing intent. Preserve any security-relevant instructions or constraints verbatim so they remain in effect after compaction.
7. Pending Tasks: Outline any pending tasks that you have explicitly been asked to work on.
8. Current Work: Describe in detail precisely what was being worked on immediately before this summary request, paying special attention to the most recent messages from both user and assistant. Include file names and code snippets where applicable.
9. Optional Next Step: List the next step that you will take that is related to the most recent work you were doing. IMPORTANT: ensure that this step is DIRECTLY in line with the user's most recent explicit requests, and the task you were working on immediately before this summary request. If your last task was concluded, then only list next steps if they are explicitly in line with the users request. Do not start on tangential requests or really old requests that were already completed without confirming with the user first.
                       If there is a next step, include direct quotes from the most recent conversation showing exactly what task you were working on and where you left off. This should be verbatim to ensure there's no drift in task interpretation.

Here's an example of how your output should be structured:

<example>
<analysis>
[Your thought process, ensuring all points are covered thoroughly and accurately]
</analysis>

<summary>
1. Primary Request and Intent:
   [Detailed description]

2. Key Technical Concepts:
   - [Concept 1]
   - [Concept 2]
   - [...]

3. Files and Code Sections:
   - [File Name 1]
      - [Summary of why this file is important]
      - [Summary of the changes made to this file, if any]
      - [Important Code Snippet]
   - [File Name 2]
      - [Important Code Snippet]
   - [...]

4. Errors and fixes:
    - [Detailed description of error 1]:
      - [How you fixed the error]
      - [User feedback on the error if any]
    - [...]

5. Problem Solving:
   [Description of solved problems and ongoing troubleshooting]

6. All user messages:
    - [Detailed non tool use user message]
    - [...]

7. Pending Tasks:
   - [Task 1]
   - [Task 2]
   - [...]

8. Current Work:
   [Precise description of current work]

9. Optional Next Step:
   [Optional Next step to take]

</summary>
</example>

Please provide your summary based on the conversation so far, following this structure and ensuring precision and thoroughness in your response.

There may be additional summarization instructions provided in the included context. If so, remember to follow these instructions when creating the above summary. Examples of instructions include:
<example>
## Compact Instructions
When summarizing the conversation focus on typescript code changes and also remember the mistakes you made and how you fixed them.
</example>

<example>
# Summary instructions
When you are using compact - please focus on test output and code changes. Include file reads verbatim.
</example>
`

	const footer = "\nREMINDER: Do NOT call any tools. Respond with plain text only — an <analysis> block followed by a <summary> block. Tool calls will be rejected and you will fail the task."

	prompt := systemSection +
		"<conversation_history>\n" + historyXML + "\n</conversation_history>\n\n" +
		header + body

	if additionalInstructions != "" {
		prompt += "\nAdditional Instructions:\n" + additionalInstructions
	}
	prompt += footer
	return prompt
}

// extractCompactInstructions 从消息文本中提取用户自定义的 compact 指令。
// Claude Code 会将 CLAUDE.md 中 "## Compact Instructions" 或 "# Summary instructions" 小节
// 附加到 /compact 请求的最后一条 user 消息中。
func extractCompactInstructions(text string) string {
	markers := []string{"## Compact Instructions", "# Summary instructions", "## Summary instructions"}
	for _, marker := range markers {
		idx := strings.Index(text, marker)
		if idx != -1 {
			snippet := strings.TrimSpace(text[idx:])
			// 取到下一个 ## 标题或文本结束
			lines := strings.Split(snippet, "\n")
			var result []string
			for i, line := range lines {
				if i > 0 && strings.HasPrefix(strings.TrimSpace(line), "##") {
					break
				}
				result = append(result, line)
			}
			return strings.TrimSpace(strings.Join(result, "\n"))
		}
	}
	return ""
}
