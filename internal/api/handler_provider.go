package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/polarisagi/hermes/internal/domain"
)

func (h *AdminHandler) HandleAIAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		providers, err := h.providerRepo.GetUserProviders(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(providers)

	case http.MethodPost:
		var p domain.UserProvider
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		optimizeGoogleCredentials(&p)
		if err := h.providerRepo.CreateUserProvider(r.Context(), &p); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		go h.reloadAll(context.Background())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "id": p.ID})

	case http.MethodPut:
		var p domain.UserProvider
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		optimizeGoogleCredentials(&p)
		if err := h.providerRepo.UpdateUserProvider(r.Context(), &p); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		go h.reloadAll(context.Background())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success"})

	case http.MethodDelete:
		idStr := r.URL.Query().Get("id")
		id, err := strconv.Atoi(idStr)
		if err != nil || id <= 0 {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := h.providerRepo.DeleteUserProvider(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		go h.reloadAll(context.Background())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success"}`))

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *AdminHandler) HandleSysProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	providers, err := h.providerRepo.GetAllSysProviders(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	endpoints, err := h.providerRepo.GetAllSysAccessEndpoints(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"providers": providers,
		"endpoints": endpoints,
	})
}

// optimizeGoogleCredentials 自动判断用户的凭据是 API Key 还是 ADC JSON，并正确地放入对应字段
func optimizeGoogleCredentials(p *domain.UserProvider) {
	if p.ProviderID == "google" || p.ProviderID == "agent_platform" {
		var creds map[string]interface{}
		if err := json.Unmarshal(p.AuthCredentials, &creds); err == nil {
			changed := false

			// 如果有 adc_json，但它是一段非 JSON 的纯文本
			if adcVal, ok := creds["adc_json"].(string); ok && adcVal != "" {
				adcVal = strings.TrimSpace(adcVal)
				if !strings.HasPrefix(adcVal, "{") {
					// 纯文本，应作为 api_key
					creds["api_key"] = adcVal
					delete(creds, "adc_json")
					changed = true
				}
			}

			// 如果有 api_key，但它是一段 JSON
			if keyVal, ok := creds["api_key"].(string); ok && keyVal != "" {
				keyVal = strings.TrimSpace(keyVal)
				if strings.HasPrefix(keyVal, "{") {
					// JSON，应作为 adc_json
					creds["adc_json"] = keyVal
					delete(creds, "api_key")
					changed = true
				}
			}

			if changed {
				if b, err := json.Marshal(creds); err == nil {
					p.AuthCredentials = b
				}
			}
		} else {
			// 如果 auth_credentials 整个是一段普通文本（如直接传了 key 的字符串）
			raw := strings.TrimSpace(string(p.AuthCredentials))
			if raw != "" && raw != `"{}"` && raw != `null` {
				newCreds := make(map[string]interface{})
				raw = strings.TrimPrefix(raw, "\"")
				raw = strings.TrimSuffix(raw, "\"")
				if strings.HasPrefix(raw, "{") {
					newCreds["adc_json"] = raw
				} else {
					newCreds["api_key"] = raw
				}
				if b, err := json.Marshal(newCreds); err == nil {
					p.AuthCredentials = b
				}
			}
		}
	}
}
