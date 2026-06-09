package domain

import "time"

// SysModel 代表系统内置支持的各厂商的大模型（全球唯一的模型实体字典）
type SysModel struct {
	ModelID              string  `json:"model_id"`
	DisplayName          string  `json:"display_name"`
	CapabilityTier       string  `json:"capability_tier"`
	ContextLength        int     `json:"context_length"`
	MaxOutputTokens      int     `json:"max_output_tokens"`
	SupportsVision       bool    `json:"supports_vision"`
	SupportsAudioInput   bool    `json:"supports_audio_input"`
	SupportsAudioOutput  bool    `json:"supports_audio_output"`
	SupportsTools        bool    `json:"supports_tools"`
	PromptPricePer1k     float64 `json:"prompt_price_per_1k"`
	CompletionPricePer1k float64 `json:"completion_price_per_1k"`
	ReleasedAt           int64   `json:"released_at"` // Unix timestamp (same as OpenRouter's 'created')
	IsActive             bool    `json:"is_active"`
	VersionWeight        int     `json:"version_weight"` // 版本权重（用于排序，越大越新）
	IsLegacy             bool    `json:"is_legacy"`      // 是否为过时旧模型
}

// SysProviderModel 代表某个厂商提供的模型映射
type SysProviderModel struct {
	ProviderID    string `json:"provider_id"`
	ModelID       string `json:"model_id"`
	ActualModelID string `json:"actual_model_id"`
}

// UserModel 用户为自己的某个渠道配置的模型实体（包含了主观的意图分级）
type UserModel struct {
	ID             int    `json:"id"`
	UserProviderID int    `json:"user_provider_id"`
	DisplayName    string `json:"display_name"`
	ModelID        string `json:"model_id"`
	CapabilityTier string `json:"capability_tier"` // 主观标记该模型的梯队
	IsActive       bool   `json:"is_active"`
	VersionWeight  int    `json:"version_weight"`  // 继承自 sys_models，若为0则降级为 released_at
	IsLegacy       bool   `json:"is_legacy"`       // 继承自 sys_models
}

// SysModelIntentDict 系统级路由意图字典（model_id → capability_tier，轻量路由专用）
type SysModelIntentDict struct {
	ModelID        string `json:"model_id"`
	CapabilityTier string `json:"capability_tier"`
	Source         string `json:"source"` // "seed", "openrouter_sync", "litellm_sync", "auto_inferred"
}

// UserModelIntentDict 用户级别的手动覆盖字典（优先级高于系统字典）
type UserModelIntentDict struct {
	ID             int       `json:"id"`
	ModelID        string    `json:"model_id"` // 任意模型标识符（客户端或服务端均可）
	CapabilityTier string    `json:"capability_tier"`
	Source         string    `json:"source"` // "manual", "auto_regex", "auto_llm"
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
