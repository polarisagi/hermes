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
		if key := extractAPIKey(provider); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		return nil

	case domain.AuthTypeHeader:
		key := extractAPIKey(provider)
		headerName := ep.AuthHeader
		if headerName == "" {
			headerName = "x-api-key"
		}
		if key != "" {
			req.Header.Set(headerName, key)
		}
		return nil

	case domain.AuthTypeQuery:
		key := extractAPIKey(provider)
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
		// TODO: 实现 Google ADC OAuth token 置换
		return errors.New("auth type adc not fully implemented yet")

	case domain.AuthTypeAWSSigV4:
		// TODO: 实现 AWS Signature V4
		return errors.New("auth type aws_sigv4 not fully implemented yet")

	default:
		// 退化为 Bearer
		if key := extractAPIKey(provider); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		return nil
	}
}

func extractAPIKey(provider *domain.UserProvider) string {
	if provider == nil {
		return ""
	}
	var creds map[string]interface{}
	if err := json.Unmarshal(provider.AuthCredentials, &creds); err == nil {
		if key, ok := creds["api_key"].(string); ok && key != "" {
			return key
		}
	}
	key := strings.TrimSpace(string(provider.AuthCredentials))
	if key != "" && !strings.Contains(key, "{") {
		return key
	}
	return ""
}
