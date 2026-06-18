package togoogle

import (
	"encoding/json"
	"log/slog"
)

// applyContextManagementEdits 执行 Anthropic ContextManagement 编辑指令。
//
// Claude Code 在发送请求时会附带 context_management.edits 字段，
// Anthropic 原生 API 在转发给模型前会按这些指令预处理历史消息。
// 网关代理 Gemini 时必须自行执行这些指令，否则会将超出预期的 context（旧 thinking、旧工具结果）
// 全量发给 Gemini，导致 prompt 冗余、签名对不上、乃至空响应等问题。
//
// 当前实现：
//   - clear_thinking_20251015：清理旧 thinking/redacted_thinking 块，保留最后 keepN 个含思考的 model turn
//   - clear_tool_uses_20250919：清理旧 tool_result 内容，保留最后 keepN 个工具调用/结果对
//   - compact_20260112：已由 IsCompactRequest 处理，此处跳过
func applyContextManagementEdits(messages []Message, cm *ContextManagement) []Message {
	if cm == nil || len(cm.Edits) == 0 {
		return messages
	}
	for _, edit := range cm.Edits {
		switch edit.Type {
		case "clear_thinking_20251015":
			keepN := parseKeepN(edit.Keep, "thinking_turns", 1)
			var clearedBlocks int
			messages, clearedBlocks = clearOldThinkingBlocks(messages, keepN)
			slog.Info("🧹 [ContextMgmt] clear_thinking 执行完毕",
				"keep_n", keepN,
				"thinking_turns_total", len(messages),
				"thinking_blocks_cleared", clearedBlocks,
			)
		case "clear_tool_uses_20250919":
			keepN := parseKeepN(edit.Keep, "tool_uses", 3)
			messages = clearOldToolResultContents(messages, keepN)
			slog.Info("🧹 [ContextMgmt] clear_tool_uses 执行完毕",
				"keep_n", keepN,
			)
		case "compact_20260112":
			// compact 已通过 IsCompactRequest 逻辑处理，此处忽略
		}
	}
	return messages
}

// parseKeepN 解析 ContextEdit.Keep 字段，返回保留数量。
//
// Keep 字段有两种格式：
//   - 旧格式（string）："last_n"——直接使用 defaultN
//   - 新格式（object）：{"type": "thinking_turns"|"tool_uses", "value": N}——使用 value
func parseKeepN(raw json.RawMessage, _ string, defaultN int) int {
	if len(raw) == 0 {
		return defaultN
	}
	var obj struct {
		Value int `json:"value"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.Value > 0 {
		return obj.Value
	}
	return defaultN
}

// clearOldThinkingBlocks 清理历史中旧 thinking/redacted_thinking 块。
//
// 策略：找出所有含 thinking 块的 assistant turn，保留最后 keepN 个，
// 清理其余 turn 中的 thinking/redacted_thinking 内容块（保留 text/tool_use 等其他块）。
// 返回清理后的消息列表及实际清理的 thinking 块总数（用于日志）。
func clearOldThinkingBlocks(messages []Message, keepN int) ([]Message, int) {
	// 收集含 thinking 块的 assistant turn 下标
	var thinkingIdxs []int
	for i, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		if arr, ok := msg.Content.([]interface{}); ok {
			for _, item := range arr {
				if m, ok := item.(map[string]interface{}); ok {
					t, _ := m["type"].(string)
					if t == "thinking" || t == "redacted_thinking" {
						thinkingIdxs = append(thinkingIdxs, i)
						break
					}
				}
			}
		}
	}

	if len(thinkingIdxs) <= keepN {
		return messages, 0 // 未超限，无需清理
	}

	// 最旧的几个需要清理
	clearSet := make(map[int]bool, len(thinkingIdxs)-keepN)
	for _, idx := range thinkingIdxs[:len(thinkingIdxs)-keepN] {
		clearSet[idx] = true
	}

	result := make([]Message, len(messages))
	copy(result, messages)
	clearedBlocks := 0
	for idx := range clearSet {
		msg := result[idx]
		arr, ok := msg.Content.([]interface{})
		if !ok {
			continue
		}
		filtered := make([]interface{}, 0, len(arr))
		for _, item := range arr {
			if m, ok := item.(map[string]interface{}); ok {
				t, _ := m["type"].(string)
				if t == "thinking" || t == "redacted_thinking" {
					clearedBlocks++
					continue // 清除
				}
			}
			filtered = append(filtered, item)
		}
		result[idx] = Message{Role: msg.Role, Content: filtered}
	}
	return result, clearedBlocks
}

// clearOldToolResultContents 清理旧 tool_result 的返回内容，保留最后 keepN 个工具调用结果对。
//
// 策略：按 tool_result 出现顺序排列，保留最新的 keepN 个，
// 将其余 tool_result 的 content 替换为占位文本，结构保留（Gemini 需要 functionResponse 与 functionCall 一一对应）。
func clearOldToolResultContents(messages []Message, keepN int) []Message {
	// 按顺序收集所有 tool_result 块的位置
	type loc struct{ msgIdx, blkIdx int }
	var locs []loc
	for i, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		arr, ok := msg.Content.([]interface{})
		if !ok {
			continue
		}
		for j, item := range arr {
			if m, ok := item.(map[string]interface{}); ok {
				if m["type"] == "tool_result" {
					locs = append(locs, loc{i, j})
				}
			}
		}
	}

	if len(locs) <= keepN {
		return messages // 未超限
	}

	// 需要清理内容的位置集合
	type key struct{ m, b int }
	clearSet := make(map[key]bool, len(locs)-keepN)
	for _, l := range locs[:len(locs)-keepN] {
		clearSet[key{l.msgIdx, l.blkIdx}] = true
	}

	// 找出需要修改的消息下标
	modMsgs := make(map[int]bool)
	for k := range clearSet {
		modMsgs[k.m] = true
	}

	result := make([]Message, len(messages))
	copy(result, messages)
	for i := range modMsgs {
		msg := result[i]
		arr, ok := msg.Content.([]interface{})
		if !ok {
			continue
		}
		newArr := make([]interface{}, len(arr))
		copy(newArr, arr)
		for j := range newArr {
			if !clearSet[key{i, j}] {
				continue
			}
			m, ok := newArr[j].(map[string]interface{})
			if !ok {
				continue
			}
			// 深拷贝并替换 content，保留 tool_use_id 等元数据
			newM := make(map[string]interface{}, len(m))
			for k, v := range m {
				newM[k] = v
			}
			newM["content"] = "[tool output cleared to save context]"
			newArr[j] = newM
		}
		result[i] = Message{Role: msg.Role, Content: newArr}
	}
	return result
}
