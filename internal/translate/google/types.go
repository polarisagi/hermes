// Package google 定义 Google Gemini API 的公共工具函数。
//
// 该包作为共享协议层，被 toanthropic、toopenai、togoogle 等子翻译器共同引用。
// 所有与 Gemini 协议格式相关的公共函数都应放在此包中。
package google

import (
	"strings"
)

// IsGemini3Model 判断目标模型是否为 Gemini 3.x 系列。
//
// Gemini 3.x 的 thinkingConfig 使用 thinkingLevel（LOW/MEDIUM/HIGH）
// 替代 Gemini 2.5 的 thinkingBudget（整数 token 数），两者不可混用（API 会返回 400）。
func IsGemini3Model(model string) bool {
	return strings.HasPrefix(strings.ToLower(model), "gemini-3")
}

// ExtractSystemInstruction 从 Gemini 请求体中提取 systemInstruction 文本。
//
// Gemini 格式：{"systemInstruction": {"parts": [{"text": "..."}]}}
func ExtractSystemInstruction(gReq map[string]interface{}) string {
	si, ok := gReq["systemInstruction"].(map[string]interface{})
	if !ok {
		return ""
	}
	return ExtractPartsText(si["parts"])
}

// ExtractPartsText 从 Gemini parts 数组中提取所有文本内容，用换行符拼接。
//
// parts 格式：[{"text": "..."}, {"text": "..."}]
func ExtractPartsText(partsRaw interface{}) string {
	parts, ok := partsRaw.([]interface{})
	if !ok {
		return ""
	}
	var sb strings.Builder
	for _, p := range parts {
		part, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		if text, ok := part["text"].(string); ok && text != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(text)
		}
	}
	return sb.String()
}

// ThinkingLevelToEffort 将 Gemini thinkingLevel 枚举映射到通用 effort 字符串。
//
// Gemini 2.5 枚举：MINIMAL / LOW / MEDIUM / HIGH
// Gemini 3.x 枚举：LOW / MEDIUM / HIGH（无 MINIMAL）
// 两者的 LOW/MINIMAL 都映射到 effort:low。
func ThinkingLevelToEffort(level string) string {
	switch strings.ToUpper(level) {
	case "NONE":
		return "none"
	case "MINIMAL", "LOW":
		return "low"
	case "MEDIUM":
		return "medium"
	case "HIGH":
		return "high"
	default:
		return "medium"
	}
}

// ThinkingBudgetToEffort 将 Gemini 2.5 thinkingBudget token 数映射到 effort。
func ThinkingBudgetToEffort(budget int) string {
	switch {
	case budget <= 0:
		return "none"
	case budget <= 4000:
		return "low"
	case budget <= 16000:
		return "medium"
	case budget <= 32000:
		return "high"
	default:
		return "xhigh"
	}
}
