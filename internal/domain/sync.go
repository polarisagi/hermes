package domain

import "time"

// ExternalProviderCache 来自外部数据源的厂商原始数据（暂存，未晋升到 sys_providers）
type ExternalProviderCache struct {
	ID           int    `json:"id"`
	ProviderID   string `json:"provider_id"`
	Source       string `json:"source"` // 'openrouter' | 'siliconflow' | 'manual'
	ProviderName string `json:"provider_name"`
	Description  string `json:"description"`
	WebsiteURL   string `json:"website_url"`
	APIBaseURL   string `json:"api_base_url"`
	APIProtocol  string `json:"api_protocol"` // 'openai' | 'anthropic' | 'google'
	// Status: 'pending' | 'promoted' | 'rejected'
	Status       string     `json:"status"`
	RejectReason string     `json:"reject_reason"`
	SyncedAt     time.Time  `json:"synced_at"`
	PromotedAt   *time.Time `json:"promoted_at,omitempty"`
}

// ExternalModelCache 来自外部数据源的模型原始数据（暂存，未晋升到 sys_models）
type ExternalModelCache struct {
	ID             int    `json:"id"`
	ModelID        string `json:"model_id"`
	Source         string `json:"source"` // 'openrouter' | 'litellm' | 'siliconflow'
	DisplayName    string `json:"display_name"`
	CapabilityTier string `json:"capability_tier"`
	// TierInferSrc 记录 tier 的判断来源：'source_tag' | 'auto_regex' | 'fallback_default'
	TierInferSrc         string  `json:"tier_infer_src"`
	ContextLength        int     `json:"context_length"`
	MaxOutputTokens      int     `json:"max_output_tokens"`
	SupportsVision       bool    `json:"supports_vision"`
	SupportsAudioInput   bool    `json:"supports_audio_input"`
	SupportsAudioOutput  bool    `json:"supports_audio_output"`
	SupportsTools        bool    `json:"supports_tools"`
	PromptPricePer1k     float64 `json:"prompt_price_per_1k"`
	CompletionPricePer1k float64 `json:"completion_price_per_1k"`
	ReleasedAt           int64   `json:"released_at"` // Unix 时间戳，0=来源未提供
	IsLegacy             bool    `json:"is_legacy"`
	// Status: 'pending' | 'promoted' | 'rejected'
	Status string `json:"status"`
	// RejectReason: 'pre_2025' | 'is_legacy' | 'no_tier'
	RejectReason string     `json:"reject_reason"`
	SyncedAt     time.Time  `json:"synced_at"`
	PromotedAt   *time.Time `json:"promoted_at,omitempty"`
}

// 暂存表状态常量
const (
	CacheStatusPending  = "pending"
	CacheStatusPromoted = "promoted"
	CacheStatusRejected = "rejected"
)

// 拒绝原因常量
const (
	RejectReasonPre2025         = "pre_2025"
	RejectReasonIsLegacy        = "is_legacy"
	RejectReasonNoTier          = "no_tier"
	RejectReasonOutdatedVersion = "outdated_version"
)
