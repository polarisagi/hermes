package router

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/polarisagi/hermes/internal/domain"
	"github.com/polarisagi/hermes/internal/store"
)

// IntentInferer 负责推断未知模型的意图标签
type IntentInferer struct {
	intentRepo *store.IntentRepo
}

func NewIntentInferer(intentRepo *store.IntentRepo) *IntentInferer {
	return &IntentInferer{intentRepo: intentRepo}
}

// InferTierOnly 纯推断，不写数据库，返回 (tier, source)
// source: "auto_regex" | "auto_llm" | "fallback_default"
func (i *IntentInferer) InferTierOnly(ctx context.Context, modelID string) (string, string) {
	if tier := i.inferByKeywords(modelID); tier != "" {
		return tier, "auto_regex"
	}
	if tier := i.inferByLLM(ctx, modelID); tier != "" {
		return tier, "auto_llm"
	}
	return "smart", "fallback_default"
}

// InferUnknownModel 自动分类未知模型并持久化到用户意图字典，返回 (tier, source)
func (i *IntentInferer) InferUnknownModel(ctx context.Context, modelID string) (string, string) {
	tier, source := i.InferTierOnly(ctx, modelID)
	_ = i.intentRepo.SaveUserIntent(ctx, &domain.UserModelIntentDict{
		ModelID:        modelID,
		CapabilityTier: tier,
		Source:         source,
	})
	return tier, source
}

// inferByKeywords 通过内置特征词推断（仅 fast / smart 两级）
// 思考/推理能力已统一为请求参数，不再通过模型名区分 reasoning tier
func (i *IntentInferer) inferByKeywords(modelID string) string {
	lower := strings.ToLower(modelID)

	// turbo 已从 fast 关键词中移除：gpt-4-turbo 是旗舰，已被 smart 的 gpt-4 关键词捕获；
	// 在 2026 年，gpt-3.5-turbo 早已过时，不在实际使用场景中出现。
	if regexp.MustCompile(`(?i)(\bmini\b|haiku|flash|lite|nano|fast|small|distill|\b8b\b|\b9b\b|\b12b\b|\b14b\b)`).MatchString(lower) {
		return "fast"
	}

	if regexp.MustCompile(`(?i)(sonnet|opus|pro|max|large|gpt-4|gpt-5|v3|v4|ultra|grok|nemotron|sonar|claude-3|claude-4|qwen|ernie|thinking|reason(?:ing)?|deep-research|\bo1\b|\bo3\b|\bo4\b|\br1\b|\br2\b)`).MatchString(lower) {
		return "smart"
	}

	return ""
}

func (i *IntentInferer) inferByLLM(_ context.Context, _ string) string {
	// TODO: 等待 Proxy 层内部调用接口就绪
	return ""
}

// ParseVersionWeight 解析模型名称中的版本号，用于新旧排序
func (i *IntentInferer) ParseVersionWeight(modelID string) int {
	weight := 0

	if strings.Contains(modelID, "latest") {
		weight += 9999999
	}

	verRe := regexp.MustCompile(`(?:gpt-|gemini-|claude-|v|o)(\d+)(?:[-.](\d+))?(o)?`)
	if matches := verRe.FindStringSubmatch(modelID); len(matches) > 1 {
		major, _ := strconv.Atoi(matches[1])
		minor := 0
		if len(matches) > 2 && matches[2] != "" {
			minor, _ = strconv.Atoi(matches[2])
		}
		baseWeight := major*10000 + minor*100
		if len(matches) > 3 && matches[3] == "o" {
			baseWeight += 500
		}
		weight += baseWeight * 10000
	}

	return weight
}

// IsLegacyModel 判断是否为带日期后缀的历史快照版本
func (i *IntentInferer) IsLegacyModel(modelID string) bool {
	if regexp.MustCompile(`(202\d)[-]?(\d{2})[-]?(\d{2})`).MatchString(modelID) {
		return true
	}
	if regexp.MustCompile(`-(0[1-9]|1[0-2])([0-2][0-9]|3[01])$|-\d{6}$`).MatchString(modelID) {
		return true
	}
	lower := strings.ToLower(modelID)
	return strings.Contains(lower, "legacy") ||
		strings.Contains(lower, "deprecated") ||
		strings.Contains(lower, "gpt-3.5")
}
