# CLAUDE.md

本文件为 Claude Code 提供项目协作规范与架构导读。
> 关于各类 AI 编程助手的详细行为准则与背景上下文，请参阅扩展阅读：[AGENTS.md](AGENTS.md)

## AI 协作十二定律 (交互纪律)

**🖐 管手：行为约束**
1. **先想再写**：遇不确定必须提问，禁止静默假设。
2. **最小可行代码**：交付最少代码，禁止超前抽象和过度复杂化。
3. **外科手术式修改**：仅触碰必需代码，禁止越界“顺手”重构。
4. **目标驱动执行**：动手前定义成功标准，完成后验证是否达标。

**🧠 管脑：边界与认知**
5. **模型只做判断型任务**：避免用于确定性转换，多做分类、起草、总结、提取。
6. **Token 预算是上限**：严控预算，避免长上下文导致质量断崖。
7. **暴露冲突，不要调和**：当意见矛盾时选其一并解释，禁止“取平均值”的缝合方案。
8. **写之前先读**：添加代码前必须读取直接调用方、共享工具等上下文。

**🗣 管嘴：沟通与验证**
9. **测试验证意图**：测试需编码“WHY”而非仅“WHAT”，守住业务本质。
10. **每步之后存档**：多步任务完成后总结：做了什么、验证了什么、剩下什么。
11. **遵守代码库约定**：一致性大于个人品味，严格遵守现有规范。
12. **大声失败**：不确定是否完成或遇阻时必须明确说出，拒绝静默完成。

## 语言与格式规范

- **[强制] 全部输出使用中文**，包括分析推理、技术讨论、文档与架构决策说明。
- **输出要求**：精简直接，落盘优先。禁止问候、解释、确认语。结论前置，依据紧随。
- **代码注释**：中文，说明"为什么"——设计意图、约束条件、非显而易见的逻辑。
- **代码标识符**：英文（遵循 Go 社区惯例），命名清晰自解释。
- **提交信息**：中文简述，`type(scope): 描述` 格式。
- **日志格式**：统一使用 `slog.Error/Warn/Info/Debug("固定中文消息", "key", val)` 结构化格式；消息串禁止拼接动态值，所有变量均以 key-value 参数传入。

## 构建与运行

```bash
make build              # 当前平台编译
make build-all          # 交叉编译全平台
go run ./cmd/hermes     # 开发模式启动
make run-test           # 本地测试模式启动 (监听 28889 端口，避免与线上 27777 冲突)
go test ./...           # 运行全部测试
go build ./...          # 仅做编译检查
```
数据目录：`~/.polarisagi/hermes/`（SQLite 数据库）  
管理后台：`http://127.0.0.1:27777`

> **🤖 AI 编程客户端本地测试须知**：
> 如果你需要测试修改后的网关代码，**严禁**直接运行在默认的 `27777` 端口（会导致与正在使用的生产网关冲突，使你自己断网）。
> 请**务必**使用 `make run-test` 或 `TEST_MODE=true go run ./cmd/hermes` 来启动本地测试网关，这会将其监听在 `28889` 端口。测试你的请求时，也将目标 URL 修改为 `http://127.0.0.1:28889/...`。

## 项目概览 & 架构

> 详细的系统分层、核心请求链路及架构设计理念请参阅 [docs/architecture/ARCHITECTURE.md](docs/architecture/ARCHITECTURE.md) 架构设计文档。
> 另外，关于 OpenAI Responses API 等接口的官方参考资料及 OpenAPI 规范存放在 `docs/openapi/` 目录下（如 [openapi.yaml](docs/openapi/openapi.yaml)）。

**PolarisAGI Hermes** 是多协议 LLM API 代理网关，支持 OpenAI、Anthropic、Google Agent Platform 互转，提供负载均衡、熔断保护与用量计费，规避单账号速率限制。

### 请求处理流程
1. HTTP 入口检测源协议 -> 2. 提取模型名 -> 3. Proxy 重试循环与节点抢占 -> 4. Auth 鉴权注入与 HTTP 发送 -> 5. 纯净协议转换 (TranslateRequest/Response) 与状态归还。

### 目录结构
- `internal/app/`：依赖注入与应用生命周期
- `internal/proxy/`：网关数据面（重试循环、入口 HTTP）
- `internal/pool/`：节点池调度与状态机
- `internal/router/`：路由管线（Pipeline）与意图推断
- `internal/auth/`：动态全局鉴权头注入
- `internal/translate/`：纯净协议转换层（仅做 JSON payload 与格式映射）
- `internal/domain/`：全局模型、领域结构与系统常量
- `internal/store/`：SQLite 持久化（含 migrations）
- `internal/sync/`：外部模型字典同步与晋升
- `internal/clientcfg/`：AI 客户端一键配置注入
- `internal/api/`：管理面板 RESTful API

### 核心机制
- **协议转换**：支持 Anthropic/OpenAI/Google 间的互相转换及直通。
- **节点状态机**：Idle -> Busy -> Idle。失败则 Cooldown（指数退避），随后 Probation（试探），多次失败则 Exhausted（隔离）。
- **并发与限流**：节点互斥锁防并发超限（防 429）。
- **热重载**：配置存 SQLite，后台修改后触发内存重建。
- **计费**：优先使用上游 Token 计数，异步写入 DB，超限自动 Exhausted。
- **Anthropic -> Gemini 转换约束**：
  - 清洗不支持的 JSON Schema 关键字。
  - 对话必须 user/model 交替，合并连续同角色消息。
  - 工具调用响应名须一致。思考响应则转换为 Anthropic thinking 块。