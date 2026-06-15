package billing

import (
	"bytes"
	"context"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/polarisagi/hermes/internal/store"
)

var (
	priceCache sync.Map
	lastSync   time.Time
	syncMutex  sync.Mutex
)

type modelPrice struct {
	prompt1k     float64
	completion1k float64
}

func getModelPrice(modelName string) modelPrice {
	syncMutex.Lock()
	if time.Since(lastSync) > 5*time.Minute {
		// 刷新缓存
		repo := store.NewModelRepo()
		models, err := repo.GetSysModels(context.Background())
		if err == nil {
			for _, m := range models {
				priceCache.Store(m.ModelID, modelPrice{
					prompt1k:     m.PromptPricePer1k,
					completion1k: m.CompletionPricePer1k,
				})
			}
			lastSync = time.Now()
		} else {
			slog.Warn("更新模型价格缓存失败", "error", err)
		}
	}
	syncMutex.Unlock()

	if val, ok := priceCache.Load(modelName); ok {
		return val.(modelPrice)
	}
	// 如果精确模型名不存在，尝试匹配前缀
	var matchedPrice modelPrice
	var longestPrefix int
	priceCache.Range(func(key, value interface{}) bool {
		k := key.(string)
		if strings.HasPrefix(modelName, k) && len(k) > longestPrefix {
			longestPrefix = len(k)
			matchedPrice = value.(modelPrice)
		}
		return true
	})

	if longestPrefix > 0 {
		return matchedPrice
	}

	return modelPrice{prompt1k: 0.0, completion1k: 0.0}
}

// CalculateCost 根据模型名和 token 用量计算费用 (USD)
func CalculateCost(provider, modelName string, promptTokens, candidateTokens, cachedTokens int64, bodyBytes []byte) float64 {
	price := getModelPrice(modelName)
	promptRate := price.prompt1k
	candidateRate := price.completion1k

	// 默认兜底价格 (如果 sys_models 没配置)
	if promptRate == 0 && candidateRate == 0 {
		if strings.Contains(modelName, "gpt-4") || strings.Contains(modelName, "claude-3-opus") {
			promptRate = 5.0
			candidateRate = 15.0
		} else if strings.Contains(modelName, "claude-3-5") || strings.Contains(modelName, "sonnet") {
			promptRate = 3.0
			candidateRate = 15.0
		} else {
			promptRate = 1.0
			candidateRate = 2.0
		}
	}

	// 如果是通过 Vertex AI 渠道调用 Gemini，部分模型价格更贵（覆盖 AI Studio 价格）
	if provider == "google" || provider == "agent_platform" {
		if strings.Contains(modelName, "gemini-") {
			if strings.Contains(modelName, "flash") {
				// Vertex AI Gemini Flash 价格: 0.075 / 1M input, 0.30 / 1M output
				promptRate = 0.000075
				candidateRate = 0.00030
			} else if strings.Contains(modelName, "pro") {
				// Vertex AI Gemini Pro 价格: 1.25 / 1M input, 5.0 / 1M output
				promptRate = 0.00125
				candidateRate = 0.0050
			}
		}
	}

	if strings.Contains(modelName, "gemini-") && promptTokens > 128000 {
		promptRate *= 2.0
		candidateRate *= 2.0
	}

	uncachedTokens := promptTokens - cachedTokens
	if uncachedTokens < 0 {
		uncachedTokens = 0
	}

	cachedRate := promptRate * 0.50 // Default 50% discount for cached tokens

	if strings.Contains(modelName, "deepseek-") {
		cachedRate = promptRate * 0.10
	} else if strings.Contains(modelName, "gemini-") {
		cachedRate = promptRate * 0.25
	} else if strings.Contains(modelName, "claude-") {
		cachedRate = promptRate * 0.10
	}

	cost := (float64(uncachedTokens) / 1000.0 * promptRate) +
		(float64(cachedTokens) / 1000.0 * cachedRate) +
		(float64(candidateTokens) / 1000.0 * candidateRate)

	// 多模态补偿逻辑 (系数 1.05)
	if provider == "google" && len(bodyBytes) > 0 {
		hasMultimodal := bytes.Contains(bodyBytes, []byte(`"image_url"`)) ||
			bytes.Contains(bodyBytes, []byte(`"inlineData"`)) ||
			bytes.Contains(bodyBytes, []byte(`"inline_data"`)) ||
			bytes.Contains(bodyBytes, []byte(`"file_uri"`)) ||
			bytes.Contains(bodyBytes, []byte(`"fileUri"`))
		if hasMultimodal {
			cost *= 1.05
		}
	}

	return math.Ceil(cost*1000000) / 1000000
}
