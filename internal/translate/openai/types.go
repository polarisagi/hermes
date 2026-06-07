package openai

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/polarisagi/hermes/internal/domain"
)

// ExtractAPIKey 从渠道凭证提取 API key
func ExtractAPIKey(provider *domain.UserProvider) string {
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

func IsResponsesAPIRequest(r *http.Request) bool {
	return strings.HasSuffix(r.URL.Path, "/responses") || strings.Contains(r.URL.Path, "/v1/responses")
}

// IsOpenAINativeBackend 判断目标后端是否为 OpenAI 官方（api.openai.com），用于决定是否做 Responses API 透传
func IsOpenAINativeBackend(provider *domain.UserProvider, ep *domain.SysAccessEndpoint) bool {
	urls := []string{strings.ToLower(provider.ProviderID)}
	if ep != nil {
		urls = append(urls, strings.ToLower(ep.DefaultBaseURL))
	}
	for _, u := range urls {
		if strings.Contains(u, "api.openai.com") || u == "openai" {
			return true
		}
	}
	return false
}

func ExtractTextFromContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok && t != "" {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}
