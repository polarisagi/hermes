package billing

import (
	"log/slog"

	"github.com/polarisagi/hermes/internal/domain"
	"github.com/polarisagi/hermes/internal/store"
)

// ProcessBilling 根据不同的响应状态进行费用计算并存储日志
func ProcessBilling(
	providerName string,
	accountName string,
	apiProtocol string,
	clientName string,
	clientModelID string,
	actualModelID string,
	reqBody []byte,
	responseContent string,
	upstreamPromptTokens int,
	upstreamCompletionTokens int,
	upstreamCachedTokens int,
	latencyMs int,
	statusCode int,
	errorMsg string,
	reqMethod string,
	reqPath string,
) {
	promptTokens := int64(upstreamPromptTokens)
	completionTokens := int64(upstreamCompletionTokens)
	cachedTokens := int64(upstreamCachedTokens)
	source := "upstream"

	// 如果服务端未提供 token 数据，则使用本地计算兜底
	if promptTokens == 0 && completionTokens == 0 && statusCode == 200 {
		promptTokens = EstimatePromptTokens(reqBody)
		if responseContent != "" {
			completionTokens = EstimateCompletionTokens(responseContent)
		} else {
			completionTokens = 0
		}
		source = "calculated fallback"
	}

	totalTokens := promptTokens + completionTokens

	// 计算费用
	cost := CalculateCost(providerName, actualModelID, promptTokens, completionTokens, cachedTokens, reqBody)

	// 打印明确的计费和 Token 消耗日志
	slog.Info("💰 [Billing] 请求计费统计",
		"client", clientName,
		"method", reqMethod,
		"path", reqPath,
		"model", actualModelID,
		"latency_ms", latencyMs,
		"prompt_tokens", promptTokens,
		"cached_tokens", cachedTokens,
		"completion_tokens", completionTokens,
		"total_tokens", totalTokens,
		"cost_usd", cost,
		"source", source,
	)

	// 异步写入计费流水到数据库
	logEntry := &domain.AccountLog{
		AccountName:      accountName,
		APIProtocol:      apiProtocol,
		ClientName:       clientName,
		RequestedModelID: clientModelID,
		ActualModelID:    actualModelID,
		PromptTokens:     int(promptTokens),
		CompletionTokens: int(completionTokens),
		TotalTokens:      int(totalTokens),
		LatencyMs:        latencyMs,
		StatusCode:       statusCode,
		ErrorMsg:         errorMsg,
		Cost:             cost,
	}

	store.GetAccountLogRepo().SaveAsync(logEntry)
}
