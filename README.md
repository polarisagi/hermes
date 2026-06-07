# PolarisAGI-Hermes 🌌

[![Go Report Card](https://goreportcard.com/badge/github.com/polarisagi/hermes)](https://goreportcard.com/report/github.com/polarisagi/hermes)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPL_v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![Release](https://img.shields.io/github/v/release/polarisagi/hermes)](https://github.com/polarisagi/hermes/releases)

<p align="center">
  <strong>🇬🇧 English</strong> | <a href="README_zh.md">🇨🇳 简体中文</a>
</p>

---

**PolarisAGI-Hermes** is a lightweight, highly intelligent **Universal LLM API Proxy & Concurrency Control Gateway**. 

Originally designed as a Google Vertex AI adapter, it has completely evolved into a **Universal AI Gateway**. It natively supports proxying, format conversion, and routing across almost all mainstream protocols (OpenAI, Google Gemini, Google Agent Platform, Anthropic, and Local models like Ollama/vLLM). It comes with a massive built-in dictionary covering **30+ global model providers** out-of-the-box. 

It completely solves business interruptions caused by API Key rate limits, bans, or depleted balances by utilizing multi-account rotation and intelligent concurrency queuing. The latest version is purely **Zero-Config**, driven by an embedded **SQLite** database, and comes with a built-in **Web Admin Dashboard**.

> **🌟 Why the name Hermes?**
> In Greek mythology, Hermes is the swift messenger of the gods and the patron of boundaries. Similarly, **PolarisAGI-Hermes** acts as the ultimate "messenger" in the AI world—seamlessly translating complex, heterogeneous AI protocols (OpenAI, Anthropic, Gemini) and guiding requests swiftly and reliably across boundaries, breaking the silos between different AI providers.

---

### ✨ Core Features

1. **🔑 One-Click Client Auto-Config (Highly Recommended!)**
   Break the restrictions of commercial AI clients (like Claude Code, Codex, Cursor) that lock you into official APIs! The Admin Panel lets you instantly inject PolarisAGI-Hermes proxy settings, **allowing you to freely use your own API Keys and third-party models in closed software.**
   > 🔥 **Pro Tip: We highly recommend pairing this with [DeepSeek](https://platform.deepseek.com)**! DeepSeek is fully compatible with the OpenAI protocol and offers incredible performance at a disruptive price. When combined with PolarisAGI-Hermes' multi-account rotation and circuit breakers, you achieve the ultimate seamless AI coding experience.

2. **🌐 Universal Protocol & Provider Support**
   Includes a built-in, ready-to-use dictionary of 30+ global AI providers (OpenAI, Anthropic, DeepSeek, Zhipu, Moonshot, Qwen, Doubao, Mistral, Groq, SiliconFlow, etc.). No need to manually dig up Base URLs anymore.

3. **🎛️ Simple / Pro Dual-Mode UI**
   **Simple Mode**: Streamlined forms for quick setup. **Pro Mode**: Unlocks full control over concurrency limits, billing alerts, intelligent retries, and advanced model mappings.

4. **🔀 Dual-Track Model Mapping Engine**
   - **Minimalist Mode (Intelligent Semantic Mapping)**: Automatically identifies model tiers and intelligently maps cross-protocol requests transparently.
   - **Pro Mode (1-to-1 Exact Mapping)**: Allows manual hardcoded routing rules with Regex support for absolute control.

5. **🛡️ Multi-Account Pool & Single Concurrency Isolation**
   Requests are queued based on physical accounts. Strict physical-level single-concurrency isolation prevents API bans (especially crucial for Google/Anthropic endpoints).

6. **⚡ 5-State Circuit Breaker & Auto-Retry Rotation**
   🟢 Idle → 🟡 Busy → 🔴 Cooldown → 🟠 Probation → ⚫ Exhausted. A built-in gateway retry loop handles upstream 429/5xx errors by seamlessly rotating to backup nodes while applying exponential backoff to failing nodes, ensuring graceful recovery.

7. **💰 Billing & Quota Management**
   Tracks token usage via SQLite. Supports setting maximum spend percentages to auto-disable accounts near exhaustion.

8. **🚀 Zero Dependency Deployment**
   Single binary, built-in Web UI, embedded DB migrations. Just run it!

---

### 🔀 Protocol Route Matrix

| Source Protocol (Client) | Target Protocol (Upstream) | Description |
|--------------------------|----------------------------|-------------|
| openai                   | openai                     | Passthrough + Rotation (Works for all OpenAI-compatible APIs) |
| openai                   | anthropic                  | OpenAI → Anthropic format conversion |
| openai                   | google                     | OpenAI → Google Gemini / Agent Platform conversion |
| anthropic                | anthropic                  | Passthrough + Rotation |
| anthropic                | openai                     | Anthropic → OpenAI format conversion |
| anthropic                | google                     | Anthropic → Google format conversion |
| google                   | google                     | Passthrough + Rotation |
| local                    | local                      | Local model passthrough (Ollama/vLLM, no auth) |

---

### 📂 Default Directory
All configurations, billing records, and SQLite databases (`polarisagi_hermes.db`) are safely stored in:
`~/.polarisagi/hermes/`

---

### 🚀 Quick Install

> **💡 Network Connectivity Notice**: Our installation scripts have a built-in **smart fallback mechanism for Mainland China users**. If the script detects a GitHub timeout, it will automatically switch to a domestic proxy node (`ghproxy.net`) to ensure a smooth download.

**macOS / Linux:**
```bash
# Recommended (Includes smart network fallback)
curl -sSL https://raw.githubusercontent.com/polarisagi/hermes/main/scripts/install.sh | bash

# Mainland China Fallback 1 (ghfast mirror)
curl -sSL https://ghfast.top/https://raw.githubusercontent.com/polarisagi/hermes/main/scripts/install.sh | bash

# Mainland China Fallback 2 (ghproxy mirror)
curl -sSL https://mirror.ghproxy.com/https://raw.githubusercontent.com/polarisagi/hermes/main/scripts/install.sh | bash
```

**Windows:**
```powershell
# Recommended (Includes smart network fallback)
irm https://raw.githubusercontent.com/polarisagi/hermes/main/scripts/install.ps1 | iex

# Mainland China Fallback 1 (ghfast mirror)
irm https://ghfast.top/https://raw.githubusercontent.com/polarisagi/hermes/main/scripts/install.ps1 | iex

# Mainland China Fallback 2 (ghproxy mirror)
irm https://mirror.ghproxy.com/https://raw.githubusercontent.com/polarisagi/hermes/main/scripts/install.ps1 | iex
```
*The gateway will run as a background service and auto-start on user logon.*

---

### 🛠️ Getting Started

By default, the gateway listens on `127.0.0.1:27777`.

1. **Admin Panel**: Visit [http://127.0.0.1:27777](http://127.0.0.1:27777)
2. **Add Channel**: Select Protocol → Choose Provider → Enter API Key → Save.
3. **One-Click Client Config**: Navigate to "Client Config", pick your target software, select your injected API key, and you're done!
4. **API Endpoints** (Point your AI tools to these URLs, API Key can be any dummy text):
   - OpenAI Protocol: `http://127.0.0.1:27777/v1/openai/`
   - Anthropic Protocol: `http://127.0.0.1:27777/v1/anthropic/`
   - Google Protocol: `http://127.0.0.1:27777/v1/google/`

---

### 🧪 Local Testing Mode 

When testing modified gateway code locally, please use test mode (listens on port `28889`) to avoid conflicts with your running production instance:

```bash
make run-test
# Or manually:
TEST_MODE=true go run ./cmd/hermes
```

---

### 🗑️ Uninstall

**macOS / Linux:**
```bash
# Default
curl -sSL https://raw.githubusercontent.com/polarisagi/hermes/main/scripts/uninstall.sh | bash

# Mainland China Fallback 1 (ghfast mirror)
curl -sSL https://ghfast.top/https://raw.githubusercontent.com/polarisagi/hermes/main/scripts/uninstall.sh | bash

# Mainland China Fallback 2 (ghproxy mirror)
curl -sSL https://mirror.ghproxy.com/https://raw.githubusercontent.com/polarisagi/hermes/main/scripts/uninstall.sh | bash
```
**Windows:**
```powershell
# Default
irm https://raw.githubusercontent.com/polarisagi/hermes/main/scripts/uninstall.ps1 | iex

# Mainland China Fallback 1 (ghfast mirror)
irm https://ghfast.top/https://raw.githubusercontent.com/polarisagi/hermes/main/scripts/uninstall.ps1 | iex

# Mainland China Fallback 2 (ghproxy mirror)
irm https://mirror.ghproxy.com/https://raw.githubusercontent.com/polarisagi/hermes/main/scripts/uninstall.ps1 | iex
```
> **Note**: Uninstalling only removes the service and binary. Data remains safely in `~/.polarisagi/hermes/`. Delete it manually if you want a complete wipe.

---

### 📄 License
GNU Affero General Public License v3.0 (AGPL-3.0). *(If you use this code, please retain the original author credit: `mrlaoliai`)*

---

### 🌐 Links & Contact
* **Official Website**: [https://polarisagi.online/](https://polarisagi.online/)
* **GitHub Repository**: [https://github.com/polarisagi/hermes](https://github.com/polarisagi/hermes)
* **Author / Creator**: `mrlaoliai` (Find me on Xiaohongshu, Douyin, TikTok, and X)
* **Contact Email**: [polarisagi.online@gmail.com](mailto:polarisagi.online@gmail.com)
