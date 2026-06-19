package toanthropic

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/polarisagi/hermes/internal/translate"
	gcommon "github.com/polarisagi/hermes/internal/translate/google"
)

func mapGenerationConfig(genCfg map[string]interface{}, aReq map[string]interface{}) {
	if genCfg == nil {
		return
	}
	if v, ok := genCfg["maxOutputTokens"].(float64); ok && v > 0 {
		aReq["max_tokens"] = int(v)
	}
	if v, ok := genCfg["temperature"].(float64); ok {
		aReq["temperature"] = v
	}
	if v, ok := genCfg["topP"].(float64); ok {
		aReq["top_p"] = v
	}
	if v, ok := genCfg["topK"].(float64); ok {
		k := int(v)
		aReq["top_k"] = k
	}
	if v, ok := genCfg["stopSequences"].([]interface{}); ok && len(v) > 0 {
		var seqs []string
		for _, s := range v {
			if str, ok := s.(string); ok {
				seqs = append(seqs, str)
			}
		}
		if len(seqs) > 0 {
			aReq["stop_sequences"] = seqs
		}
	}
}

// mapThinkingConfig 将 Gemini thinkingConfig 映射为 Anthropic thinking + effort 字段
//
// Gemini 2.5：thinkingBudget（整数）
// Gemini 3.x：thinkingLevel（MINIMAL/LOW/MEDIUM/HIGH/NONE）
// 两者都映射到 Anthropic thinking:{type:"adaptive"} + 顶层 effort
func mapThinkingConfig(genCfg map[string]interface{}, aReq map[string]interface{}) {
	if genCfg == nil {
		return
	}
	tc, ok := genCfg["thinkingConfig"].(map[string]interface{})
	if !ok {
		return
	}

	// thinkingLevel（Gemini 3.x）优先
	if level, ok := tc["thinkingLevel"].(string); ok {
		switch strings.ToUpper(level) {
		case "NONE":
			aReq["thinking"] = map[string]interface{}{"type": "disabled"}
			return
		case "MINIMAL", "LOW":
			aReq["thinking"] = map[string]interface{}{"type": "adaptive"}
			aReq["effort"] = "low"
			return
		case "MEDIUM":
			aReq["thinking"] = map[string]interface{}{"type": "adaptive"}
			aReq["effort"] = "medium"
			return
		case "HIGH":
			aReq["thinking"] = map[string]interface{}{"type": "adaptive"}
			aReq["effort"] = "high"
			return
		}
	}

	// thinkingBudget（Gemini 2.5）
	// 2026年：新版 Anthropic 模型（Opus 4.6+）已废弃 budget_tokens，统一映射到 adaptive + effort
	if budget, ok := tc["thinkingBudget"].(float64); ok {
		if budget <= 0 {
			aReq["thinking"] = map[string]interface{}{"type": "disabled"}
			return
		}
		aReq["thinking"] = map[string]interface{}{"type": "adaptive"}
		aReq["effort"] = translate.MapEffortToAnthropic(gcommon.ThinkingBudgetToEffort(int(budget)))
		return
	}

	// 只设置了 includeThoughts=true 但无其他配置：默认开启 medium
	if incl, ok := tc["includeThoughts"].(bool); ok && incl {
		aReq["thinking"] = map[string]interface{}{"type": "adaptive"}
		aReq["effort"] = "medium"
	}
}

// convertContentsToMessages 将 Gemini contents 数组转换为 Anthropic messages 数组
//
// 关键规则：
//  1. role:user → user，role:model → assistant
//  2. functionCall part → assistant 消息中的 tool_use block
//  3. functionResponse part → user 消息中的 tool_result block
//  4. thought=true part → thinking block（含 thoughtSignature 转为 signature）
//  5. inlineData / fileData → image block
//  6. 连续同角色消息需合并（Anthropic 要求 user/assistant 交替）
func convertContentsToMessages(contentsRaw interface{}) []map[string]interface{} {
	contents, ok := contentsRaw.([]interface{})
	if !ok {
		return []map[string]interface{}{}
	}

	// 先建立 functionCall name → pseudo-ID 映射（Anthropic 需要 tool_use_id）
	// Gemini 不携带 call ID，用 "call_{name}_{index}" 作为稳定 ID
	callCounter := map[string]int{}

	var msgs []map[string]interface{}
	for _, c := range contents {
		content, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		role := "user"
		if r, ok := content["role"].(string); ok && r == "model" {
			role = "assistant"
		}
		parts, _ := content["parts"].([]interface{})
		blocks := convertPartsToBlocks(parts, role, callCounter)
		if len(blocks) == 0 {
			continue
		}

		msgs = append(msgs, map[string]interface{}{
			"role":    role,
			"content": blocks,
		})
	}

	// 合并连续同角色消息
	msgs = mergeConsecutiveSameRole(msgs)
	// 确保以 user 消息结尾（若以 assistant 结尾，Anthropic 会报错）
	if len(msgs) > 0 && msgs[len(msgs)-1]["role"] == "assistant" {
		msgs = append(msgs, map[string]interface{}{
			"role":    "user",
			"content": []map[string]interface{}{{"type": "text", "text": "Please continue."}},
		})
	}
	return msgs
}

func convertPartsToBlocks(parts []interface{}, _ string, callCounter map[string]int) []map[string]interface{} {
	var blocks []map[string]interface{}
	for _, p := range parts {
		part, ok := p.(map[string]interface{})
		if !ok {
			continue
		}

		// thought=true → thinking block
		if isThought, _ := part["thought"].(bool); isThought {
			text, _ := part["text"].(string)
			sig, _ := part["thoughtSignature"].(string)
			block := map[string]interface{}{
				"type":     "thinking",
				"thinking": text,
			}
			if sig != "" {
				block["signature"] = sig
			}
			blocks = append(blocks, block)
			continue
		}

		// text part
		if text, ok := part["text"].(string); ok {
			blocks = append(blocks, map[string]interface{}{
				"type": "text",
				"text": text,
			})
			continue
		}

		// functionCall → tool_use（assistant 角色）
		if fc, ok := part["functionCall"].(map[string]interface{}); ok {
			name, _ := fc["name"].(string)
			callCounter[name]++
			id := fmt.Sprintf("call_%s_%d", name, callCounter[name])
			blocks = append(blocks, map[string]interface{}{
				"type":  "tool_use",
				"id":    id,
				"name":  name,
				"input": fc["args"],
			})
			continue
		}

		// functionResponse → tool_result（user 角色）
		if fr, ok := part["functionResponse"].(map[string]interface{}); ok {
			name, _ := fr["name"].(string)
			// 找到对应的 call ID（用相同计数）
			callID := fmt.Sprintf("call_%s_%d", name, callCounter[name])
			respContent := ""
			if resp, ok := fr["response"].(map[string]interface{}); ok {
				if ct, ok := resp["content"].(string); ok {
					respContent = ct
				} else {
					b, _ := json.Marshal(resp)
					respContent = string(b)
				}
			}
			blocks = append(blocks, map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": callID,
				"content":     respContent,
			})
			continue
		}

		// inlineData → image block
		if inlineData, ok := part["inlineData"].(map[string]interface{}); ok {
			mimeType, _ := inlineData["mimeType"].(string)
			data, _ := inlineData["data"].(string)
			if data != "" {
				blocks = append(blocks, map[string]interface{}{
					"type": "image",
					"source": map[string]interface{}{
						"type":       "base64",
						"media_type": mimeType,
						"data":       data,
					},
				})
			}
			continue
		}

		// fileData（GCS URI）→ image block（URL 源）
		if fileData, ok := part["fileData"].(map[string]interface{}); ok {
			uri, _ := fileData["fileUri"].(string)
			mimeType, _ := fileData["mimeType"].(string)
			if uri != "" {
				if strings.HasPrefix(uri, "gs://") {
					slog.Warn("[google→anthropic] GCS URI 不被 Anthropic 直接支持，跳过", "uri", uri)
					continue
				}
				blocks = append(blocks, map[string]interface{}{
					"type": "image",
					"source": map[string]interface{}{
						"type":       "url",
						"url":        uri,
						"media_type": mimeType,
					},
				})
			}
			continue
		}
	}
	return blocks
}

func mergeConsecutiveSameRole(msgs []map[string]interface{}) []map[string]interface{} {
	if len(msgs) <= 1 {
		return msgs
	}
	result := []map[string]interface{}{msgs[0]}
	for i := 1; i < len(msgs); i++ {
		last := result[len(result)-1]
		curr := msgs[i]
		if last["role"] == curr["role"] {
			lastBlocks, _ := last["content"].([]map[string]interface{})
			currBlocks, _ := curr["content"].([]map[string]interface{})
			last["content"] = append(lastBlocks, currBlocks...)
		} else {
			result = append(result, curr)
		}
	}
	return result
}
