# PolarisAGI-Hermes 🌌

[![Go Report Card](https://goreportcard.com/badge/github.com/polarisagi/hermes)](https://goreportcard.com/report/github.com/polarisagi/hermes)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPL_v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![Release](https://img.shields.io/github/v/release/polarisagi/hermes)](https://github.com/polarisagi/hermes/releases)

<p align="center">
  <a href="README.md">🇬🇧 English</a> | <strong>🇨🇳 简体中文</strong>
</p>

---

**PolarisAGI-Hermes** 是一款轻量级、智能化的 **全能型大语言模型 API 代理分发与并发控制网关**。

它从最初的 Google Vertex AI 账号适配器，全面进化为**通用 AI 网关**。原生支持拦截、转换和路由几乎所有主流协议（OpenAI、Google Gemini 原生、Google Agent Platform、Anthropic，以及 Ollama/vLLM 本地模型双路协议），内置包含全球 **30+ 家**大模型厂商配置的即用型字典。完美解决因单一 API Key 限流、封禁或余额不足造成的业务中断问题。

最新版本为 **零配置启动 (Zero-Config)**、**SQLite 数据库驱动**，内置 **Web 管理面板**，提供开箱即用的商用级体验。

---

### ✨ 核心特性

1. **🔑 一键客户端配置（重点推荐！）**
   打破 Claude Code、Codex 等商业 AI 工具锁定官方 API 的限制！在管理面板一键注入代理，**让您在这些封闭软件中自由使用自己的 API Key**。
   > 🔥 **特别推荐搭配 [DeepSeek](https://platform.deepseek.com) 使用**！DeepSeek 全面兼容 OpenAI 协议，性能卓越、性价比极高。配合 PolarisAGI-Hermes 的多账号轮询与智能熔断，实现极致流畅的 AI 编程体验。

2. **🌐 全协议全厂商支持**
   内置 30+ 家主流大模型厂商数据字典（OpenAI、Anthropic、DeepSeek、智谱、月之暗面、通义千问、豆包、混元、千帆、Mistral、Groq、SiliconFlow 等），开箱即用，无需手动填写 Base URL。

3. **🎛️ 简单/专业双模式 UI**
   **简单模式**：精简表单，快速上手；**专业模式**：解锁全部控制项，精细化管理并发、计费、模型映射等高级配置。

4. **🔀 双轨制模型映射引擎**
   - **极简模式（智能语义匹配）**：自动识别模型档次，跨协议智能映射，对客户端完全透明。
   - **专业模式（1对1 精确映射）**：手动配置强制硬连接，支持正则拦截，绝对控制。

5. **🛡️ 多账号池与物理级单并发隔离**
   基于物理账号池排队，单账号同一时间只处理一个请求，极大降低 API 封禁风险。

6. **⚡ 五态智能熔断与自动重试轮询**
   🟢 Idle → 🟡 Busy → 🔴 Cooldown → 🟠 Probation → ⚫ Exhausted。网关层内置全局重试循环，遇到上游 429/5xx 错误时自动无缝切换备用节点，并对故障节点实施指数退避，试探成功后无感恢复。

7. **💰 精细化计费与水位拦截**
   内置 SQLite 追踪 Token 消费，可设置最高消费比例，防止资金烧穿。

8. **🚀 极简部署**
   单文件无依赖，自带 Web UI 及嵌入式数据迁移，直接运行。

---

### 🔀 协议路由矩阵

| 源协议（客户端发送） | 目标协议（上游厂商） | 说明 |
|------------|------------|------|
| openai | openai | 透传 + 多账号轮询（适用于所有 OpenAI 兼容厂商） |
| openai | anthropic | OpenAI → Anthropic 跨协议转换 |
| openai | google | OpenAI → Google Gemini/Agent Platform 转换 |
| anthropic | anthropic | 透传 + 多账号轮询 |
| anthropic | openai | Anthropic → OpenAI 格式转换 |
| anthropic | google | Anthropic → Google 格式转换 |
| google | google | 透传 + 多账号轮询 |
| local | local | 本地模型透传（Ollama/vLLM，无鉴权） |

---

### 📂 默认数据目录

所有配置、账单记录和 SQLite 数据库均安全保存在：`~/.polarisagi/hermes/`

---

### 🚀 快速安装

> **💡 网络连通性说明**：我们的安装脚本内置了**智能国内加速机制**。当检测到访问 GitHub 超时，脚本会自动切换至国内镜像节点（ghproxy.net）下载安装包，确保国内用户顺畅安装。

**macOS / Linux:**
```bash
# 推荐命令（已包含智能网络降级下载）
curl -sSL https://raw.githubusercontent.com/polarisagi/hermes/main/scripts/install.sh | bash

```

**Windows:**
```powershell
# 推荐命令（已包含智能网络降级下载）
irm https://raw.githubusercontent.com/polarisagi/hermes/main/scripts/install.ps1 | iex

```
*安装完成后，PolarisAGI-Hermes 将作为后台服务自动运行，并在每次用户登录时自启。*

---

### 🛠️ 开始使用

网关启动后，默认监听 `127.0.0.1:27777` 端口。

1. **登录 Admin Panel**: 访问 [http://127.0.0.1:27777](http://127.0.0.1:27777)
2. **配置渠道账号**: 选择协议类型 → 选择厂商 → 输入 API Key → 保存。
3. **一键配置客户端**: 在"客户端配置"页面，选择目标软件 → 选择注入的 API Key → 一键完成！
4. **调用 API**（将您的 AI 工具指向以下地址，API Key 填任意值）：
   - OpenAI 协议: `http://127.0.0.1:27777/v1/openai/`
   - Anthropic 协议: `http://127.0.0.1:27777/v1/anthropic/`
   - Google 协议: `http://127.0.0.1:27777/v1/google/`

---

### 🧪 本地测试模式

在本地测试修改后的代码时，请使用测试模式（监听在 `28889` 端口），避免与生产网关冲突：

```bash
make run-test
# 或:
TEST_MODE=true go run ./cmd/hermes
```

---

### 🗑️ 一键卸载

**macOS / Linux:**
```bash
# 默认
curl -sSL https://raw.githubusercontent.com/polarisagi/hermes/main/scripts/uninstall.sh | bash

```
**Windows:**
```powershell
# 默认
irm https://raw.githubusercontent.com/polarisagi/hermes/main/scripts/uninstall.ps1 | iex

```
> **注意**: 卸载只移除服务和二进制，数据保留在 `~/.polarisagi/hermes/`，如需彻底清理请手动删除。

---

### 📄 开源协议
GNU Affero General Public License v3.0 (AGPL-3.0). *(如果您使用了此代码，请保留原作者信息：`mrlaoliai`)*

---

### 🌐 链接与联系方式
* **官方网站**: [https://polarisagi.online/](https://polarisagi.online/)
* **GitHub 仓库**: [https://github.com/polarisagi/hermes](https://github.com/polarisagi/hermes)
* **作者 / 创作者**: `mrlaoliai` (欢迎在 小红书、抖音、TikTok、X 平台关注同名账号)
* **联系邮箱**: [polarisagi.online@gmail.com](mailto:polarisagi.online@gmail.com)
