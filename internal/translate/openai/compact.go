package openai

import (
	"fmt"
	"strings"
)

// IsCompactRequestOpenAI 检测 OpenAI Chat Completions 格式请求是否为 compact/上下文压缩请求。
//
// 检测信号（满足任一即判定）：
//  1. __hermes_compact: true（由 ResponsesAPIToChatCompletions 从 truncation:"auto" 转换而来）
//  2. 最后一条 user 消息含 ≥2 个压缩特征关键词（与 anthr.IsCompactRequest 保持一致）
func IsCompactRequestOpenAI(req map[string]interface{}) bool {
	if v, ok := req["__hermes_compact"].(bool); ok && v {
		return true
	}
	msgs, ok := req["messages"].([]interface{})
	if !ok || len(msgs) == 0 {
		return false
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		msg, ok := msgs[i].(map[string]interface{})
		if !ok {
			continue
		}
		if role, _ := msg["role"].(string); role != "user" {
			continue
		}
		content := ExtractTextFromContent(msg["content"])
		features := 0
		if strings.Contains(content, "TEXT ONLY") {
			features++
		}
		if strings.Contains(strings.ToLower(content), "summary") {
			features++
		}
		if strings.Contains(content, "Do NOT call any tools") {
			features++
		}
		if strings.Contains(content, "<analysis>") {
			features++
		}
		if strings.Contains(content, "<summary>") {
			features++
		}
		return features >= 2
	}
	return false
}

// BuildCompactPromptFromOpenAI 将 OpenAI messages 数组转换为 compact 摘要 prompt（单条 user 消息文本）。
//
// 输出结构与 anthr.BuildCompactPrompt 保持一致，以便 Google/DeepSeek/通用后端均能正确生成摘要。
// tool 消息只记录工具名，跳过原始输出内容，避免第三方模型将冗长工具结果原样写入摘要。
func BuildCompactPromptFromOpenAI(messages []interface{}) string {
	var sb strings.Builder
	var systemParts []string

	for _, item := range messages {
		msg, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)

		switch role {
		case "system", "developer":
			if t := ExtractTextFromContent(msg["content"]); t != "" {
				systemParts = append(systemParts, t)
			}
		case "user":
			sb.WriteString(fmt.Sprintf("<turn role=\"%s\">\n", role))
			sb.WriteString(ExtractTextFromContent(msg["content"]))
			sb.WriteString("\n</turn>\n")
		case "assistant":
			sb.WriteString("<turn role=\"assistant\">\n")
			if c, ok := msg["content"].(string); ok && c != "" {
				sb.WriteString(c)
				sb.WriteString("\n")
			}
			// tool_calls 只记录工具名，不记录完整 JSON arguments
			if tcs, ok := msg["tool_calls"].([]interface{}); ok {
				for _, tc := range tcs {
					if tcMap, ok := tc.(map[string]interface{}); ok {
						if fn, ok := tcMap["function"].(map[string]interface{}); ok {
							name, _ := fn["name"].(string)
							sb.WriteString(fmt.Sprintf("[Tool: %s]\n", name))
						}
					}
				}
			}
			sb.WriteString("</turn>\n")
		// role=tool（工具执行结果）跳过：原始输出极为冗长，模型会原样抄写进摘要
		}
	}

	historyXML := sb.String()

	var systemSection string
	if len(systemParts) > 0 {
		systemSection = fmt.Sprintf("<system_context>\n%s\n</system_context>\n\n", strings.Join(systemParts, "\n"))
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
   - Specific details like file names, full code snippets, function signatures, file edits
   - Errors that you ran into and how you fixed them
   - Pay special attention to specific user feedback, especially if the user told you to do something differently.
   - Note any security-relevant instructions or constraints the user stated. These MUST be preserved verbatim in the summary.
2. Double-check for technical accuracy and completeness.

Your summary should include the following sections:
1. Primary Request and Intent
2. Key Technical Concepts
3. Files and Code Sections
4. Errors and fixes
5. Problem Solving
6. All user messages
7. Pending Tasks
8. Current Work
9. Optional Next Step
`

	const footer = "\nREMINDER: Do NOT call any tools. Respond with plain text only — an <analysis> block followed by a <summary> block. Tool calls will be rejected and you will fail the task."

	return systemSection +
		"<conversation_history>\n" + historyXML + "\n</conversation_history>\n\n" +
		header + body + footer
}
