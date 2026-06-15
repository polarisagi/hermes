-- PolarisAGI Hermes 统一初始化脚本 (2026 架构)
-- 包含完整 DDL + 种子数据，一次性建库。

-- ============================================================
-- SECTION 1: DDL — 系统字典层（只读）
-- ============================================================

-- 厂商字典
CREATE TABLE IF NOT EXISTS sys_providers (
    provider_id   VARCHAR PRIMARY KEY,
    provider_name VARCHAR NOT NULL,
    description   TEXT,
    display_order INTEGER DEFAULT 0
);

-- 接入端点字典（每个厂商可有多个协议端点）
CREATE TABLE IF NOT EXISTS sys_access_endpoints (
    endpoint_id               VARCHAR PRIMARY KEY,
    provider_id               VARCHAR NOT NULL,
    display_name              VARCHAR NOT NULL,
    api_protocol              VARCHAR NOT NULL,
    default_base_url          VARCHAR NOT NULL,
    auth_type                 VARCHAR NOT NULL,
    auth_header               VARCHAR,
    required_credential_fields JSON NOT NULL,
    display_order             INTEGER DEFAULT 0,
    FOREIGN KEY(provider_id) REFERENCES sys_providers(provider_id)
);

-- 全局模型字典
CREATE TABLE IF NOT EXISTS sys_models (
    model_id              TEXT PRIMARY KEY,
    display_name          TEXT NOT NULL,
    capability_tier       TEXT CHECK(capability_tier IN ('smart', 'fast')),
    context_length        INTEGER DEFAULT 0,
    max_output_tokens     INTEGER DEFAULT 0,
    supports_vision       BOOLEAN DEFAULT FALSE,
    supports_audio_input  BOOLEAN DEFAULT FALSE,
    supports_audio_output BOOLEAN DEFAULT FALSE,
    supports_tools        BOOLEAN DEFAULT FALSE,
    prompt_price_per_1k   REAL DEFAULT 0.0,
    completion_price_per_1k REAL DEFAULT 0.0,
    released_at           INTEGER DEFAULT 0,
    is_active             BOOLEAN DEFAULT TRUE,
    version_weight        INTEGER DEFAULT 0,
    is_legacy             BOOLEAN DEFAULT FALSE
);

-- 厂商模型映射（model_id → actual_model_id，如有差异）
CREATE TABLE IF NOT EXISTS sys_provider_models (
    provider_id    TEXT NOT NULL,
    model_id       TEXT NOT NULL,
    actual_model_id TEXT NOT NULL,
    PRIMARY KEY (provider_id, model_id),
    FOREIGN KEY(provider_id) REFERENCES sys_providers(provider_id),
    FOREIGN KEY(model_id)    REFERENCES sys_models(model_id)
);

-- 意图路由字典（model_id → capability_tier，路由热路径专用）
-- tier 体系：smart（旗舰） / fast（极速），2026 年思考能力已统一为请求参数
CREATE TABLE IF NOT EXISTS sys_model_intent_dict (
    model_id        TEXT PRIMARY KEY,
    capability_tier TEXT NOT NULL CHECK(capability_tier IN ('smart', 'fast')),
    source          TEXT NOT NULL DEFAULT 'seed'
);

-- ============================================================
-- SECTION 2: DDL — 用户配置层（动态）
-- ============================================================

-- 用户渠道账号
CREATE TABLE IF NOT EXISTS user_providers (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    name             VARCHAR NOT NULL,
    provider_id      VARCHAR NOT NULL,
    auth_credentials JSON NOT NULL,
    priority         INTEGER DEFAULT 10,
    weight           INTEGER DEFAULT 100,
    concurrency_limit INTEGER DEFAULT 0,
    min_interval_sec  INTEGER DEFAULT 0,
    timeout_sec       INTEGER DEFAULT 120,
    retry_times       INTEGER DEFAULT 3,
    status            INTEGER DEFAULT 1,
    balance           REAL DEFAULT 0,
    limit_percent     REAL DEFAULT 90.0,
    used_amount       REAL DEFAULT 0,
    valid_from        DATETIME,
    valid_to          DATETIME,
    created_at        DATETIME DEFAULT (datetime('now', 'localtime')),
    FOREIGN KEY(provider_id) REFERENCES sys_providers(provider_id)
);


-- 用户渠道账号启用的端点覆盖
CREATE TABLE IF NOT EXISTS user_access_endpoints (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    user_provider_id INTEGER NOT NULL,
    sys_endpoint_id  VARCHAR NOT NULL,
    api_protocol     VARCHAR,
    is_enabled       BOOLEAN DEFAULT 1,
    custom_base_url  VARCHAR DEFAULT '',
    FOREIGN KEY(user_provider_id) REFERENCES user_providers(id) ON DELETE CASCADE,
    FOREIGN KEY(sys_endpoint_id) REFERENCES sys_access_endpoints(endpoint_id)
);


-- 用户为渠道配置的模型实例
CREATE TABLE IF NOT EXISTS user_models (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    user_provider_id INTEGER NOT NULL,
    display_name     VARCHAR,
    model_id         VARCHAR NOT NULL,
    capability_tier  VARCHAR NOT NULL CHECK(capability_tier IN ('smart', 'fast')),
    is_active        BOOLEAN DEFAULT 1,
    FOREIGN KEY(user_provider_id) REFERENCES user_providers(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_models_user_provider_id_model_id ON user_models(user_provider_id, model_id);

-- 用户手动覆盖 + 系统自动学习的意图字典（优先级高于 sys 字典）
CREATE TABLE IF NOT EXISTS user_model_intent_dict (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    model_id        VARCHAR NOT NULL UNIQUE,
    capability_tier VARCHAR NOT NULL CHECK(capability_tier IN ('smart', 'fast')),
    source          VARCHAR DEFAULT 'manual',
    created_at      DATETIME DEFAULT (datetime('now', 'localtime')),
    updated_at      DATETIME DEFAULT (datetime('now', 'localtime'))
);

-- 专业模式强制路由表（1:N，同一请求可路由到多个目标渠道做负载均衡）
CREATE TABLE IF NOT EXISTS user_custom_routes (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    requested_model_id   VARCHAR NOT NULL,
    target_provider_id   VARCHAR NOT NULL,
    target_model_id      VARCHAR NOT NULL,
    is_active            BOOLEAN DEFAULT 1
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_ucr_req_target
    ON user_custom_routes(requested_model_id, target_provider_id, target_model_id);

-- ============================================================
-- SECTION 3: DDL — 运营层（日志 / 计费 / 配置）
-- ============================================================

-- Token 消费账单（异步写入，与路由日志分离）
CREATE TABLE IF NOT EXISTS account_logs (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    account_name     VARCHAR,
    api_protocol     VARCHAR,
    requested_model_id VARCHAR,
    actual_model_id  VARCHAR,
    prompt_tokens    INTEGER,
    completion_tokens INTEGER,
    total_tokens     INTEGER,
    latency_ms       INTEGER,
    status_code      INTEGER,
    error_msg        TEXT,
    cost             REAL DEFAULT 0.0,
    client_name      VARCHAR,
    created_at       DATETIME DEFAULT (datetime('now', 'localtime'))
);

-- 路由决策透明日志（记录推断来源、是否降级，7 天自动清理）
CREATE TABLE IF NOT EXISTS routing_logs (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at       DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
    requested_model  TEXT NOT NULL,
    resolved_tier    TEXT NOT NULL,
    resolution_src   TEXT NOT NULL,   -- custom_route|user_dict|sys_dict|auto_regex|fallback_default
    tier_degraded    INTEGER NOT NULL DEFAULT 0,
    original_tier    TEXT,
    provider_name    TEXT,
    actual_model     TEXT,
    user_provider_id INTEGER,
    client_name      VARCHAR
);
CREATE INDEX IF NOT EXISTS idx_routing_logs_created   ON routing_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_routing_logs_requested ON routing_logs(requested_model);

-- 全局系统配置（熔断参数、UI 模式等）
CREATE TABLE IF NOT EXISTS system_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- 客户端一键配置备份（Claude Code / Codex 等原始配置存档，支持还原）
CREATE TABLE IF NOT EXISTS client_config_backups (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    client_name      TEXT NOT NULL UNIQUE,
    config_path      TEXT NOT NULL,
    original_content TEXT NOT NULL DEFAULT '',
    injected_url     TEXT NOT NULL DEFAULT '',
    backed_up_at     DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
    updated_at       DATETIME NOT NULL DEFAULT (datetime('now', 'localtime'))
);

-- 外部厂商数据暂存（OpenRouter / LiteLLM 拉取，经晋升审查后写入 sys_*）
CREATE TABLE IF NOT EXISTS external_provider_cache (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    provider_id   TEXT NOT NULL,
    source        TEXT NOT NULL,   -- 'openrouter'|'siliconflow'|'manual'
    provider_name TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    website_url   TEXT NOT NULL DEFAULT '',
    api_base_url  TEXT NOT NULL DEFAULT '',
    api_protocol  TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'pending',  -- pending|promoted|rejected
    reject_reason TEXT NOT NULL DEFAULT '',
    synced_at     DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
    promoted_at   DATETIME,
    UNIQUE(provider_id, source)
);
CREATE INDEX IF NOT EXISTS idx_ext_provider_status ON external_provider_cache(status);

-- 外部模型数据暂存
CREATE TABLE IF NOT EXISTS external_model_cache (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    model_id              TEXT NOT NULL,
    source                TEXT NOT NULL,   -- 'openrouter'|'litellm'|'siliconflow'
    display_name          TEXT NOT NULL DEFAULT '',
    capability_tier       TEXT NOT NULL DEFAULT '',
    tier_infer_src        TEXT NOT NULL DEFAULT '',  -- 'source_tag'|'auto_regex'|'fallback_default'
    context_length        INTEGER NOT NULL DEFAULT 0,
    max_output_tokens     INTEGER NOT NULL DEFAULT 0,
    supports_vision       INTEGER NOT NULL DEFAULT 0,
    supports_audio_input  INTEGER NOT NULL DEFAULT 0,
    supports_audio_output INTEGER NOT NULL DEFAULT 0,
    supports_tools        INTEGER NOT NULL DEFAULT 0,
    prompt_price_per_1k   REAL NOT NULL DEFAULT 0,
    completion_price_per_1k REAL NOT NULL DEFAULT 0,
    released_at           INTEGER NOT NULL DEFAULT 0,
    is_legacy             INTEGER NOT NULL DEFAULT 0,
    status                TEXT NOT NULL DEFAULT 'pending',   -- pending|promoted|rejected
    reject_reason         TEXT NOT NULL DEFAULT '',          -- 'pre_2025'|'is_legacy'|'no_tier'
    synced_at             DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
    promoted_at           DATETIME,
    UNIQUE(model_id, source)
);
CREATE INDEX IF NOT EXISTS idx_ext_model_status   ON external_model_cache(status);
CREATE INDEX IF NOT EXISTS idx_ext_model_released ON external_model_cache(released_at);
CREATE INDEX IF NOT EXISTS idx_ext_model_model_id ON external_model_cache(model_id);

-- ============================================================
