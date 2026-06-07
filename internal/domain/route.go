package domain

import "time"

// UserCustomRoute 专业模式：强制 1对1 路由
type UserCustomRoute struct {
	ID                int    `json:"id"`
	RequestedModelID  string `json:"requested_model_id"`   // 客户端请求的模型，如 "gpt-4o"
	TargetUserModelID int    `json:"target_user_model_id"` // 强制绑定的后台 UserModels 的 ID
	IsActive          bool   `json:"is_active"`
}

// RoutingLog 路由决策日志（独立于计费，供透明层 UI 展示和用户调整）
type RoutingLog struct {
	ID             int       `json:"id"`
	CreatedAt      time.Time `json:"created_at"`
	RequestedModel string    `json:"requested_model"`
	ResolvedTier   string    `json:"resolved_tier"`
	// ResolutionSrc: custom_route | user_dict | sys_dict | auto_regex | fallback_default
	ResolutionSrc  string `json:"resolution_src"`
	TierDegraded   bool   `json:"tier_degraded"`
	OriginalTier   string `json:"original_tier,omitempty"`
	ProviderName   string `json:"provider_name,omitempty"`
	ActualModel    string `json:"actual_model,omitempty"`
	UserProviderID int    `json:"user_provider_id,omitempty"`
	ClientName     string `json:"client_name,omitempty"`
}

// AccountLog 请求流水账单
type AccountLog struct {
	ID               int       `json:"id"`
	AccountName      string    `json:"account_name"`
	APIProtocol      string    `json:"api_protocol"`
	RequestedModelID string    `json:"requested_model_id"`
	ActualModelID    string    `json:"actual_model_id"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	LatencyMs        int       `json:"latency_ms"`
	StatusCode       int       `json:"status_code"`
	ErrorMsg         string    `json:"error_msg"`
	Cost             float64   `json:"cost"`
	ClientName       string    `json:"client_name"`
	CreatedAt        time.Time `json:"created_at"`
}
