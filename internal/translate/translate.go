package translate

import (
	"net/http"

	"github.com/polarisagi/hermes/internal/domain"
)

// Translator 协议转换器接口，纯粹的 JSON 报文映射器。
// 不负责网络请求、鉴权和重试，这些由 proxy 层完成。
type Translator interface {
	// TranslateRequest 将原始请求转换为目标后端的 payload，并返回目标子路径
	TranslateRequest(
		r *http.Request,
		originalBody []byte,
		provider *domain.UserProvider,
		targetEndpoint *domain.SysAccessEndpoint,
		targetModel string,
	) (payload []byte, targetPath string, err error)

	// TranslateResponse 将目标后端的响应转换为客户端格式（含流式和非流式）
	TranslateResponse(w http.ResponseWriter, r *http.Request, upstreamResp *http.Response) error
}

// Factory 根据协议转换键名（如 "anthropic_google"）分配对应的 Translator
type Factory struct {
	translators map[string]Translator
}

func NewFactory() *Factory {
	return &Factory{translators: make(map[string]Translator)}
}

// Register 注册翻译器，key 格式为 "{src}_{dst}"，如 "anthropic_google"
func (f *Factory) Register(key string, t Translator) {
	f.translators[key] = t
}

// Get 获取指定协议对的翻译器
func (f *Factory) Get(key string) Translator {
	return f.translators[key]
}

// OriginalReqBodyKey 用于在 context 中传递原始请求体的 key
type OriginalReqBodyKey struct{}
