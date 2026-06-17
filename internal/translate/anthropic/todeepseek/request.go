package todeepseek

import (
	"log/slog"
	"strings"

	"github.com/polarisagi/hermes/internal/translate"
	anthr "github.com/polarisagi/hermes/internal/translate/anthropic"
)

// adaptForDeepSeek 将 Claude Code 发出的 Anthropic 请求适配为 DeepSeek Anthropic 兼容格式。
//
// 适配规则（基于 DeepSeek 2026年5月官方文档）：
//
//  0. 模型名映射（仅当模型仍是 claude-* 名时）：
//     claude-opus-*   → deepseek-v4-pro
//     claude-sonnet-* → deepseek-v4-pro（DeepSeek 默认映射到 v4-flash，体验较差，此处修正）
//     claude-haiku-*  → deepseek-v4-flash
//
//  1. thinking.type 映射：
//     "adaptive" → "enabled"（DeepSeek 不支持 adaptive，Claude 2026 新增此类型）
//     "enabled"  → "enabled"（清除 budget_tokens，DeepSeek 不使用此字段）
//     "disabled" → 保持不变
//     其他/nil   → 移除 thinking 字段（让 DeepSeek 使用默认行为）
//
//  2. 思考强度映射（顶层 effort → output_config.effort）：
//     Claude Code 用顶层 effort 字段；DeepSeek 用 output_config.effort
//     max / ultra_code / xhigh → "max"
//     high / medium / low / "" → "high"
//     若已有 output_config，规范化其 effort 值
//     若开启思考但无 effort，默认 "high"
//
//  3. 消息历史过滤：
//     redacted_thinking → 删除（DeepSeek 不支持；这是 Claude 的脱敏思考块，无法还原）
//     thinking（assistant 消息 + 有工具调用）→ 保留（DeepSeek Anthropic API 支持）
//     thinking（assistant 消息 + 无工具调用）→ 删除（DeepSeek 文档：非工具轮会被忽略）
//     image / document → 删除并警告（DeepSeek Anthropic API 不支持多模态）
//
//  4. 清理 Claude Code 专属字段（DeepSeek 不识别，传入可能导致 400 错误）：
//     context_management
func adaptForDeepSeek(req *anthr.MessageRequest) {
	if strings.HasPrefix(strings.ToLower(req.Model), "claude-") {
		req.Model = mapModelForDeepSeek(req.Model)
	}

	if req.Thinking != nil {
		switch req.Thinking.Type {
		case "adaptive", "enabled":
			req.Thinking = &anthr.ThinkingConfig{Type: "enabled"}
		case "disabled":
			// 保持不变
		default:
			req.Thinking = nil
		}
	} else if req.Effort != "" {
		req.Thinking = &anthr.ThinkingConfig{Type: "enabled"}
	}

	if req.Effort != "" {
		req.OutputConfig = &anthr.OutputConfig{Effort: translate.MapEffortToDeepSeek(req.Effort)}
		req.Effort = ""
	} else if req.OutputConfig != nil && req.OutputConfig.Effort != "" {
		req.OutputConfig.Effort = translate.MapEffortToDeepSeek(req.OutputConfig.Effort)
	}

	if req.Thinking != nil && req.Thinking.Type == "enabled" &&
		(req.OutputConfig == nil || req.OutputConfig.Effort == "") {
		req.OutputConfig = &anthr.OutputConfig{Effort: "high"}
	}

	if anthr.IsCompactRequest(req) {
		promptInjection := anthr.BuildCompactPrompt(req)
		req.Messages = []anthr.Message{{
			Role:    "user",
			Content: promptInjection,
		}}
		req.System = nil
		req.Tools = nil
		req.ToolChoice = nil
		var temp float64 = 0.0
		req.Temperature = &temp
	} else {
		req.Messages = filterMessagesForDeepSeek(req.Messages)
	}
	
	req.ContextManagement = nil
}

// mapModelForDeepSeek 将 Anthropic claude-* 模型名映射到对应的 DeepSeek 模型。
//
// 映射策略（修正 DeepSeek 官方的自动映射）：
//   - claude-opus-*   → deepseek-v4-pro（强推理，旗舰级）
//   - claude-sonnet-* → deepseek-v4-pro（DeepSeek 官方默认映射到 v4-flash，体验明显差，此处修正为 v4-pro）
//   - claude-haiku-*  → deepseek-v4-flash（轻量快速，与官方映射一致）
//   - 其他（已是 deepseek-* 等）→ 原样返回
func mapModelForDeepSeek(model string) string {
	lower := strings.ToLower(model)
	switch {
	case strings.HasPrefix(lower, "claude-opus"):
		return "deepseek-v4-pro"
	case strings.HasPrefix(lower, "claude-sonnet"):
		return "deepseek-v4-pro"
	case strings.HasPrefix(lower, "claude-haiku"):
		return "deepseek-v4-flash"
	default:
		return model
	}
}

func filterMessagesForDeepSeek(messages []anthr.Message) []anthr.Message {
	result := make([]anthr.Message, 0, len(messages))
	for _, msg := range messages {
		result = append(result, filterMessageContentForDeepSeek(msg))
	}
	return result
}

func filterMessageContentForDeepSeek(msg anthr.Message) anthr.Message {
	blocks, ok := msg.Content.([]interface{})
	if !ok {
		return msg
	}

	hasToolUse := false
	for _, b := range blocks {
		if blk, ok := b.(map[string]interface{}); ok && blk["type"] == "tool_use" {
			hasToolUse = true
			break
		}
	}

	var newBlocks []interface{}
	for _, b := range blocks {
		blk, ok := b.(map[string]interface{})
		if !ok {
			newBlocks = append(newBlocks, b)
			continue
		}

		switch blk["type"] {
		case "redacted_thinking":
			// DeepSeek 不支持，丢弃

		case "thinking":
			if msg.Role != "assistant" {
				break
			}
			if hasToolUse {
				newBlocks = append(newBlocks, blk)
			}
			// 非工具调用轮：丢弃

		case "image", "document":
			slog.Warn("[todeepseek] DeepSeek 不支持的 content 类型，已过滤",
				"role", msg.Role,
				"type", blk["type"],
			)

		default:
			newBlocks = append(newBlocks, blk)
		}
	}

	if len(newBlocks) == 0 && len(blocks) > 0 {
		newBlocks = []interface{}{map[string]interface{}{
			"type": "text",
			"text": "",
		}}
	}

	msg.Content = newBlocks
	return msg
}

func thinkingType(t *anthr.ThinkingConfig) string {
	if t == nil {
		return "none"
	}
	return t.Type
}

func outputConfigEffort(oc *anthr.OutputConfig) string {
	if oc == nil {
		return "none"
	}
	return oc.Effort
}
