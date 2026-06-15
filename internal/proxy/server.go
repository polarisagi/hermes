package proxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/klauspost/compress/zstd"

	"time"

	"github.com/polarisagi/hermes/internal/auth"
	"github.com/polarisagi/hermes/internal/billing"
	"github.com/polarisagi/hermes/internal/domain"
	"github.com/polarisagi/hermes/internal/pool"
	"github.com/polarisagi/hermes/internal/router"
	"github.com/polarisagi/hermes/internal/translate"
	"github.com/polarisagi/hermes/internal/translate/anthropic"
	"github.com/polarisagi/hermes/internal/translate/openai"
	"github.com/polarisagi/hermes/pkg/httpclient"
	"github.com/polarisagi/hermes/pkg/logger"
)

// Server 网关数据面，处理所有 LLM 代理请求
type Server struct {
	pipeline     *router.Pipeline
	chanManager  *pool.Manager
	transFactory *translate.Factory
}

func NewServer(
	pipeline *router.Pipeline,
	chanManager *pool.Manager,
	transFactory *translate.Factory,
) *Server {
	return &Server{
		pipeline:     pipeline,
		chanManager:  chanManager,
		transFactory: transFactory,
	}
}

func detectPayloadProtocol(reqMap map[string]interface{}) string {
	if _, ok := reqMap["system"]; ok {
		return "anthropic"
	}
	_, hasMaxTokens := reqMap["max_tokens"]
	hasOpenAISystem := false
	if msgs, ok := reqMap["messages"].([]interface{}); ok {
		for _, m := range msgs {
			if msgMap, ok := m.(map[string]interface{}); ok {
				if role, _ := msgMap["role"].(string); role == "system" {
					hasOpenAISystem = true
				}
			}
		}
	}
	if _, ok := reqMap["contents"]; ok {
		return "google"
	}
	if hasOpenAISystem {
		return "openai"
	}
	if hasMaxTokens {
		return "anthropic"
	}
	return ""
}

func extractModelName(r *http.Request, reqMap map[string]interface{}) string {
	// 1. Check JSON body
	if m, ok := reqMap["model"].(string); ok && m != "" {
		return m
	}

	// 2. Check URL path (Google protocol usually has it in the URL)
	path := r.URL.Path
	if idx := strings.Index(path, "/models/"); idx != -1 {
		modelPart := path[idx+len("/models/"):]
		if colonIdx := strings.Index(modelPart, ":"); colonIdx != -1 {
			return modelPart[:colonIdx]
		}
		if slashIdx := strings.Index(modelPart, "/"); slashIdx != -1 {
			return modelPart[:slashIdx]
		}
		return modelPart
	}

	// 3. Check HTTP Headers (Fallback for some proxies/clients)
	if m := r.Header.Get("X-Model"); m != "" {
		return m
	}
	if m := r.Header.Get("X-Requested-Model"); m != "" {
		return m
	}

	return ""
}

// targetPriority 协议优先级矩阵：给定客户端协议，优先选择哪种后端协议
var targetPriority = map[string][]string{
	"anthropic": {"anthropic", "google", "openai"},
	"openai":    {"openai", "anthropic", "google"},
	"google":    {"google", "openai", "anthropic"},
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	slog.Info("📥 [Incoming Request]", "method", r.Method, "path", r.URL.Path, "remote_addr", r.RemoteAddr)
	startTime := time.Now()
	iw := billing.NewInterceptResponseWriter(w)
	w = iw

	clientName := r.Header.Get("X-Client-Name")
	if clientName == "" {
		ua := r.Header.Get("User-Agent")
		uaLower := strings.ToLower(ua)
		if strings.Contains(ua, "Claude") || strings.Contains(uaLower, "claude-code") {
			clientName = "Claude Code"
		} else if strings.Contains(ua, "Cursor") || strings.Contains(uaLower, "cursor") {
			clientName = "Cursor"
		} else if strings.Contains(ua, "Windsurf") || strings.Contains(uaLower, "windsurf") {
			clientName = "Windsurf"
		} else if strings.Contains(ua, "Copilot") || strings.Contains(uaLower, "copilot") {
			clientName = "Copilot"
		} else if strings.Contains(uaLower, "opencode") {
			clientName = "OpenCode"
		} else if strings.Contains(uaLower, "codex") {
			clientName = "Codex"
		} else if strings.Contains(uaLower, "gemini") {
			clientName = "Gemini CLI"
		} else if strings.Contains(uaLower, "curl") {
			clientName = "curl"
		} else {
			// fallback to just the raw user agent or a portion of it
			clientName = strings.Split(ua, "/")[0]
			if clientName == "" {
				clientName = "Unknown"
			}
		}
	}

	var finalActualModel string
	var finalProviderName string
	var finalAccountName string
	var finalAPIProtocol string

	var finalReqBody []byte
	var finalReqModel string
	var skipBilling bool

	defer func() {
		if skipBilling {
			return
		}
		latencyMs := int(time.Since(startTime).Milliseconds())
		p, c, cache, content := iw.GetTokens()

		billing.ProcessBilling(
			finalProviderName,
			finalAccountName,
			finalAPIProtocol,
			clientName,
			finalReqModel,
			finalActualModel,
			finalReqBody,
			content,
			int(p),
			int(c),
			int(cache),
			latencyMs,
			iw.StatusCode,
			"",
			r.Method,
			r.URL.Path,
		)
	}()
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")

	path := r.URL.Path
	var clientProtocol string
	if strings.HasPrefix(path, "/v1/openai/") {
		clientProtocol = "openai"
	} else if strings.HasPrefix(path, "/v1/anthropic/") {
		clientProtocol = "anthropic"
	} else if strings.HasPrefix(path, "/v1/google/") {
		clientProtocol = "google"
	} else {
		http.Error(w, "Invalid gateway endpoint. Please use /v1/openai/, /v1/anthropic/, or /v1/google/", http.StatusNotFound)
		return
	}

	bodyReader := r.Body
	contentEncoding := r.Header.Get("Content-Encoding")
	if contentEncoding == "gzip" {
		gr, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, "Failed to decompress gzip body", http.StatusBadRequest)
			return
		}
		defer gr.Close()
		bodyReader = gr
	} else if contentEncoding == "zstd" {
		zr, err := zstd.NewReader(r.Body)
		if err != nil {
			http.Error(w, "Failed to decompress zstd body", http.StatusBadRequest)
			return
		}
		defer zr.Close()
		bodyReader = io.NopCloser(zr)
	}

	bodyBytes, err := io.ReadAll(bodyReader)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if logger.IsDebugEnabled() {
		slog.Debug(">>> 收到客户端原始请求",
			"url", r.URL.String(),
			"method", r.Method,
			"headers", fmt.Sprintf("%v", r.Header),
			"body", string(bodyBytes),
		)
	}

	// 拦截处理客户端特有的探测、WebSocket 等预检请求
	if openai.HandleClientProbes(w, r) {
		return
	}

	// 本地拦截 count_tokens：不转发到任何后端，用程序计算直接返回
	// 支持中文（CJK），格式符合 Anthropic count_tokens API 规范（Claude Code /context 所需）
	if clientProtocol == "anthropic" && strings.HasSuffix(path, "/count_tokens") {
		skipBilling = true
		anthropic.HandleCountTokens(w, bodyBytes)
		return
	}

	var reqMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &reqMap); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	detected := detectPayloadProtocol(reqMap)
	if detected != "" && detected != clientProtocol {
		if (clientProtocol == "anthropic" && detected == "openai") ||
			(clientProtocol == "openai" && detected == "anthropic") ||
			(clientProtocol != "google" && detected == "google") {
			slog.Warn("协议 Payload 不匹配", "client_protocol", clientProtocol, "detected_payload", detected)
			http.Error(w, "Protocol Payload Mismatch: You connected to /v1/"+clientProtocol+"/ but sent a "+detected+" payload.", http.StatusBadRequest)
			return
		}
	}

	modelName := extractModelName(r, reqMap)

	finalReqModel = modelName
	finalReqBody = bodyBytes

	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		activeChan, actualModel, err := s.pipeline.RouteRequest(r.Context(), modelName)
		finalActualModel = actualModel
		if activeChan != nil && activeChan.Provider != nil {
			finalProviderName = activeChan.Provider.ProviderID
			finalAccountName = activeChan.Provider.Name
		}
		if err != nil {
			if errors.Is(err, pool.ErrAllChannelsBusy) && attempt < maxRetries {
				slog.Warn("所有可用渠道均繁忙，等待重试", "attempt", attempt, "model", modelName)
				time.Sleep(500 * time.Millisecond)
				lastErr = err
				continue
			}
			skipBilling = true
			if errors.Is(err, pool.ErrAllChannelsBusy) {
				slog.Error("路由匹配失败 (所有可用渠道均繁忙/熔断/额度耗尽)", "requested_model", modelName, "error", err)
				s.writeError(w, clientProtocol, http.StatusTooManyRequests, "rate_limit_error",
					"All upstream channels are currently busy, cooling down, or exhausted. Please try again later.")
			} else {
				slog.Error("路由匹配失败 (无满足条件的可用模型/节点)", "requested_model", modelName, "error", err)
				s.writeError(w, clientProtocol, http.StatusServiceUnavailable, "invalid_request_error",
					"No available models found for the requested capability tier. Please check your system model configurations.")
			}
			return
		}

		var targetProtocol string
		var targetEndpoint *domain.SysAccessEndpoint

		priorityList := targetPriority[clientProtocol]
		lowerModel := strings.ToLower(actualModel)
		if strings.Contains(lowerModel, "gemini") || strings.Contains(lowerModel, "gemma") {
			priorityList = []string{"google", "openai", "anthropic"}
		} else if strings.Contains(lowerModel, "claude") {
			priorityList = []string{"anthropic", "openai", "google"}
		}

		for _, p := range priorityList {
			if ep, exists := activeChan.Endpoints[p]; exists {
				targetProtocol = p
				targetEndpoint = ep
				finalAPIProtocol = p
				break
			}
		}

		if targetProtocol == "" {
			slog.Error("无兼容的后端协议端点", "channel", activeChan.Provider.Name, "client_protocol", clientProtocol)
			s.chanManager.ReleaseChannel(activeChan)
			skipBilling = true
			http.Error(w, "No compatible backend protocol endpoint for channel", http.StatusBadGateway)
			return
		}

		translatorKey := clientProtocol + "_" + targetProtocol
		if targetProtocol == "anthropic" && translate.DetectBackend(activeChan.Provider, targetEndpoint) == translate.BackendDeepSeek {
			translatorKey = clientProtocol + "_deepseek"
		}

		trans := s.transFactory.Get(translatorKey)
		if trans == nil {
			slog.Warn("未找到翻译器插件", "translator_key", translatorKey, "provider", activeChan.Provider.ProviderID)
			s.chanManager.ReleaseChannel(activeChan)
			skipBilling = true
			s.writeError(w, clientProtocol, http.StatusNotImplemented, "api_error",
				"Translator not implemented: "+translatorKey)
			return
		}

		// GEAP 动态前缀：OpenAI 端点需区分 anthropic/ 和 google/ 模型前缀
		if targetProtocol == "openai" && targetEndpoint.ProviderID == "agent_platform" {
			if strings.HasPrefix(actualModel, "gemini-claude-") || strings.HasPrefix(actualModel, "claude-") {
				actualModel = "anthropic/" + actualModel
			} else {
				actualModel = "google/" + actualModel
			}
		}

		// 1. 翻译请求（纯 JSON 映射，传入 provider 而非 ActiveChannel）
		isResponsesAPI := clientProtocol == "openai" && openai.IsResponsesAPIRequest(r)
		// 官方 OpenAI 后端：Responses API 直接透传到 /v1/responses，无需格式转换
		isOpenAINative := targetProtocol == "openai" && openai.IsOpenAINativeBackend(activeChan.Provider, targetEndpoint)
		reqBytesToTranslate := bodyBytes

		if isResponsesAPI && !isOpenAINative {
			var errConv error
			reqBytesToTranslate, errConv = openai.ResponsesAPIToChatCompletions(bodyBytes, actualModel, strings.Contains(strings.ToLower(targetEndpoint.DefaultBaseURL), "deepseek"))
			if errConv != nil {
				slog.Error("ResponsesAPIToChatCompletions 失败", "error", errConv)
				s.chanManager.ReleaseChannel(activeChan)
				skipBilling = true
				http.Error(w, "Invalid Responses API payload", http.StatusBadRequest)
				return
			}
		}

		payloadBytes, targetPath, err := trans.TranslateRequest(r, reqBytesToTranslate, activeChan.Provider, targetEndpoint, actualModel)
		if err != nil {
			slog.Error("TranslateRequest 失败", "error", err)
			s.chanManager.ReleaseChannel(activeChan)
			skipBilling = true
			http.Error(w, "Failed to translate request: "+err.Error(), http.StatusBadRequest)
			return
		}

		// 2. 构建 HTTP 请求
		var targetURL string
		if strings.HasPrefix(targetPath, "http://") || strings.HasPrefix(targetPath, "https://") {
			targetURL = targetPath
		} else {
			targetURL = translate.BuildTargetURL(activeChan.Provider, targetEndpoint, targetPath)
		}

		proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(payloadBytes))
		if err != nil {
			s.chanManager.ReleaseChannel(activeChan)
			skipBilling = true
			http.Error(w, "Failed to create proxy request", http.StatusInternalServerError)
			return
		}

		translate.CopyHeaders(proxyReq.Header, r.Header)
		proxyReq.Header.Set("Content-Type", "application/json")
		proxyReq.Host = proxyReq.URL.Host
		proxyReq.Header.Del("Content-Length")

		if targetProtocol == "anthropic" && proxyReq.Header.Get("anthropic-version") == "" {
			proxyReq.Header.Set("anthropic-version", "2023-06-01")
		}

		// 3. 注入 Auth（传入 provider，不再依赖 pool.ActiveChannel）
		if err := auth.InjectAuth(r.Context(), proxyReq, activeChan.Provider, targetEndpoint); err != nil {
			slog.Error("Auth 注入失败", "error", err)
			s.chanManager.ReleaseChannel(activeChan)
			skipBilling = true
			http.Error(w, "Auth injection failed", http.StatusInternalServerError)
			return
		}

		// 4. 发送请求
		resp, err := httpclient.Client.Do(proxyReq)
		if err != nil {
			slog.Warn("上游请求网络错误，准备重试", "attempt", attempt, "error", err, "provider", activeChan.Provider.ProviderID)
			s.chanManager.ReportError(activeChan, 502)
			s.chanManager.ReleaseChannel(activeChan)
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			slog.Warn("上游返回错误状态码，准备重试", "attempt", attempt, "status", resp.StatusCode, "provider", activeChan.Provider.ProviderID)
			s.chanManager.ReportError(activeChan, resp.StatusCode)
			s.chanManager.ReleaseChannel(activeChan)
			resp.Body.Close()
			lastErr = fmt.Errorf("upstream error: %s", resp.Status)
			continue
		}

		// 4xx 错误不重试，读取错误体并记录，方便定位协议转换问题
		if resp.StatusCode >= 400 {
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			// error_body 截断，避免泄露过多上游数据
			errSnippet := string(errBody)
			if len(errSnippet) > 500 {
				errSnippet = errSnippet[:500] + "...(truncated)"
			}
			slog.Warn("上游返回 4xx 错误",
				"status", resp.StatusCode,
				"provider", activeChan.Provider.ProviderID,
				"account", activeChan.Provider.Name,
				"url", targetURL,
				"model", actualModel,
				"error_body", errSnippet,
			)
			// 仅在 debug 模式下输出完整请求体（含用户消息，注意 PII）
			if logger.IsDebugEnabled() {
				slog.Debug("4xx 上游请求体", "sent_payload", string(payloadBytes))
			}
			s.chanManager.ReportSuccess(activeChan)
			s.chanManager.ReleaseChannel(activeChan)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write(errBody)
			return
		}
		slog.Debug("上游响应正常", "status", resp.StatusCode, "provider", activeChan.Provider.ProviderID, "model", actualModel, "content_type", resp.Header.Get("Content-Type"))

		// 5. 成功
		s.chanManager.ReportSuccess(activeChan)
		s.chanManager.ReleaseChannel(activeChan)

		// 6. 翻译响应
		// 注入原始请求体到 context 中，供翻译器判断 isCompact 和预估 tokens
		r = r.WithContext(context.WithValue(r.Context(), translate.OriginalReqBodyKey{}, reqBytesToTranslate))

		// 官方 OpenAI：Responses API 直接透传，无需 Chat Completions → Responses API 包装
		if isResponsesAPI && !isOpenAINative {
			pr, pw := io.Pipe()
			mockW := openai.NewPipeResponseWriter(w, pw)
			go func() {
				err := trans.TranslateResponse(mockW, r, resp)
				mockW.Close() // Ensure headerCh is closed even on early errors
				pw.CloseWithError(err)
			}()

			mockW.WaitHeaders()

			if mockW.IsStream() {
				slog.Debug("Responses API 使用流式处理", "model", actualModel)
				openai.HandleResponsesStream(w, pr, actualModel)
			} else {
				slog.Warn("Responses API 收到非流式响应，转为同步处理", "model", actualModel, "upstream_status", mockW.Status(), "content_type", mockW.Header().Get("Content-Type"))
				openai.HandleResponsesNonStream(w, pr, actualModel)
			}
		} else {
			if err := trans.TranslateResponse(w, r, resp); err != nil {
				slog.Error("翻译响应失败", "error", err)
			}
		}
		resp.Body.Close()
		return
	}

	slog.Error("网关所有重试耗尽", "lastErr", lastErr)
	skipBilling = true
	s.writeError(w, clientProtocol, http.StatusBadGateway, "api_error",
		fmt.Sprintf("All retries failed: %v", lastErr))
}

// writeError 按客户端协议格式写入错误响应
func (s *Server) writeError(w http.ResponseWriter, clientProtocol string, statusCode int, errType, message string) {
	if clientProtocol == "anthropic" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = fmt.Fprintf(w, `{"type":"error","error":{"type":"%s","message":"%s"}}`, errType, message)
	} else {
		http.Error(w, message, statusCode)
	}
}
