# 设计规范: Anthropic 到 Vertex 协议适配器 (Protocol Adapter)

## 概述 (Overview)
本规范详细说明了在 PolarisAGI Hermes 中实现协议适配器的方案。该适配器用于将客户端传入的 Anthropic (Claude) API 请求翻译为 Google Vertex AI (Gemini) API 请求，并将 Vertex 的响应无缝转换为 Anthropic 的格式返回给客户端。这使得专为 Anthropic 设计的工具（如 Claude Dev, 使用 Anthropic 模式的 Aider）能够通过网关现有的连接池和并发管理机制，稳定地使用 Google Vertex 基础设施。

## 架构与模块设计 (Architecture & Module Design)

1.  **隔离性 (Isolation)**: 将新建一个包 `internal/proxy/anthropic`。它严格作为一个“翻译器”和“路由器”运作。
2.  **共享节点池 (Shared Node Pool)**: `anthropic` 模块**不会**管理自己的账号池。它将直接复用现有的 `vertex` 包的账号池。
    *   **重构要求 (Refactoring Requirement)**: `internal/proxy/vertex` 包必须暴露其内部的节点借用与释放函数（例如：`AcquireAccount`, `UpdateAccountStateOnSuccess`, `UpdateAccountStateOnFailure`）。
3.  **路由 (Routing)**: `cmd/polaris/main.go` 将被更新，把 `/anthropic/v1/messages` 路径的请求路由至 `anthropic.NewHandler()` 处理。

## 数据流向与翻译规则 (Data Flow & Translation Rules)

### 1. 请求载荷翻译 (Anthropic JSON -> Vertex JSON)
*   **System Prompt**: 从 Anthropic 根级别的 `system` 字段中提取，并注入到 Vertex 结构的 `systemInstruction.parts[0].text` 中。
*   **历史消息 (Messages)**:
    *   Anthropic 的 `role: "user"` 映射为 Vertex 的 `role: "user"`。
    *   Anthropic 的 `role: "assistant"` 映射为 Vertex 的 `role: "model"`。
    *   内容数组 (Content arrays) 将被映射为 Vertex 的 `parts` 数组格式。
*   **参数映射 (Parameters)**:
    *   `max_tokens` (Anthropic 中的必填项) -> 映射为 `maxOutputTokens`。
    *   `temperature`, `top_p`, `top_k` 将被直接映射进 `generationConfig` 对象中。
*   **模型指定 (Model)**: 请求中携带的模型名称将被忽略，或被重写为指向底层 Vertex 节点所配置的模型（例如：默认映射为节点的标准目标 `gemini-1.5-pro`）。

### 2. 响应翻译 (Vertex SSE -> Anthropic SSE)
这部分是核心难点，需要将 Vertex 的 Server-Sent Events (SSE) 流实时转换为伪装的 Anthropic 流。

*   **初始化 (Initialization)**: 发送 `message_start` 事件。
*   **内容块开始 (Content Start)**: 发送 `content_block_start` 事件。
*   **增量数据块 (Delta Chunks)**: 解析传入的 Vertex 数据块 `candidates[0].content.parts[0].text`，提取增量文本，并将其包装在 Anthropic 的 `content_block_delta` 结构中：
    ```json
    event: content_block_delta
    data: {"type": "content_block_delta", "index": 0, "delta": {"type": "text_delta", "text": "..."}}
    ```
*   **流结束 (Completion)**: 发送 `content_block_stop` 和 `message_stop` 事件。
*   **非流式请求 (Non-streaming)**: 如果客户端请求 `stream: false`，网关需在内存中累积完整的 Vertex 响应，并构建出一个单独的、完整的 Anthropic 响应 JSON 返回。

## 遥测监控与计费 (Telemetry & Billing)
*   适配器必须解析数据流的最后一个数据块（或流的尾部数据）以找到来自 Vertex 的 `usageMetadata`。
*   提取其中的 prompt 和 completion token 数量，计算成本，并调用现有的 `db.SaveUsage` 函数，将产生的费用精确归因到被使用的那个 Vertex 物理账号上。
*   在数据库中，这部分请求的客户端类型应被明确标记区分（例如：标识为 `Anthropic-Adapter`）。

## 错误处理 (Error Handling)
*   与 Vertex 建立连接时发生的网络错误，将通过调用暴露的 `vertex` 包方法，触发该节点的熔断冷却程序。
*   业务验证错误（来自 Vertex 的 HTTP 4xx 状态码）将被拦截，并包装成 Anthropic 标准的错误 JSON 结构返回给客户端。