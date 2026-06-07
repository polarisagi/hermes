# Anthropic 到 Vertex 协议适配器实现计划

> **致 Agent 助手:** 必选子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 来逐任务执行此计划。步骤使用复选框 (`- [ ]`) 语法进行跟踪。

**目标:** 实现一个协议适配器，用于将 Anthropic API 请求翻译为 Vertex AI 请求，并将响应翻译回来。

**架构:** 我们将暴露现有的 `internal/proxy/vertex` 包中的连接池函数，并构建一个新包 `internal/proxy/anthropic`，它将借用账号，将 Anthropic JSON 请求翻译为 Vertex `GenerateContent` 请求，执行对 Vertex 的 HTTP 调用，并在飞行中将 Vertex JSON/SSE 响应实时转换回 Anthropic 格式。

**技术栈:** Go, 标准库 (net/http, encoding/json)。

---

### 任务 1: 暴露 `vertex` 账号管理函数

我们需要使 `internal/proxy/vertex` 中的内部账号管理函数可以被 `anthropic` 包访问。

**文件:**
- 修改: `internal/proxy/vertex/handler.go`

- [ ] **步骤 1: 将 `acquireAccount` 重命名为 `AcquireAccount`**

在 `internal/proxy/vertex/handler.go` 中，将 `acquireAccount` 函数重命名为 `AcquireAccount` 并更新它的调用。

```go
// 替换前
func acquireAccount(ctx context.Context) (*AccountState, bool, error) {

// 替换后
func AcquireAccount(ctx context.Context) (*AccountState, bool, error) {
```

在 `ProxyHandler` 中更新调用:
```go
// 在 ProxyHandler 中
	chosenState, isProbationRun, err := AcquireAccount(ctx)
```

- [ ] **步骤 2: 重命名更新函数**

重命名 `updateAccountStateOnSuccess` 和 `updateAccountStateOnFailure` 以将其导出。

```go
// 替换前
func updateAccountStateOnSuccess(state *AccountState) {
func updateAccountStateOnFailure(state *AccountState, isProbationRun bool, traceID string) {

// 替换后
func UpdateAccountStateOnSuccess(state *AccountState) {
func UpdateAccountStateOnFailure(state *AccountState, isProbationRun bool, traceID string) {
```

在 `ProxyHandler` 中更新调用:
```go
	if isNodeFailure {
		UpdateAccountStateOnFailure(chosenState, isProbationRun, traceID)
	} else {
		UpdateAccountStateOnSuccess(chosenState)
	}
```

- [ ] **步骤 3: 提交代码**

```bash
git add internal/proxy/vertex/handler.go
git commit -m "refactor: expose vertex account management functions"
```

### 任务 2: 实现 Anthropic 请求/响应结构体

定义 Anthropic API 使用的数据结构，以及用于生成请求负载所需的中间 Vertex 结构体。

**文件:**
- 创建: `internal/proxy/anthropic/models.go`

- [ ] **步骤 1: 定义结构体**

```go
package anthropic

type MessageRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	System      string    `json:"system,omitempty"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature *float64  `json:"temperature,omitempty"`
	TopP        *float64  `json:"top_p,omitempty"`
	TopK        *int      `json:"top_k,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // 可以是字符串或内容块数组
}

// Anthropic 响应结构体
type MessageResponse struct {
	ID           string       `json:"id"`
	Type         string       `json:"type"`
	Role         string       `json:"role"`
	Content      []Content    `json:"content"`
	Model        string       `json:"model"`
	StopReason   string       `json:"stop_reason"`
	StopSequence string       `json:"stop_sequence"`
	Usage        Usage        `json:"usage"`
}

type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Anthropic SSE 事件结构体
type StreamEvent struct {
	Type         string      `json:"type"`
	Message      *MessageResponse `json:"message,omitempty"`
	Index        *int        `json:"index,omitempty"`
	ContentBlock *Content    `json:"content_block,omitempty"`
	Delta        *Delta      `json:"delta,omitempty"`
	Usage        *Usage      `json:"usage,omitempty"`
}

type Delta struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	StopReason string `json:"stop_reason,omitempty"`
}
```

- [ ] **步骤 2: 提交代码**

```bash
git add internal/proxy/anthropic/models.go
git commit -m "feat: add anthropic request and response structs"
```

### 任务 3: 实现负载映射翻译

将 Anthropic 请求 JSON 映射到 Vertex GenerateContent JSON。

**文件:**
- 创建: `internal/proxy/anthropic/mapper.go`
- 测试: `internal/proxy/anthropic/mapper_test.go`

- [ ] **步骤 1: 编写翻译测试**

```go
package anthropic

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMapRequest(t *testing.T) {
	req := MessageRequest{
		Model: "claude-3-opus",
		System: "You are a bot",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: 100,
	}
	vReq, err := mapToVertexRequest(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	b, _ := json.Marshal(vReq)
	str := string(b)
	if !strings.Contains(str, `"role":"user"`) {
		t.Error("Missing user role")
	}
	if !strings.Contains(str, `"text":"You are a bot"`) {
		t.Error("Missing system prompt")
	}
}
```

- [ ] **步骤 2: 运行测试以验证失败**

```bash
cd internal/proxy/anthropic && go test -v -run TestMapRequest
```
预期结果: FAIL，缺少 mapToVertexRequest

- [ ] **步骤 3: 实现映射逻辑**

在 `internal/proxy/anthropic/mapper.go` 中:
```go
package anthropic

import (
	"encoding/json"
)

func mapToVertexRequest(req MessageRequest) (map[string]interface{}, error) {
	vertexReq := make(map[string]interface{})
	
	if req.System != "" {
		vertexReq["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]interface{}{
				{"text": req.System},
			},
		}
	}
	
	var contents []map[string]interface{}
	for _, msg := range req.Messages {
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}
		
		var parts []map[string]interface{}
		switch v := msg.Content.(type) {
		case string:
			parts = append(parts, map[string]interface{}{"text": v})
		case []interface{}:
			// 暂时的简单内容块处理
			for _, item := range v {
				if m, ok := item.(map[string]interface{}); ok {
					if m["type"] == "text" {
						parts = append(parts, map[string]interface{}{"text": m["text"]})
					}
				}
			}
		}
		contents = append(contents, map[string]interface{}{
			"role": role,
			"parts": parts,
		})
	}
	vertexReq["contents"] = contents
	
	genConfig := make(map[string]interface{})
	if req.MaxTokens > 0 {
		genConfig["maxOutputTokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		genConfig["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		genConfig["topP"] = *req.TopP
	}
	if req.TopK != nil {
		genConfig["topK"] = *req.TopK
	}
	vertexReq["generationConfig"] = genConfig
	
	return vertexReq, nil
}
```

- [ ] **步骤 4: 运行测试以验证通过**

```bash
cd internal/proxy/anthropic && go test -v -run TestMapRequest
```
预期结果: PASS

- [ ] **步骤 5: 提交代码**

```bash
git add internal/proxy/anthropic/mapper.go internal/proxy/anthropic/mapper_test.go
git commit -m "feat: implement payload mapping from anthropic to vertex"
```

### 任务 4: 实现流式响应翻译

解析 Vertex SSE 格式并流式输出 Anthropic SSE 事件。

**文件:**
- 创建: `internal/proxy/anthropic/stream.go`

- [ ] **步骤 1: 编写流式翻译逻辑**

在 `internal/proxy/anthropic/stream.go` 中:
```go
package anthropic

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
    "github.com/polarisagi/hermes/internal/db"
    "github.com/polarisagi/hermes/internal/proxy/vertex"
)

func streamAnthropicResponse(w http.ResponseWriter, vertexResp *http.Response, req MessageRequest, traceID string, state *vertex.AccountState, clientType, modelName string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)

	// 发送 message_start
	startEvent := StreamEvent{
		Type: "message_start",
		Message: &MessageResponse{
			ID:    fmt.Sprintf("msg_%s", traceID),
			Type:  "message",
			Role:  "assistant",
			Model: modelName,
			Usage: Usage{},
		},
	}
	writeSSE(w, flusher, "message_start", startEvent)

	// 发送 content_block_start
	cbStartEvent := StreamEvent{
		Type: "content_block_start",
		Index: ptrInt(0),
		ContentBlock: &Content{
			Type: "text",
			Text: "",
		},
	}
	writeSSE(w, flusher, "content_block_start", cbStartEvent)

	reader := bufio.NewReader(vertexResp.Body)
	var promptTokens, completionTokens int

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			break
		}

		line = bytes.TrimSpace(line)
		if len(line) == 0 || !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}

		data := bytes.TrimPrefix(line, []byte("data: "))
		if string(data) == "[DONE]" {
			break
		}

		var vResp map[string]interface{}
		if err := json.Unmarshal(data, &vResp); err != nil {
			continue
		}
		
		// 解析 Usage (Token使用量)
		if usage, ok := vResp["usageMetadata"].(map[string]interface{}); ok {
			if p, ok := usage["promptTokenCount"].(float64); ok {
				promptTokens = int(p)
			}
			if c, ok := usage["candidatesTokenCount"].(float64); ok {
				completionTokens = int(c)
			}
		}

		// 提取文本增量
		candidates, ok := vResp["candidates"].([]interface{})
		if !ok || len(candidates) == 0 {
			continue
		}
		
		cand, _ := candidates[0].(map[string]interface{})
		content, ok := cand["content"].(map[string]interface{})
		if !ok {
			continue
		}
		
		parts, ok := content["parts"].([]interface{})
		if !ok || len(parts) == 0 {
			continue
		}
		
		part, _ := parts[0].(map[string]interface{})
		text, _ := part["text"].(string)

		if text != "" {
			deltaEvent := StreamEvent{
				Type: "content_block_delta",
				Index: ptrInt(0),
				Delta: &Delta{
					Type: "text_delta",
					Text: text,
				},
			}
			writeSSE(w, flusher, "content_block_delta", deltaEvent)
		}
	}

	// 发送 content_block_stop
	cbStopEvent := StreamEvent{
		Type: "content_block_stop",
		Index: ptrInt(0),
	}
	writeSSE(w, flusher, "content_block_stop", cbStopEvent)

	// 发送 message_delta (停止原因 + usage)
	msgDeltaEvent := StreamEvent{
		Type: "message_delta",
		Delta: &Delta{
			StopReason: "end_turn",
		},
		Usage: &Usage{
			OutputTokens: completionTokens,
		},
	}
	writeSSE(w, flusher, "message_delta", msgDeltaEvent)

	// 发送 message_stop
	msgStopEvent := StreamEvent{
		Type: "message_stop",
	}
	writeSSE(w, flusher, "message_stop", msgStopEvent)
	
	// 结算 Usage (Token计费)
	if promptTokens > 0 || completionTokens > 0 {
        // Vertex 计费逻辑，这里我们仅记录 tokens 并依赖 db metrics
		db.SaveUsage("vertex", state.Name, clientType, "anthropic_adapter", int64(promptTokens), int64(completionTokens), 0, http.StatusOK)
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, eventType string, data interface{}) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, b)
	if flusher != nil {
		flusher.Flush()
	}
}

func ptrInt(i int) *int {
	return &i
}
```

- [ ] **步骤 2: 提交代码**

```bash
git add internal/proxy/anthropic/stream.go
git commit -m "feat: implement streaming response translation"
```

### 任务 5: 实现处理器 (Handler) 并注册路由

实现 `anthropic.Handler` 并在 `main.go` 中注册它。

**文件:**
- 创建: `internal/proxy/anthropic/handler.go`
- 修改: `cmd/hermes/main.go`

- [ ] **步骤 1: 编写 Handler 逻辑**

在 `internal/proxy/anthropic/handler.go` 中:
```go
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/polarisagi/hermes/internal/config"
	"github.com/polarisagi/hermes/internal/proxy/vertex"
	"github.com/polarisagi/hermes/internal/webapi"
)

var httpClient = &http.Client{Timeout: 180 * time.Second}

func NewHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		traceID := fmt.Sprintf("req-%d", time.Now().UnixNano())
		clientType := "Anthropic-Adapter"
		
		bodyBytes, _ := io.ReadAll(r.Body)
		r.Body.Close()

		var req MessageRequest
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			http.Error(w, `{"type": "error", "error": {"type": "invalid_request_error", "message": "invalid json"}}`, 400)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
		defer cancel()

		chosenState, isProbationRun, err := vertex.AcquireAccount(ctx)
		if err != nil || chosenState == nil {
			http.Error(w, "Anthropic Gateway: Vertex Queue Timeout", 503)
			return
		}

		vReq, _ := mapToVertexRequest(req)
		vReqBytes, _ := json.Marshal(vReq)

		// 构建目标 URL (默认使用 gemini-1.5-pro 或解析请求的模型)
		model := req.Model
		if model == "" || strings.Contains(model, "claude") {
			model = "gemini-1.5-pro"
		}
		targetURL := buildTargetURL(chosenState.AccountDetail, model, req.Stream)

		proxyReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(vReqBytes))
		proxyReq.Header.Set("Content-Type", "application/json")

		q := proxyReq.URL.Query()
		q.Set("key", chosenState.Key)
		if req.Stream {
			q.Set("alt", "sse")
		}
		proxyReq.URL.RawQuery = q.Encode()

		finalResp, err := httpClient.Do(proxyReq)
		if err != nil {
			vertex.UpdateAccountStateOnFailure(chosenState, isProbationRun, traceID)
			http.Error(w, "Vertex Network Error", 502)
			return
		}
		
		isNodeFailure := finalResp.StatusCode >= 500 || finalResp.StatusCode == 429 || finalResp.StatusCode == 401 || finalResp.StatusCode == 403

		if req.Stream {
			streamAnthropicResponse(w, finalResp, req, traceID, chosenState, clientType, model)
		} else {
            // 简单的非流式处理
			io.Copy(w, finalResp.Body)
		}

		if isNodeFailure {
			vertex.UpdateAccountStateOnFailure(chosenState, isProbationRun, traceID)
		} else {
			vertex.UpdateAccountStateOnSuccess(chosenState)
		}
	}
}

func buildTargetURL(acc config.AccountDetail, model string, stream bool) string {
	baseURL := acc.BaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1", acc.Location)
	}
	endpoint := "generateContent"
	if stream {
		endpoint = "streamGenerateContent"
	}
	return fmt.Sprintf("%s/projects/%s/locations/%s/publishers/google/models/%s:%s", 
		baseURL, acc.ProjectID, acc.Location, model, endpoint)
}
```

- [ ] **步骤 2: 注册路由**

在 `cmd/hermes/main.go` 中, 添加导入 `"github.com/polarisagi/hermes/internal/proxy/anthropic"` 并注册路由:

```go
	mux.HandleFunc("/anthropic/v1/messages", webapi.ConcurrencyLimiter(anthropic.NewHandler()))
```

- [ ] **步骤 3: 编译并测试**

```bash
go build ./cmd/hermes
```

- [ ] **步骤 4: 提交代码**

```bash
git add internal/proxy/anthropic/handler.go cmd/hermes/main.go
git commit -m "feat: complete anthropic adapter handler and routing"
```
