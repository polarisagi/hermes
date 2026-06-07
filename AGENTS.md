# 智能代理协作指南 (AGENTS.md)

欢迎！如果你是协助开发或维护本项目的 AI 编程助手（如 Claude、Antigravity 等），请仔细阅读本指南。本文件与 [CLAUDE.md](CLAUDE.md) 配合使用。

## 1. 我们的使命
PolarisAGI Hermes 是一个多协议的 LLM API 代理网关。我们的核心目标是将 OpenAI、Anthropic、Google 等多种异构模型的协议打平，并向客户端（如 Codex 等 AI Agent）提供纯净的、标准的 OpenAI Responses API 格式输出。

## 2. Agent 工作准则
- **协议严谨性**：网关涉及底层 HTTP SSE 数据流操作，任何对 `chat.completion.chunk` 或 `output_item.added` 事件结构的改动都必须 100% 符合 OpenAI 官方最新的 [openapi.yaml](docs/openapi/openapi.yaml) 规范。
- **全局拦截架构**：我们使用“无侵入”的全局拦截架构（在 `internal/proxy/server.go` 中）。请避免直接修改下层的翻译器结构去适配顶层网络协议。
- **文档维护优先**：当你引入了新的模型参数映射或处理逻辑时，必须同时更新 `CLAUDE.md` 并在对应的文档目录（如 `docs/architecture`）中体现。

## 3. 关联参考文档
- **行为准则与项目规范**：[CLAUDE.md](CLAUDE.md)
- **架构设计**：[docs/architecture/ARCHITECTURE.md](docs/architecture/ARCHITECTURE.md)
- **官方接口标准**：[docs/openapi/openapi.yaml](docs/openapi/openapi.yaml)

*请务必在修改流式解析相关的代码时，提前确认事件的生命周期（added -> delta -> done）及必填字段（如 item_id）。*
