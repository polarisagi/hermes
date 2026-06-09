package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/polarisagi/hermes/internal/domain"
)

// InjectAuth 根据端点的 AuthType 动态注入鉴权凭证
// 参数直接使用 domain 类型，不依赖 pool 包
func InjectAuth(_ context.Context, req *http.Request, provider *domain.UserProvider, ep *domain.SysAccessEndpoint) error {
	switch ep.AuthType {
	case domain.AuthTypeNone:
		return nil

	case domain.AuthTypeBearer:
		if key := extractToken(provider); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		return nil

	case domain.AuthTypeHeader:
		key := extractToken(provider)
		headerName := ep.AuthHeader
		if headerName == "" {
			headerName = "x-api-key"
		}
		if key != "" {
			req.Header.Set(headerName, key)
		}
		return nil

	case domain.AuthTypeQuery:
		key := extractToken(provider)
		if key != "" {
			queryKey := ep.AuthHeader
			if queryKey == "" {
				queryKey = "key"
			}
			q := req.URL.Query()
			q.Set(queryKey, key)
			req.URL.RawQuery = q.Encode()
		}
		return nil

	case domain.AuthTypeADC:
		// 区分纯文本 Token 和 ADC JSON
		// 如果用户在 adc_json 中填入了普通的 OAuth Token 或 API Key（非 JSON 结构）
		if token := extractToken(provider); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
			return nil
		}
		// TODO: 实现 Google ADC OAuth token 置换
		return errors.New("auth type adc not fully implemented yet")

	case domain.AuthTypeAWSSigV4:
		// TODO: 实现 AWS Signature V4
		return errors.New("auth type aws_sigv4 not fully implemented yet")

	default:
		// 退化为 Bearer
		if key := extractToken(provider); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		return nil
	}
}

// extractToken 提取纯文本凭证（API Key 或 OAuth Token）
// 优先从 api_key 字段提取，如果 adc_json 字段是不包含 '{' 的纯文本，也会将其作为 Token 提取
func extractToken(provider *domain.UserProvider) string {
	if provider == nil {
		return ""
	}
	var creds map[string]interface{}
	if err := json.Unmarshal(provider.AuthCredentials, &creds); err == nil {
		if key, ok := creds["api_key"].(string); ok && key != "" {
			return key
		}
		if adc, ok := creds["adc_json"].(string); ok && adc != "" {
			if !strings.HasPrefix(strings.TrimSpace(adc), "{") {
				return adc
			}
		}
	}
	key := strings.TrimSpace(string(provider.AuthCredentials))
	if key != "" && !strings.HasPrefix(key, "{") {
		return key
	}
	return ""
}
