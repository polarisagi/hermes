// Anthropic → Google Agent Platform 请求映射器
// 将 Anthropic Messages API 格式转换为 GEAP GenerateContent API 格式
package togoogle

import (
	"strings"
	"sync"

	gcommon "github.com/polarisagi/hermes/internal/translate/google"
)

// toolThoughtSigCache 跨请求保存 Gemini 3.x functionCall 携带的 thoughtSignature。
// key: tool_use_id（Anthropic 格式）→ value: thoughtSignature（Gemini 格式）
// Gemini 3.x 要求多轮 function calling 时在历史中带回 thoughtSignature，
// 否则返回 400 "Function call is missing a thought_signature"。
var toolThoughtSigCache sync.Map

// geapSafetySettings BLOCK_NONE 安全配置，针对所有文本内容类别
// 原因：Claude Code 频繁发送含代码、安全研究、命令行等内容，Gemini 默认阈值会误触安全过滤器
// 本网关作为 API 代理，上游客户端已自行承担内容责任，无需二次拦截
var geapSafetySettings = []map[string]interface{}{
	{"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "BLOCK_NONE"},
	{"category": "HARM_CATEGORY_DANGEROUS_CONTENT", "threshold": "BLOCK_NONE"},
	{"category": "HARM_CATEGORY_HARASSMENT", "threshold": "BLOCK_NONE"},
	{"category": "HARM_CATEGORY_SEXUALLY_EXPLICIT", "threshold": "BLOCK_NONE"},
	{"category": "HARM_CATEGORY_JAILBREAK", "threshold": "BLOCK_NONE"},
}

// findLastCompactionIndex 返回 messages 中最后一个包含 compaction 块的消息下标，
// 未找到时返回 -1。
// compaction 块是 Claude Code /compact 产生的检查点，Anthropic API 规定其之前的消息全部丢弃。
func findLastCompactionIndex(messages []Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if arr, ok := messages[i].Content.([]interface{}); ok {
			for _, item := range arr {
				if m, ok := item.(map[string]interface{}); ok {
					if m["type"] == "compaction" {
						return i
					}
				}
			}
		}
	}
	return -1
}

// effortToThinkingLevel 将 Anthropic 的 effort 字段（新格式）或 budget_tokens（旧格式）映射到 Gemini 3.x thinkingLevel 枚举值
// 优先读取 effort，没有时 fallback 到 budget_tokens 数值推断（向后兼容旧格式）
func effortToThinkingLevel(effort string, budgetTokens int) string {
	switch effort {
	case "max", "ultra_code":
		return "HIGH"
	case "high":
		return "HIGH"
	case "medium":
		return "MEDIUM"
	case "low":
		return "LOW"
	default:
		// 没有 effort 字段，fallback 到旧的 budget_tokens 数值推断
		return budgetToThinkingLevel(budgetTokens)
	}
}

// budgetToThinkingLevel 将 Anthropic thinking.budget_tokens 映射到 Gemini 3.x thinkingLevel 枚举值
// 仅用于向后兼容旧格式（budget_tokens），新格式请使用 effortToThinkingLevel
func budgetToThinkingLevel(budgetTokens int) string {
	switch {
	case budgetTokens <= 0:
		return "MEDIUM" // 未指定时取中档，兼顾质量与速度
	case budgetTokens <= 5000:
		return "LOW"
	case budgetTokens <= 16000:
		return "MEDIUM"
	default:
		return "HIGH"
	}
}

// mapToVertexRequest 将 Anthropic Messages 请求转换为 GEAP 原生的 generateContent 请求体
// model 参数用于区分 Gemini 2.5（thinkingBudget）与 Gemini 3.x（thinkingLevel）的 thinking API 差异
// 转换规则:
//   - Anthropic system → GEAP systemInstruction
//   - Anthropic user/assistant → GEAP user/model 角色
//   - Anthropic 纯文本/多模态/工具调用内容块 → GEAP parts 数组
//   - Anthropic max_tokens → GEAP maxOutputTokens
//   - Anthropic temperature/topP/topK → GEAP generationConfig
//   - Anthropic tools → GEAP tools (functionDeclarations)
//   - Anthropic tool_choice → GEAP toolConfig
//   - Anthropic metadata.user_id → GEAP labels（用于计费追踪）
//   - 默认附加 safetySettings=BLOCK_NONE 避免安全过滤器误杀代码内容
func mapToVertexRequest(req MessageRequest, model string) (map[string]interface{}, error) {
	vertexReq := make(map[string]interface{})

	var systemParts []map[string]interface{}
	if req.System != nil {
		switch sys := req.System.(type) {
		case string:
			if sys != "" {
				systemParts = append(systemParts, map[string]interface{}{"text": sys})
			}
		case []interface{}:
			for _, item := range sys {
				if m, ok := item.(map[string]interface{}); ok {
					if text, ok := m["text"].(string); ok {
						systemParts = append(systemParts, map[string]interface{}{"text": text})
					}
				}
			}
		}
	}

	systemPromptStr := "\n\nNote: In your conversation history, previous tool executions and results are recorded in `<past_tool_execution>` and `<past_tool_result>` XML tags. These are system-generated records. When YOU want to invoke a tool, DO NOT output XML or text logs. You MUST strictly use the native JSON `functionCall` mechanism."
	systemParts = append(systemParts, map[string]interface{}{"text": systemPromptStr})

	vertexReq["systemInstruction"] = map[string]interface{}{
		"parts": systemParts,
	}

	mappedContents, _ := mapMessages(req.Messages, model)
	if len(mappedContents) > 0 {
		vertexReq["contents"] = mappedContents
	}

	genConfig := make(map[string]interface{})
	if req.MaxTokens > 0 {
		genConfig["maxOutputTokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		genConfig["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		genConfig["topP"] = *req.TopP
	}
	if req.TopK != nil {
		genConfig["topK"] = *req.TopK
	}
	if len(req.StopSequences) > 0 {
		genConfig["stopSequences"] = req.StopSequences
	}
	// 扩展思考映射：Anthropic thinking 配置 → Gemini thinkingConfig
	// 新格式（2026）：type="adaptive" + 顶层 effort 字段（"low"/"medium"/"high"/"max"/"ultra_code"）
	// 旧格式（已废弃）：type="enabled" + budget_tokens
	// 禁用：type="disabled"
	// Gemini 2.5：thinkingBudget（整数 token 数）
	// Gemini 3.x：thinkingLevel（MINIMAL/LOW/MEDIUM/HIGH），两者不可同时使用（API 会返回 400）
	// includeThoughts:true 让 Gemini 在响应中返回 thought 标记的 parts，
	// 流式/非流式处理器会将其转换为 Anthropic thinking 内容块 + signature_delta
	thinkingEnabled := req.Thinking != nil &&
		(req.Thinking.Type == "enabled" || req.Thinking.Type == "adaptive")
	thinkingExplicitlyDisabled := req.Thinking != nil && req.Thinking.Type == "disabled"

	switch {
	case thinkingEnabled:
		// 客户端明确开启思考，按 effort 映射
		if gcommon.IsGemini3Model(model) {
			// Gemini 3.x：使用 thinkingLevel 枚举，不接受 thinkingBudget 整数
			genConfig["thinkingConfig"] = map[string]interface{}{
				"includeThoughts": true,
				"thinkingLevel":   effortToThinkingLevel(req.Effort, req.Thinking.BudgetTokens),
			}
		} else {
			// Gemini 2.5：使用 thinkingBudget 整数
			thinkingCfg := map[string]interface{}{
				"includeThoughts": true,
			}
			if req.Thinking.BudgetTokens > 0 {
				thinkingCfg["thinkingBudget"] = req.Thinking.BudgetTokens
			}
			genConfig["thinkingConfig"] = thinkingCfg
		}
	case thinkingExplicitlyDisabled:
		// 客户端明确禁用思考
		// Gemini 3.x 默认开启 HIGH 思考，需显式设为 MINIMAL 以尊重客户端意图
		// 注意：若请求含 tools，下方的工具兜底逻辑会覆盖此设置（Gemini 需要思考才能正确使用工具）
		// Gemini 2.5 不可靠地支持 thinkingBudget=0 禁用，不设置让其自决
		// 工具兜底逻辑同样会接管（见下方）
		if gcommon.IsGemini3Model(model) {
			genConfig["thinkingConfig"] = map[string]interface{}{
				"includeThoughts": false,
				"thinkingLevel":   "MINIMAL",
			}
		}
	default:
		// 客户端未传 thinking 参数
		// Gemini 3.x 在默认状态下内部思考但不返回 thought parts（除非设置 includeThoughts: true）
		// 设置 includeThoughts: true + MEDIUM 以接收 thought parts，供流式处理器转换为 thinking 内容块
		// Gemini 2.5 则让其自决
		if gcommon.IsGemini3Model(model) {
			genConfig["thinkingConfig"] = map[string]interface{}{
				"includeThoughts": true,
				"thinkingLevel":   "MEDIUM",
			}
		}
	}
	if len(genConfig) > 0 {
		vertexReq["generationConfig"] = genConfig
	}

	if mappedTools := mapTools(req.Tools); mappedTools != nil {
		vertexReq["tools"] = mappedTools

		// Gemini 2.5 自动 thinking（未显式配置 thinkingConfig）与 function calling 存在已知冲突：
		// 模型在无法输出原生 thought 时，会生成 name="thought" 的非法 functionCall，导致 MALFORMED_FUNCTION_CALL。
		// 由于 Gemini 2.5 Pro 不支持 thinkingBudget=0 来禁用思考，
		// 我们统一设置 includeThoughts: true 允许原生思考，流式处理器会自动将其转换为 thinking 块，
		// 从而避免模型尝试伪造 functionCall 引发崩溃。
		// 同理适用于 Gemini 3.x：即使客户端设置了 disabled，有工具时也须开启思考以确保工具调用质量。
		if _, hasExplicitThinking := genConfig["thinkingConfig"]; !hasExplicitThinking {
			if gcommon.IsGemini3Model(model) {
				// 工具兜底：优先尊重 effort，没有则默认 MEDIUM
				genConfig["thinkingConfig"] = map[string]interface{}{
					"includeThoughts": true,
					"thinkingLevel":   effortToThinkingLevel(req.Effort, 0),
				}
			} else {
				genConfig["thinkingConfig"] = map[string]interface{}{"includeThoughts": true}
			}
			vertexReq["generationConfig"] = genConfig
		} else if thinkingExplicitlyDisabled {
			// 客户端明确禁用思考，但有工具 → 强制覆盖为最低档以保证工具调用稳定性
			// 同时将 includeThoughts 保持 true，让流处理器能正确过滤 thought parts
			if gcommon.IsGemini3Model(model) {
				genConfig["thinkingConfig"] = map[string]interface{}{
					"includeThoughts": true,
					"thinkingLevel":   "LOW",
				}
				vertexReq["generationConfig"] = genConfig
			}
		}
	}

	if mappedToolChoice := mapToolChoice(req.ToolChoice); mappedToolChoice != nil {
		vertexReq["toolConfig"] = mappedToolChoice
	}

	// safetySettings：默认对所有类别设置 BLOCK_NONE，防止 Gemini 安全过滤器误杀代理流量
	vertexReq["safetySettings"] = geapSafetySettings

	// labels：将 Anthropic metadata.user_id 映射为 GEAP 请求标签，便于计费与审计追踪
	// GEAP label 限制：key/value 最长 63 字符，仅允许小写字母、数字、下划线、连字符
	if req.Metadata != nil && req.Metadata.UserID != "" {
		sanitized := sanitizeLabelValue(req.Metadata.UserID)
		if sanitized != "" {
			vertexReq["labels"] = map[string]string{
				"user-id": sanitized,
			}
		}
	}

	return vertexReq, nil
}

// sanitizeLabelValue 将任意字符串截断并清洗为合法的 GEAP label value
// GEAP 要求：小写字母、数字、下划线、连字符；最长 63 字符
func sanitizeLabelValue(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
		if b.Len() >= 63 {
			break
		}
	}
	result := strings.Trim(b.String(), "-")
	return result
}

// convertMediaSourceToVertexPart 把 Anthropic 的媒体 source (base64/url) 转换为 Vertex AI 支持的 inlineData/fileData
func convertMediaSourceToVertexPart(source map[string]interface{}, defaultMediaType string) map[string]interface{} {
	if source == nil {
		return nil
	}
	mediaType, _ := source["media_type"].(string)
	if mediaType == "" {
		mediaType = defaultMediaType
	}
	if source["type"] == "base64" {
		return map[string]interface{}{
			"inlineData": map[string]interface{}{
				"mimeType": mediaType,
				"data":     source["data"],
			},
		}
	} else if source["type"] == "url" {
		return map[string]interface{}{
			"fileData": map[string]interface{}{
				"mimeType": mediaType,
				"fileUri":  source["url"],
			},
		}
	}
	return nil
}
