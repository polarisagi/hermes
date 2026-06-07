package router

import (
	"reflect"
	"testing"
)

func TestIntentInferer_InferByKeywords(t *testing.T) {
	inferer := &IntentInferer{}

	tests := []struct {
		modelID  string
		expected string
	}{
		// fast — 含轻量化关键词
		{"gpt-4o-mini-2024", "fast"},
		{"gpt-5.4-mini", "fast"},
		{"claude-haiku-4-5", "fast"},
		{"gemini-3.1-flash", "fast"},
		{"text-embedding-3-small", "fast"},
		{"deepseek-v4-flash", "fast"},

		// fast — 含轻量化/蒸馏后缀，即使带 r1/thinking 等关键词也优先归 fast
		{"o1-mini", "fast"},
		{"o3-mini", "fast"},
		{"deepseek-r1-distill-llama-8b", "fast"},
		{"deepseek-r1-distill-qwen-14b", "fast"},

		// smart — 原"推理型"模型统一归 smart（thinking 能力已是参数，不再是独立 tier）
		{"o1", "smart"},
		{"o3", "smart"},
		{"o4-preview", "smart"},
		{"deepseek-r1", "smart"},
		{"kimi-k2-thinking", "smart"},
		{"qwen3-max-thinking", "smart"},
		{"sonar-reasoning-pro", "smart"},
		{"gemini-3.1-flash-thinking", "fast"}, // flash 是轻量模型，thinking 已是参数，不影响 tier

		// smart — 旗舰模型
		{"claude-sonnet-4-6", "smart"},
		{"claude-opus-4-8", "smart"},
		{"claude-3-5-sonnet-20241022", "smart"},
		{"claude-3-opus-20240229", "smart"},
		{"gpt-4-0613", "smart"},
		{"deepseek-v4-pro", "smart"},
		{"gemini-3.1-pro-preview-customtool", "smart"},
		{"qwen-3-max", "smart"},
		// gpt-4-turbo 系列：turbo 已从 fast 关键词移除，由 gpt-4 关键词正确归入 smart
		{"gpt-4-turbo", "smart"},
		{"gpt-4-turbo-preview", "smart"},

		// gpt-4o 系列（Codex 常用）
		{"gpt-4o", "smart"},     // 匹配 gpt-4
		{"gpt-4o-mini", "fast"}, // 匹配 mini

		// OpenAI o 系列
		{"o1", "smart"},     // 匹配 \bo1\b
		{"o3", "smart"},     // 匹配 \bo3\b
		{"o4-mini", "fast"}, // 匹配 mini

		// 无法推断（但这类模型在 sys_model_intent_dict 中有记录，优先级 3 直接命中，不会走到正则层）
		{"kimi-k2.6", ""},
		{"unknown-model-xyz", ""},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			actual := inferer.inferByKeywords(tt.modelID)
			if actual != tt.expected {
				t.Errorf("inferByKeywords(%q) = %q, expected %q", tt.modelID, actual, tt.expected)
			}
		})
	}
}

func TestPipeline_checkCustomRoute(t *testing.T) {
	p := &Pipeline{
		// 精确匹配：claude-opus-4-8 → [1, 2]
		customRouteMap: map[string][]int{
			"claude-opus-4-8": {1, 2},
		},
		// 通配符模式：claude-* (specificity=7) > *-mini (specificity=5)
		patternRoutes: []routePattern{
			{prefix: "claude-", suffix: "", ids: []int{3, 4}, specificity: 7},
			{prefix: "", suffix: "-mini", ids: []int{5}, specificity: 5},
		},
		// 全局兜底
		wildcardRouteIDs: []int{99},
	}

	tests := []struct {
		modelID  string
		expected []int
	}{
		// 精确匹配优先
		{"claude-opus-4-8", []int{1, 2}},
		// 命中前缀模式 claude-*
		{"claude-sonnet-4-6", []int{3, 4}},
		// 命中前缀模式 claude-* 而非后缀 *-mini（更具体的模式优先）
		{"claude-haiku-mini", []int{3, 4}},
		// 命中后缀模式 *-mini（不是 claude- 开头）
		{"gpt-4o-mini", []int{5}},
		// 命中全局兜底
		{"unknown-model", []int{99}},
		// 全局兜底（无任何匹配）
		{"deepseek-v4", []int{99}},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := p.checkCustomRoute(tt.modelID)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("checkCustomRoute(%q) = %v, expected %v", tt.modelID, got, tt.expected)
			}
		})
	}
}
