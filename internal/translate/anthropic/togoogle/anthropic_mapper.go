// Anthropic → Google Agent Platform 请求映射器
// 将 Anthropic Messages API 格式转换为 GEAP GenerateContent API 格式
package togoogle

import (
	"strings"
	"sync"
)

// toolThoughtSigCache 跨请求保存 Gemini 3.x functionCall 携带的 thoughtSignature。
// key: tool_use_id（Anthropic 格式）→ value: thoughtSignature（Gemini 格式）
// Gemini 3.x 要求多轮 function calling 时在历史中带回 thoughtSignature，
// 否则返回 400 "Function call is missing a thought_signature"。
var toolThoughtSigCache sync.Map

// toolGeminiCallIDCache 跨请求保存 Gemini functionCall 返回的原生调用 ID。
// key: tool_use_id（Anthropic 格式）→ value: Gemini functionCall.id
// Gemini 要求 functionResponse 必须回传该 ID，否则多轮工具调用中模型无法正确匹配结果。
var toolGeminiCallIDCache sync.Map

// geapSafetySettings BLOCK_NONE 安全配置，针对所有文本内容类别
// 原因：Claude Code 频繁发送含代码、安全研究、命令行等内容，Gemini 默认阈值会误触安全过滤器
// 本网关作为 API 代理，上游客户端已自行承担内容责任，无需二次拦截
var geapSafetySettings = []map[string]interface{}{
	{"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "BLOCK_NONE"},
	{"category": "HARM_CATEGORY_DANGEROUS_CONTENT", "threshold": "BLOCK_NONE"},
	{"category": "HARM_CATEGORY_HARASSMENT", "threshold": "BLOCK_NONE"},
	{"category": "HARM_CATEGORY_SEXUALLY_EXPLICIT", "threshold": "BLOCK_NONE"},
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
	case "max", "ultra_code", "xhigh":
		return "HIGH"
	case "high":
		return "HIGH"
	case "medium":
		return "MEDIUM"
	case "low":
		return "LOW"
	default:
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
					// 只处理 text 类型的 system block，非文本块（如 image）静默跳过
					if m["type"] != "text" {
						continue
					}
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

	// 执行 Anthropic ContextManagement 编辑指令（清理旧 thinking 块、旧工具结果等），
	// 对齐 Anthropic 原生 API 在发往模型前的预处理行为。
	cleanedMsgs := applyContextManagementEdits(req.Messages, req.ContextManagement)
	mappedContents, _ := mapMessages(cleanedMsgs, model)
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
	// 扩展思考映射：Anthropic thinking 配置 → Gemini thinkingConfig（仅 Gemini 3.x）
	// 新格式（2026）：type="adaptive" + 顶层 effort 字段（"low"/"medium"/"high"/"max"/"ultra_code"）
	// 旧格式（向后兼容）：type="enabled" + budget_tokens
	// 禁用：type="disabled" → 映射到 "LOW"（保留基础推理，完全关闭降低质量）
	// includeThoughts:true 让 Gemini 在响应中返回 thought parts，
	// 流式/非流式处理器将其转换为 Anthropic thinking 内容块 + signature_delta
	switch {
	case req.Thinking != nil && (req.Thinking.Type == "enabled" || req.Thinking.Type == "adaptive"):
		// 客户端明确开启思考，按 effort 映射
		genConfig["thinkingConfig"] = map[string]interface{}{
			"includeThoughts": true,
			"thinkingLevel":   effortToThinkingLevel(req.Effort, req.Thinking.BudgetTokens),
		}
	case req.Thinking != nil && req.Thinking.Type == "disabled":
		// 客户端要求禁用 → 降到 LOW 而非完全关闭，2026 年客户端不会真正关闭 thinking
		genConfig["thinkingConfig"] = map[string]interface{}{
			"includeThoughts": true,
			"thinkingLevel":   "LOW",
		}
	default:
		// 无 thinking 配置：优先尊重顶层 effort，没有则默认 MEDIUM（与旧行为一致）
		genConfig["thinkingConfig"] = map[string]interface{}{
			"includeThoughts": true,
			"thinkingLevel":   effortToThinkingLevel(req.Effort, 0),
		}
	}
	if len(genConfig) > 0 {
		vertexReq["generationConfig"] = genConfig
	}

	if mappedTools := mapTools(req.Tools); mappedTools != nil {
		vertexReq["tools"] = mappedTools
		// thinkingConfig 已在上方 switch 中统一设置（ALL 分支），无需工具兜底
		// toolConfig 仅在有 tools 时设置，避免构造无效请求
		if mappedToolChoice := mapToolChoice(req.ToolChoice); mappedToolChoice != nil {
			vertexReq["toolConfig"] = mappedToolChoice
		}
	}

	// maxOutputTokens 补偿：
	// Anthropic max_tokens 仅计响应文本，thinking 由 budget_tokens 单独控制；
	// 而 Gemini 的 maxOutputTokens = thinking token + 响应 token 合并上限。
	// 若不补充思考预算，Gemini 在思考阶段就耗尽配额，导致正文为空（MAX_TOKENS + no parts）。
	// 策略：在 thinkingConfig 确定后，将对应 level 的估算开销叠加到 maxOutputTokens。
	// 上限：Gemini 3.x 模型最大输出为 65536 token，不得超过此值。
	const gemini3MaxOutputTokens = 65536
	if req.MaxTokens > 0 {
		if tc, ok := genConfig["thinkingConfig"].(map[string]interface{}); ok {
			if includeThoughts, _ := tc["includeThoughts"].(bool); includeThoughts {
				overhead := 8000 // AUTO/MEDIUM 默认开销
				switch tc["thinkingLevel"] {
				case "HIGH":
					// 客户端明确开启且有 budget_tokens 时，尊重客户端预算；否则用估算值
					if req.Thinking != nil && req.Thinking.BudgetTokens > 0 {
						overhead = req.Thinking.BudgetTokens
					} else {
						overhead = 16000
					}
				case "MEDIUM":
					overhead = 8000
				case "LOW":
					overhead = 2000
				}
				total := req.MaxTokens + overhead
				if total > gemini3MaxOutputTokens {
					total = gemini3MaxOutputTokens
				}
				genConfig["maxOutputTokens"] = total
				vertexReq["generationConfig"] = genConfig
			}
		}
	}

	// output_config 映射：将 Anthropic 结构化输出配置转换为 Gemini responseMimeType/responseSchema
	if req.OutputConfig != nil {
		switch req.OutputConfig.Format {
		case "json":
			genConfig["responseMimeType"] = "application/json"
			if req.OutputConfig.Schema != nil {
				genConfig["responseSchema"] = req.OutputConfig.Schema
			}
			vertexReq["generationConfig"] = genConfig
		}
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
