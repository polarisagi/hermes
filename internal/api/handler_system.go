package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/polarisagi/hermes/internal/config"
	"github.com/polarisagi/hermes/pkg/logger"
)

func (h *AdminHandler) HandleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")

		getIntSetting := func(key string, def int) int {
			valStr, _ := h.settingsRepo.GetSetting(r.Context(), key)
			if valStr == "" {
				return def
			}
			val, err := strconv.Atoi(valStr)
			if err != nil {
				return def
			}
			return val
		}

		settings := map[string]interface{}{
			"listen_addr": config.GlobalConfig.Server.ListenAddr,
			"breaker": map[string]int{
				"initial_cooldown_seconds": getIntSetting("initial_cooldown_seconds", 60),
				"max_cooldown_seconds":     getIntSetting("max_cooldown_seconds", 3600),
				"failure_threshold":        getIntSetting("failure_threshold", 3),
				"failure_window_seconds":   getIntSetting("failure_window_seconds", 120),
			},
			"google_oauth_client_id":     "",
			"google_oauth_client_secret": "",
			"update_proxy_channel":       "",
		}

		if val, _ := h.settingsRepo.GetSetting(r.Context(), "listen_addr"); val != "" {
			settings["listen_addr"] = val
		}
		if val, _ := h.settingsRepo.GetSetting(r.Context(), "google_oauth_client_id"); val != "" {
			settings["google_oauth_client_id"] = val
		}
		if val, _ := h.settingsRepo.GetSetting(r.Context(), "google_oauth_client_secret"); val != "" {
			settings["google_oauth_client_secret"] = val
		}
		if val, _ := h.settingsRepo.GetSetting(r.Context(), "update_proxy_channel"); val != "" {
			settings["update_proxy_channel"] = val
		}

		_ = json.NewEncoder(w).Encode(settings)
		return
	}

	if r.Method == http.MethodPost {
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			if v, ok := payload["listen_addr"].(string); ok {
				_ = h.settingsRepo.SetSetting(r.Context(), "listen_addr", v)
			}
			if v, ok := payload["google_oauth_client_id"].(string); ok {
				_ = h.settingsRepo.SetSetting(r.Context(), "google_oauth_client_id", v)
			}
			if v, ok := payload["google_oauth_client_secret"].(string); ok {
				_ = h.settingsRepo.SetSetting(r.Context(), "google_oauth_client_secret", v)
			}
			if v, ok := payload["update_proxy_channel"].(string); ok {
				_ = h.settingsRepo.SetSetting(r.Context(), "update_proxy_channel", v)
			}
			for _, key := range []string{"initial_cooldown_seconds", "max_cooldown_seconds", "failure_threshold", "failure_window_seconds"} {
				if v, ok := payload[key].(float64); ok {
					_ = h.settingsRepo.SetSetting(r.Context(), key, strconv.Itoa(int(v)))
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success"}`))
		return
	}
}

func (h *AdminHandler) GetLogs(w http.ResponseWriter, r *http.Request) {
	logPath := logger.GetLogPath()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	data, err := os.ReadFile(logPath)
	if err != nil {
		_, _ = w.Write([]byte("No logs found or unable to read log file."))
		return
	}
	_, _ = w.Write(data)
}

func (h *AdminHandler) SetDebug(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"debug": logger.IsDebugEnabled()})
		return
	}

	if r.Method == http.MethodPost {
		var payload struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			logger.SetDebug(payload.Enabled)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "debug": logger.IsDebugEnabled()})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (h *AdminHandler) StartGoogleOAuth(w http.ResponseWriter, r *http.Request) {
	clientID, _ := h.settingsRepo.GetSetting(r.Context(), "google_oauth_client_id")
	clientSecret, _ := h.settingsRepo.GetSetting(r.Context(), "google_oauth_client_secret")

	if clientID == "" || clientSecret == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<script>alert("请先在系统设置中配置 Google OAuth Client ID 和 Secret。"); window.close();</script>`))
		return
	}

	// 用 CSPRNG 生成不可预测的 state，防止 CSRF 攻击
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		http.Error(w, "Failed to generate state", http.StatusInternalServerError)
		return
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	oauthStateStore.Store(state, map[string]string{
		"client_id":     clientID,
		"client_secret": clientSecret,
	})

	redirectURI := "http://127.0.0.1:27777/api/admin/oauth/google/callback"
	authURL := fmt.Sprintf("https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&access_type=offline&prompt=consent&state=%s",
		url.QueryEscape(clientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape("https://www.googleapis.com/auth/cloud-platform"),
		url.QueryEscape(state),
	)

	http.Redirect(w, r, authURL, http.StatusFound)
}

func (h *AdminHandler) CallbackGoogleOAuth(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	v, ok := oauthStateStore.LoadAndDelete(state)
	if !ok {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}
	creds := v.(map[string]string)

	resp, err := http.PostForm("https://oauth2.googleapis.com/token", url.Values{
		"code":          {code},
		"client_id":     {creds["client_id"]},
		"client_secret": {creds["client_secret"]},
		"redirect_uri":  {"http://127.0.0.1:27777/api/admin/oauth/google/callback"},
		"grant_type":    {"authorization_code"},
	})
	if err != nil || resp.StatusCode != 200 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<script>alert("获取 Token 失败。"); window.close();</script>`))
		return
	}
	defer resp.Body.Close()

	var tokenRes map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tokenRes); err != nil {
		http.Error(w, "Failed to parse token response", http.StatusInternalServerError)
		return
	}

	refreshToken, ok := tokenRes["refresh_token"].(string)
	if !ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<script>alert("未获取到 refresh_token，请尝试重新登录。"); window.close();</script>`))
		return
	}

	adcJson := map[string]string{
		"client_id":     creds["client_id"],
		"client_secret": creds["client_secret"],
		"refresh_token": refreshToken,
		"type":          "authorized_user",
	}
	adcBytes, _ := json.MarshalIndent(adcJson, "", "  ")

	// 将 targetOrigin 固定为 opener 的同源地址，防止 refresh_token 被恶意 opener 窃取
	html := fmt.Sprintf(`<html><body><script>
		var target = window.opener ? window.opener.location.origin : window.location.origin;
		window.opener.postMessage({ type: 'google_adc_auth', data: %s }, target);
		window.close();
	</script></body></html>`, strconv.Quote(string(adcBytes)))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}
