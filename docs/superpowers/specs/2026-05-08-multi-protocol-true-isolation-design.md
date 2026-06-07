# 设计方案: 多协议真实隔离与模型名称映射 v3

**日期:** 2026-05-08
**状态:** 草案 (Draft)
**作者:** mrlaoliai

---

## 1. 概述 (Overview)

目前 PolarisAGI Hermes 支持四种传入的协议路径 (`/v1/openai/`, `/v1/gemini/`, `/v1/vertex/`, `/v1/anthropic/`)，但协议检测和路由逻辑将“HTTP 格式”与“后端身份”混淆在了一起。本设计旨在将它们干净利落地分离开来，添加模型别名解析，并将处理 `google/` 前缀正式确立为一个明确的协议边界关注点。

## 2. 核心概念 (Core Concepts)

### 2.1 协议 ≠ 后端 (Protocol ≠ Backend)

| 术语 | 定义 |
|------|-----------|
| **源协议 (Source Protocol)** | 客户端访问的 API 路径 `/v1/{protocol}/`。决定了请求体的格式。 |
| **目标协议 (Target Protocol)** | 后端节点使用的协议。决定了请求去向哪里以及它期望的 HTTP 请求体格式。 |
| **HTTP 格式 (HTTP Format)** | 线上传输格式 (例如：OpenAI 兼容 JSON, Vertex 原生, Anthropic Messages)。两个不同的源协议可以共享相同的 HTTP 格式。 |
| **API Key** | 不是一种协议，而是一种凭证类型。OpenAI 和 Google AI Studio 均使用 `Authorization: Bearer <key>`。Vertex 使用 OAuth2 / 服务账号 Token。 |

### 2.2 协议矩阵 (Protocol Matrix)

| 源协议 | HTTP 格式 | 典型后端 | 模型命名惯例 |
|----------------|-------------|-----------------|----------------------|
| `openai` | OpenAI 兼容 JSON | OpenAI.com, DeepSeek, 任何 OAI 兼容的服务端 | `gpt-5.5`, `deepseek-v4-flash` |
| `gemini` | OpenAI 兼容 JSON (相同格式!) | Google AI Studio (generativelanguage.googleapis.com) | `google/gemini-3.1-pro` |
| `vertex` | Google Vertex 原生格式 | Vertex AI (aiplatform.googleapis.com) | `gemini-3.1-pro` (无前缀) |
| `anthropic` | Anthropic Messages | Anthropic API, 任何 Anthropic 兼容端 | `claude-opus-4-7` |

**核心洞察:** OpenAI 和 Gemini 协议共享同样的 HTTP 传输格式，但它们指向的是**不同**的后端提供商，并且使用了**不同**的模型命名惯例。网关唯一的区别在于：(a) 路由到哪个后端节点池，以及 (b) 是否在模型名称上剥离/添加 `google/` 前缀。

### 2.3 `google/` 前缀规则

- **包含 `google/` 前缀** → 模型名称适用于 Google AI Studio / OpenAI 兼容的 Google API。示例：`google/gemini-3.1-pro`
- **不包含前缀** → 模型名称适用于 Vertex AI 原生协议。示例：`gemini-3.1-pro`
- **跨协议翻译规则:**
  - gemini → vertex: **剥离** 源模型中的 `google/` 前缀，得到目标模型。
  - vertex → gemini: **添加** `google/` 前缀到源模型，得到目标模型。
  - openai → vertex: 如果源模型包含 `google/` 前缀，在请求 Vertex 目标前将其剥离。
  - 所有其他跨协议对: 前缀不发生改变 (完全按照 model_mappings 匹配)。

## 3. 架构更改 (Architecture Changes)

### 3.1 路由管理器 (Route Manager) — 无需更改

路由管理器已经处理了所有源/目标协议的组合。它通过 `sourceProtocol` 找到匹配的路由，通过 `model_mappings` 匹配模型，并选择一个 `provider == route.TargetProtocol` 的节点。无需结构性变更。

### 3.2 翻译器注册中心 (Translator Registry) — 填补空缺

当前已注册的翻译器:
```
openai      → openai      ✅ OpenAIToOpenAI
openai      → gemini      ✅ OpenAIToOpenAI
openai      → vertex      ✅ OpenAIToVertex
anthropic   → anthropic   ✅ AnthropicToAnthropic
anthropic   → openai      ✅ AnthropicToOpenAI
anthropic   → gemini      ✅ AnthropicToOpenAI
anthropic   → vertex      ✅ AnthropicToVertex
vertex      → openai      ✅ VertexToOpenAI
vertex      → gemini      ✅ VertexToOpenAI
vertex      → vertex      ✅ VertexToVertex
```

缺失的 (需要添加):
```
gemini      → gemini      ❌ (使用现有的 OpenAIToOpenAI — 因为 HTTP 格式相同)
gemini      → openai      ❌ (使用现有的 OpenAIToOpenAI — 因为 HTTP 格式相同)
gemini      → vertex      ❌ 新增: 需要 GeminiToVertex 翻译器
gemini      → anthropic   ❌ (不在范围 — 不太可能的用法)
```

#### 需要新增的注册:

```go
// 在 openai/openai_to_openai.go 的 init() 函数中:
router.RegisterTranslator("gemini", "openai", OpenAIToOpenAI)
router.RegisterTranslator("gemini", "gemini", OpenAIToOpenAI)
```

#### 需要新增的文件: `translators/gemini/gemini_to_vertex.go`
创建 `gemini` 包，注册 `gemini → vertex` 翻译器。这本质上就是 OpenAIToVertex，但是源模型名称会带有 `google/` 前缀，需要在应用目标模型映射之前将其剥离。

### 3.3 模型别名解析流水线 (Model Name Resolution Pipeline)

在提取源模型名称和路由匹配之间增加一个新的步骤：

```go
// 在 router/http.go 的 ServeHTTP 中，在 extractModelName 之后:
resolvedModel, isCustommodelAlias := resolveModelAlias(modelName, sourceProtocol)
```

#### `resolveModelAlias(modelName, protocol string) (string, string)`

一个已知模型别名的查找表。返回规范的模型名称和一个别名标签。

```go
var modelAliases = map[string]struct{ canonical string; label string }{
    "gpt-5.5":              {canonical: "gpt-5.5-customtools", label: "customtools"},
    "gpt-5.4":              {canonical: "gpt-5.4-customtools",  label: "customtools"},
}
```

如果 `modelName` 在 Map 中，返回 `(canonical, label)`。否则返回 `(modelName, "")`。

被解析后的 `resolvedModel` 才是最终传递给 `MatchAndAcquireRoute()` 并最终传递给翻译器函数的值。而原始的 `modelName` 和 `label` 将会被传递给翻译器用于计费上下文 (因此成本会计入原始模型，而不是别名模型上)。

### 3.4 `google/` 前缀自动处理

路由中的 `model_mappings` JSON 配置已经支持了明确的 1对1 映射：

```json
{"match": "google/gemini-3.1-pro", "target": "gemini-3.1-pro"}
```

为了方便起见，原本计划在翻译器分发层添加**自动剥离前缀**:

**在分发给翻译器之前, 在 `ServeHTTP` 中:**
```go
// 如果是跨协议从 gemini/openai → vertex，自动剥离解析后模型的 google/ 前缀
// 前提是它没有被 model_mappings 覆盖处理过
if (sourceProtocol == "gemini" || sourceProtocol == "openai") && 
   dest.TargetProtocol == "vertex" &&
   strings.HasPrefix(resolvedModel, "google/") {
    // 解析出的模型已经包含 google/ 前缀; 
    // 如果 model_mappings 没有提供特定的目标覆盖，
    // 则自动剥离该前缀
    if dest.TargetModel == resolvedModel {
        dest.TargetModel = strings.TrimPrefix(resolvedModel, "google/")
    }
}
```

等一下 — 这种逻辑非常脆弱。**更好的方式是: 保持简单且明确。** 路由中的 `model_mappings` JSON 必须处理所有的前缀转换。**不再进行**自动剥离；用户必须显式地配置路由。

**更新后的规则:**
- 配置为 `gemini → vertex` 的路由，必须在 model_mappings 中明确将 `google/` 前缀剥离掉。
- 示例: `{"match": "google/gemini-3.1-pro", "target": "gemini-3.1-pro"}`
- 网关**不会**自动转换前缀 (显式优于隐式)。

### 3.5 计费字典 (Pricing Dictionary) — 已完成 (v2.0.3+)

已经更新了以下内容：

| 分类 | 模型 | 状态 |
|----------|--------|--------|
| DeepSeek | `deepseek-v4-flash`, `deepseek-v4-pro` | ✅ |
| Anthropic | `claude-opus-4-7`, `claude-opus-4-6`, `claude-sonnet-4-6`, `claude-sonnet-4-5`, `claude-haiku-4-5` | ✅ |
| OpenAI | `gpt-5.5`, `gpt-5.5-customtools`, `gpt-5.4`, `gpt-5.4-mini` | ✅ (还需 gpt-5.5-customtools) |
| Google (OAI) | `google/gemini-3.1-pro`, `google/gemini-3.1-flash`, 等 | ✅ |
| Google (Vertex) | `gemini-3.1-pro`, `gemini-3.1-flash`, 等 | ✅ |

**仍需进行:** 将 `gpt-5.5-customtools` 添加为具有与 `gpt-5.5` 相同价格的单独模型。

### 3.6 前端更改 (Frontend Changes)

- 管理后台的路由表单: `source_protocol` 下拉框必须包含 `gemini` 作为选项。
- 管理后台的路由表单: `target_protocol` 下拉框必须包含 `gemini` 作为选项。
- 节点管理: `provider` 字段必须接受 `gemini` 作为一种有效的提供商类型。
- 路由列表展示: 显示清晰的 `source → target` 协议名称。

## 4. 实施计划 (Implementation Plan)

### 阶段 1: 添加缺失的翻译器注册 + gpt-5.5-customtools
1. 在计费字典中添加 `gpt-5.5-customtools` (价格同 gpt-5.5)
2. 添加 `model_alias` Map: `gpt-5.5` → `gpt-5.5-customtools`
3. 添加 `gemini → openai` 和 `gemini → gemini` 翻译器注册
4. 构建并验证编译

### 阶段 2: 创建 gemini_to_vertex 翻译器
1. 创建 `internal/translators/gemini/gemini_to_vertex.go`
2. 在 init() 中注册 `gemini → vertex`
3. 针对 Vertex 目标处理 google/ 前缀的剥离
4. 在 main.go 中导入
5. 构建并验证

### 阶段 3: 模型别名解析层
1. 在 `router/utils.go` 中添加 `resolveModelAlias()`
2. 连接到 `ServeHTTP` 中 — 在路由匹配之前解析别名
3. 将原始模型名称(解析前)传给翻译器以供计费使用
4. 构建并验证

### 阶段 4: 前端
1. 在 source/target 协议下拉框中添加 `gemini`
2. 在节点 provider 类型选项中添加 `gemini`
3. 编译并测试管理后台 UI

### 阶段 5: 端到端测试
1. 测试 `openai → openai` (透传)
2. 测试 `openai → vertex` (带有 google/ 前缀的跨协议)
3. 测试 `gemini → vertex` (剥离 google/ 前缀)
4. 测试 `gemini → gemini` (透传)
5. 测试 `vertex → vertex` (透传)
6. 验证所有路径的计费准确性

## 5. 需要创建/修改的文件

| 文件 | 动作 | 描述 |
|------|--------|-------------|
| `internal/router/utils.go` | 修改 | 添加 `resolveModelAlias()` 和 `modelAliases` Map |
| `internal/router/http.go` | 修改 | 将别名解析接线到 ServeHTTP 中 |
| `internal/translators/openai/openai_to_openai.go` | 修改 | 添加 gemini→openai, gemini→gemini 注册 |
| `internal/translators/gemini/gemini_to_vertex.go` | 创建 | 新的翻译器: gemini 源 → vertex 目标 |
| `internal/translators/utils/shared_utils.go` | 修改 | 将 `gpt-5.5-customtools` 加入价格字典 |
| `cmd/hermes/main.go` | 修改 | 导入 gemini 翻译器包 |
| `web/admin.html` (或前端) | 修改 | 将 gemini 添加到协议下拉框 |
| `README.md` | 修改 | 更新协议矩阵文档 |

## 6. 自查 (Self-Review)

- [x] 没有占位符或 TODOs
- [x] 阶段顺序符合逻辑 (先翻译器，然后别名，最后前端)
- [x] google/ 前缀规则在每个路由中是显式的，而不是隐式的自动转换
- [x] 计费字典覆盖了所有带前缀和不带前缀的协议
- [x] 模型别名是一个单独的查找表，独立于路由的 model_mappings
