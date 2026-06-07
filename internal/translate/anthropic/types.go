// Package anthropic 定义 Anthropic Messages API 的请求/响应数据模型。
// 该包作为共享协议层，被 togoogle、toopenai 等子翻译器共同引用。
package anthropic

// MessageRequest 对应 Anthropic Messages API 请求体
type MessageRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`

	System interface{} `json:"system,omitempty"`

	MaxTokens     int      `json:"max_tokens"`
	Temperature   *float64 `json:"temperature,omitempty"`
	TopP          *float64 `json:"top_p,omitempty"`
	TopK          *int     `json:"top_k,omitempty"`
	StopSequences []string `json:"stop_sequences,omitempty"`

	Stream bool `json:"stream,omitempty"`

	Tools      []Tool      `json:"tools,omitempty"`
	ToolChoice *ToolChoice `json:"tool_choice,omitempty"`

	Thinking     *ThinkingConfig `json:"thinking,omitempty"`
	Effort       string          `json:"effort,omitempty"`
	OutputConfig *OutputConfig   `json:"output_config,omitempty"`

	Metadata          *RequestMetadata   `json:"metadata,omitempty"`
	ContextManagement *ContextManagement `json:"context_management,omitempty"`
}

// ThinkingConfig Anthropic 扩展思考配置
//
// 2026 当前标准（Opus 4.7/4.8）：Type = "adaptive"，配合顶层 Effort 字段控制强度
// 旧格式（已废弃，Opus 4.6 及以下）：Type = "enabled"，BudgetTokens 控制思考 token 预算
type ThinkingConfig struct {
	Type         string `json:"type,omitempty"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
	Display      string `json:"display,omitempty"`
}

// Message 单条对话消息
type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// Tool Claude 可调用的工具定义
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema,omitempty"`
	Type        string                 `json:"type,omitempty"`
}

// ToolChoice 工具选择策略
type ToolChoice struct {
	Type                   string `json:"type"`
	Name                   string `json:"name,omitempty"`
	DisableParallelToolUse bool   `json:"disable_parallel_tool_use,omitempty"`
}

// OutputConfig 输出行为控制（DeepSeek Anthropic 接口专用）
type OutputConfig struct {
	Effort string `json:"effort,omitempty"`
}

type RequestMetadata struct {
	UserID string `json:"user_id,omitempty"`
}

// ContextManagement Anthropic 上下文管理配置（beta 特性）
type ContextManagement struct {
	Edits []ContextEdit `json:"edits,omitempty"`
}

type ContextEdit struct {
	Type     string `json:"type,omitempty"`
	Strategy string `json:"strategy,omitempty"`
	Keep     string `json:"keep,omitempty"`
}

// MessageResponse Anthropic 非流式响应结构
type MessageResponse struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	Role         string    `json:"role"`
	Content      []Content `json:"content"`
	Model        string    `json:"model"`
	StopReason   string    `json:"stop_reason"`
	StopSequence string    `json:"stop_sequence"`
	Usage        Usage     `json:"usage"`
}

// Content 内容块（支持 text、thinking、tool_use、tool_result、image 等类型）
type Content struct {
	Type string `json:"type"`

	Text string `json:"text,omitempty"`

	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`

	ID    string      `json:"id,omitempty"`
	Name  string      `json:"name,omitempty"`
	Input interface{} `json:"input,omitempty"`

	ToolUseID string      `json:"tool_use_id,omitempty"`
	Content   interface{} `json:"content,omitempty"`

	Source map[string]interface{} `json:"source,omitempty"`
}

// Usage Anthropic 用量统计（2025-2026 标准）
type Usage struct {
	InputTokens              int                  `json:"input_tokens"`
	OutputTokens             int                  `json:"output_tokens"`
	CacheCreationInputTokens int                  `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int                  `json:"cache_read_input_tokens,omitempty"`
	OutputTokensDetails      *OutputTokensDetails `json:"output_tokens_details,omitempty"`
}

// OutputTokensDetails 输出 token 详情（Anthropic 2025-2026 新增）
type OutputTokensDetails struct {
	ThinkingTokens int `json:"thinking_tokens,omitempty"`
}

// StreamEvent Anthropic SSE 事件结构
type StreamEvent struct {
	Type         string           `json:"type"`
	Message      *MessageResponse `json:"message,omitempty"`
	Index        *int             `json:"index,omitempty"`
	ContentBlock *Content         `json:"content_block,omitempty"`
	Delta        *Delta           `json:"delta,omitempty"`
	Usage        *Usage           `json:"usage,omitempty"`
}

// Delta SSE 增量更新
type Delta struct {
	Type         string `json:"type,omitempty"`
	Text         string `json:"text,omitempty"`
	Thinking     string `json:"thinking,omitempty"`
	Signature    string `json:"signature,omitempty"`
	StopReason   string `json:"stop_reason,omitempty"`
	StopSequence string `json:"stop_sequence,omitempty"`
	PartialJson  string `json:"partial_json,omitempty"`
	Content      string `json:"content,omitempty"`
}
