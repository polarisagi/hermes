// Anthropic 协议类型别名
// 所有 Anthropic 数据模型定义已迁移至父包 internal/translator/anthropic/models.go，
// 此处通过 Go 类型别名（type alias）无缝引用，togoogle 包内的所有现有代码无需修改。
package togoogle

import anthr "github.com/polarisagi/hermes/internal/translate/anthropic"

// ── Anthropic 协议类型别名（来自父包，勿在此处添加新类型定义）──────────────────
type (
	MessageRequest      = anthr.MessageRequest
	ThinkingConfig      = anthr.ThinkingConfig
	OutputConfig        = anthr.OutputConfig
	ContextManagement   = anthr.ContextManagement
	ContextEdit         = anthr.ContextEdit
	RequestMetadata     = anthr.RequestMetadata
	Tool                = anthr.Tool
	ToolChoice          = anthr.ToolChoice
	Message             = anthr.Message
	MessageResponse     = anthr.MessageResponse
	Content             = anthr.Content
	Usage               = anthr.Usage
	OutputTokensDetails = anthr.OutputTokensDetails
	StreamEvent         = anthr.StreamEvent
	Delta               = anthr.Delta
)
