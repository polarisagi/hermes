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
    target_user_model_id INTEGER NOT NULL,
    is_active            BOOLEAN DEFAULT 1,
    FOREIGN KEY(target_user_model_id) REFERENCES user_models(id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_ucr_req_target
    ON user_custom_routes(requested_model_id, target_user_model_id);

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
-- SECTION 4: DML — 厂商字典种子数据
-- ============================================================

INSERT OR IGNORE INTO sys_providers (provider_id, provider_name, description, display_order) VALUES
('deepseek',                        'DeepSeek (深度求索)',                    NULL,  1),
('anthropic',                       'Anthropic',                             NULL,  2),
('google',                          'Google AI Studio (原生 Gemini)',         NULL,  3),
('mistral',                         'Mistral AI',                            NULL,  4),
('openai',                          'OpenAI',                                NULL,  5),
('perplexity',                      'Perplexity',                            NULL,  6),
('xai',                             'xAI (Grok)',                            NULL,  7),
('01ai',                            '零一万物 (01.AI)',                       NULL,  8),
('baichuan',                        '百川智能 (Baichuan)',                    NULL,  9),
('dashscope',                       '阿里云 (通义千问)',                      NULL, 10),
('doubao',                          '火山引擎 (字节豆包)',                    NULL, 11),
('hunyuan',                         '腾讯云 (混元)',                         NULL, 12),
('minimax',                         'MiniMax (海螺)',                         NULL, 13),
('moonshot',                        '月之暗面 (Kimi)',                        NULL, 14),
('qianfan',                         '百度智能云 (千帆/文心)',                 NULL, 15),
('sensenova',                       '商汤科技 (日日新)',                      NULL, 16),
('stepfun',                         '阶跃星辰 (StepFun)',                     NULL, 17),
('zhipu',                           '智谱 AI (ChatGLM)',                      NULL, 18),
('azure',                           'Microsoft Azure OpenAI',                NULL, 19),
('azure_speech',                    'Azure Speech',                          NULL, 20),
('bedrock',                         'Amazon AWS Bedrock',                    NULL, 21),
('byteplus',                        'BytePlus',                              NULL, 22),
('gemini_enterprise_agent_platform','Gemini Enterprise Agent Platform',      NULL, 23),
('github_copilot',                  'GitHub Copilot',                        NULL, 24),
('nvidia',                          'NVIDIA NIM',                            NULL, 25),
('opencode',                        'OpenCode',                              NULL, 26),
('ds4',                             'ds4 (local DeepSeek V4)',               NULL, 27),
('inferrs',                         'inferrs (local models)',                NULL, 28),
('lmstudio',                        'LM Studio (local models)',              NULL, 29),
('ollama',                          'Ollama (本地部署)',                      NULL, 30),
('sglang',                          'SGLang (local models)',                 NULL, 31),
('vllm',                            'vLLM (本地部署)',                        NULL, 32),
('custom_anthropic',                '自定义 (Anthropic 兼容)',               NULL, 33),
('custom_google',                   '自定义 (Google 兼容)',                  NULL, 34),
('custom_openai',                   '自定义 (OpenAI 兼容)',                  NULL, 35);

-- ============================================================
-- SECTION 5: DML — 接入端点种子数据
-- ============================================================

INSERT OR IGNORE INTO sys_access_endpoints
    (endpoint_id, provider_id, display_name, api_protocol, default_base_url, auth_type, auth_header, required_credential_fields, display_order)
VALUES
('01ai_bearer',                                  '01ai',                             '零一万物 API Key',                          'openai',     'https://api.lingyiwanwu.com/v1',                                                                          'bearer', 'Authorization',                '["api_key"]',                                   0),
('anthropic_key',                                'anthropic',                        'Anthropic API Key',                         'anthropic',  'https://api.anthropic.com/v1',                                                                            'header', 'x-api-key',                   '["api_key"]',                                   0),
('azure_key',                                    'azure',                            'Azure API Key',                             'openai',     'https://{resource}.openai.azure.com/openai',                                                              'header', 'api-key',                     '["api_key", "resource"]',                       0),
('azure_speech_key',                             'azure_speech',                     'Azure Speech Key',                          'openai',     'https://{region}.tts.speech.microsoft.com',                                                               'header', 'Ocp-Apim-Subscription-Key',   '["api_key", "region"]',                         0),
('baichuan_bearer',                              'baichuan',                         '百川 API Key',                              'openai',     'https://api.baichuan-ai.com/v1',                                                                          'bearer', 'Authorization',                '["api_key"]',                                   0),
('bedrock_aksk',                                 'bedrock',                          'AWS AK/SK',                                 'openai',     'https://bedrock-runtime.{region}.amazonaws.com',                                                          'aws_sigv4', '',                         '["aws_access_key", "aws_secret_key", "region"]', 0),
('byteplus_bearer',                              'byteplus',                         'BytePlus Key',                              'openai',     'https://api.byteplus.com/v1',                                                                             'bearer', 'Authorization',                '["api_key"]',                                   0),
('custom_anthropic_key',                         'custom_anthropic',                 '自定义 Anthropic Key',                      'anthropic',  'https://api.anthropic.com/v1',                                                                            'header', 'x-api-key',                   '["api_key"]',                                   0),
('custom_google_key',                            'custom_google',                    '自定义 Google Key',                         'google',     'https://generativelanguage.googleapis.com/v1beta',                                                        'query',  'key',                         '["api_key"]',                                   0),
('custom_openai_bearer',                         'custom_openai',                    '自定义 OpenAI Key',                         'openai',     'https://api.openai.com/v1',                                                                               'bearer', 'Authorization',                '["api_key"]',                                   0),
('dashscope_bearer',                             'dashscope',                        '阿里云 API Key',                            'openai',     'https://dashscope.aliyuncs.com/compatible-mode/v1',                                                       'bearer', 'Authorization',                '["api_key"]',                                   0),
('deepseek_anthropic_key',                       'deepseek',                         'DeepSeek Anthropic Key',                    'anthropic',  'https://api.deepseek.com/anthropic',                                                                           'header', 'x-api-key',                   '["api_key"]',                                   0),
('deepseek_bearer',                              'deepseek',                         'DeepSeek API Key',                          'openai',     'https://api.deepseek.com',                                                                             'bearer', 'Authorization',                '["api_key"]',                                   0),
('doubao_bearer',                                'doubao',                           '火山引擎 API Key',                          'openai',     'https://ark.cn-beijing.volces.com/api/v3',                                                                 'bearer', 'Authorization',                '["api_key"]',                                   0),
('ds4_none',                                     'ds4',                              'ds4 (无鉴权)',                              'local',      'http://127.0.0.1:8080/v1',                                                                                'none',   '',                            '[]',                                            0),
('gemini_enterprise_agent_platform_adc',         'gemini_enterprise_agent_platform', 'Gemini Enterprise Agent Platform ADC',      'google',     'https://aiplatform.googleapis.com/v1/projects/{project_id}/locations/{region}/publishers/google',                           'adc',    'Authorization',                '["project_id", "region"]',                      0),
('gemini_enterprise_agent_platform_key',         'gemini_enterprise_agent_platform', 'Gemini Enterprise Agent Platform Key',      'google',     'https://aiplatform.googleapis.com/v1/projects/{project_id}/locations/{region}/publishers/google',                           'header', 'Authorization',                '["api_key", "project_id", "region"]',           0),
('gemini_enterprise_agent_platform_anthropic_adc','gemini_enterprise_agent_platform','Gemini Enterprise Agent Platform Anthropic ADC','anthropic','https://aiplatform.googleapis.com/v1/projects/{project_id}/locations/{region}/publishers/anthropic',                          'adc',    'Authorization',                '["project_id", "region"]',                      0),
('gemini_enterprise_agent_platform_anthropic_key','gemini_enterprise_agent_platform','Gemini Enterprise Agent Platform Anthropic Key','anthropic','https://aiplatform.googleapis.com/v1/projects/{project_id}/locations/{region}/publishers/anthropic',                          'header', 'Authorization',                '["api_key", "project_id", "region"]',           0),
('github_copilot_bearer',                        'github_copilot',                   'GitHub Copilot Token',                      'openai',     'https://api.githubcopilot.com',                                                                           'bearer', 'Authorization',                '["api_key"]',                                   0),
('google_key',                                   'google',                           'Google AI Studio Key',                      'google',     'https://generativelanguage.googleapis.com/v1beta',                                                        'query',  'key',                         '["api_key"]',                                   0),
('hunyuan_bearer',                               'hunyuan',                          '腾讯混元 API Key',                          'openai',     'https://api.hunyuan.cloud.tencent.com/v1',                                                                'bearer', 'Authorization',                '["api_key"]',                                   0),
('inferrs_none',                                 'inferrs',                          'inferrs (无鉴权)',                          'local',      'http://127.0.0.1:8000/v1',                                                                                'none',   '',                            '[]',                                            0),
('kimi_coding_bearer',                           'moonshot',                         'Kimi For Coding Key',                       'openai',     'https://api.moonshot.cn/v1',                                                                              'bearer', 'Authorization',                '["api_key"]',                                   0),
('lmstudio_none',                                'lmstudio',                         'LM Studio (无鉴权)',                        'local',      'http://127.0.0.1:1234/v1',                                                                                'none',   '',                            '[]',                                            0),
('minimax_bearer',                               'minimax',                          'MiniMax API Key',                           'openai',     'https://api.minimax.chat/v1',                                                                             'bearer', 'Authorization',                '["api_key"]',                                   0),
('mistral_bearer',                               'mistral',                          'Mistral API Key',                           'openai',     'https://api.mistral.ai/v1',                                                                               'bearer', 'Authorization',                '["api_key"]',                                   0),
('moonshot_bearer',                              'moonshot',                         '月之暗面 API Key',                          'openai',     'https://api.moonshot.cn/v1',                                                                              'bearer', 'Authorization',                '["api_key"]',                                   0),
('nvidia_bearer',                                'nvidia',                           'NVIDIA NIM Key',                            'openai',     'https://integrate.api.nvidia.com/v1',                                                                     'bearer', 'Authorization',                '["api_key"]',                                   0),
('ollama_none',                                  'ollama',                           'Ollama 无鉴权',                             'local',      'http://127.0.0.1:11434/v1',                                                                               'none',   '',                            '[]',                                            0),
('openai_bearer',                                'openai',                           'OpenAI API Key',                            'openai',     'https://api.openai.com/v1',                                                                               'bearer', 'Authorization',                '["api_key"]',                                   0),
('opencode_bearer',                              'opencode',                         'OpenCode Key',                              'openai',     'https://api.opencode.com/v1',                                                                             'bearer', 'Authorization',                '["api_key"]',                                   0),
('opencode_go_bearer',                           'opencode',                         'OpenCode Go Key',                           'openai',     'https://api.opencode.com/go/v1',                                                                          'bearer', 'Authorization',                '["api_key"]',                                   0),
('opencode_zen_bearer',                          'opencode',                         'OpenCode Zen Key',                          'openai',     'https://api.opencode.com/zen/v1',                                                                         'bearer', 'Authorization',                '["api_key"]',                                   0),
('perplexity_bearer',                            'perplexity',                       'Perplexity Key',                            'openai',     'https://api.perplexity.ai',                                                                               'bearer', 'Authorization',                '["api_key"]',                                   0),
('qianfan_bearer',                               'qianfan',                          '百度千帆 API Key',                          'openai',     'https://qianfan.baidubce.com/v2',                                                                         'bearer', 'Authorization',                '["api_key"]',                                   0),
('sensenova_bearer',                             'sensenova',                        '商汤 API Key',                              'openai',     'https://api.sensenova.cn/compatible-mode/v1',                                                             'bearer', 'Authorization',                '["api_key"]',                                   0),
('sglang_none',                                  'sglang',                           'SGLang (无鉴权)',                           'local',      'http://127.0.0.1:30000/v1',                                                                               'none',   '',                            '[]',                                            0),
('stepfun_bearer',                               'stepfun',                          '阶跃星辰 API Key',                          'openai',     'https://api.stepfun.com/v1',                                                                              'bearer', 'Authorization',                '["api_key"]',                                   0),
('vllm_none',                                    'vllm',                             'vLLM 无鉴权',                               'local',      'http://127.0.0.1:8000/v1',                                                                                'none',   '',                            '[]',                                            0),
('xai_bearer',                                   'xai',                              'xAI API Key',                               'openai',     'https://api.x.ai/v1',                                                                                    'bearer', 'Authorization',                '["api_key"]',                                   0),
('zhipu_bearer',                                 'zhipu',                            '智谱 API Key',                              'openai',     'https://open.bigmodel.cn/api/paas/v4',                                                                    'bearer', 'Authorization',                '["api_key"]',                                   0);

-- ============================================================
-- SECTION 6: DML — 全局模型字典种子数据
-- 覆盖 2025~2026 年主流旗舰和极速模型，含 Gemini 2.x / GPT-4o / o 系列
-- ============================================================

INSERT OR IGNORE INTO sys_models
    (model_id, display_name, capability_tier, context_length, max_output_tokens,
     supports_vision, supports_audio_input, supports_audio_output, supports_tools,
     prompt_price_per_1k, completion_price_per_1k, released_at, is_active, version_weight, is_legacy)
VALUES
-- ── Anthropic Claude 4.x ────────────────────────────────────
('claude-opus-4-8',              'Claude Opus 4.8',              'smart', 200000,  32000, 1,0,0,1, 0.005,0.025, 0,          1, 0, 0),
('claude-opus-4-7',              'Claude Opus 4.7',              'smart', 200000,  32000, 1,0,0,1, 0.005,0.025, 0,          1, 0, 0),
('claude-opus-4-6',              'Claude Opus 4.6',              'smart', 200000,  32000, 1,0,0,1, 0.005,0.025, 0,          1, 0, 0),
('claude-opus-4-7-fast',         'Claude Opus 4.7 Fast',         'fast',  200000,  32000, 1,0,0,1, 0.03,0.15, 0,          1, 0, 0),
('claude-opus-4-6-fast',         'Claude Opus 4.6 Fast',         'fast',  200000,  32000, 1,0,0,1, 0.03,0.15, 0,          1, 0, 0),
('claude-sonnet-4-6',            'Claude Sonnet 4.6',            'smart', 200000,  64000, 1,0,0,1, 0.003,0.015, 0,          1, 0, 0),
('claude-haiku-4-5',             'Claude Haiku 4.5',             'fast',  200000,  32000, 1,0,0,1, 0.001,0.005, 0,          1, 0, 0),
-- ── Google Gemini 3.x (2026 旗舰预览) ───────────────────────
('gemini-3.5-flash',             'Gemini 3.5 Flash',             'fast',  1048576, 32768, 1,0,0,1, 0.0015,0.009, 1779193800, 1, 0, 0),
('gemini-3.1-pro',               'Gemini 3.1 Pro',               'smart', 2097152, 65536, 1,0,0,1, 0.002,0.012, 1772045923, 1, 0, 0),
('gemini-3.1-pro-preview',       'Gemini 3.1 Pro Preview',       'smart', 2097152, 65536, 1,0,0,1, 0.002,0.012, 1772045923, 1, 0, 0),
('gemini-3.1-pro-preview-customtools','Gemini 3.1 Pro Preview Customtools','smart',2097152,65536,1,0,0,1,0.002,0.012,1772045923,1,0,0),
('gemini-3.1-flash',             'Gemini 3.1 Flash',             'fast',  1048576, 32768, 1,0,0,1, 0.0005,0.002, 1778168828, 1, 0, 0),
('gemini-3.1-flash-lite',        'Gemini 3.1 Flash Lite',        'fast',  1048576, 32768, 1,0,0,1, 0.00025,0.0015, 1778168828, 1, 0, 0),
('gemini-3.1-flash-image-preview','Gemini 3.1 Flash Image Preview','fast', 1048576, 32768, 1,0,0,1, 0.0005,0.002, 1772119558, 1, 0, 0),
('gemini-3.1-flash-lite-preview','Gemini 3.1 Flash Lite Preview', 'fast', 1048576, 32768, 1,0,0,1, 0.00025,0.0015, 1772512673, 1, 0, 0),
-- ── Google Gemini 2.5 (2025 实际在用) ───────────────────────
('gemini-2.5-pro',               'Gemini 2.5 Pro',               'smart', 2097152, 65536, 1,0,0,1, 0.00125,0.01, 1748995200, 1, 0, 0),
('gemini-2.5-pro-preview',       'Gemini 2.5 Pro Preview',       'smart', 2097152, 65536, 1,0,0,1, 0.00125,0.01, 1748995200, 1, 0, 0),
('gemini-2.5-pro-preview-05-06', 'Gemini 2.5 Pro Preview 05-06', 'smart', 2097152, 65536, 1,0,0,1, 0.00125,0.01, 1746489600, 1, 0, 0),
('gemini-2.5-pro-preview-06-05', 'Gemini 2.5 Pro Preview 06-05', 'smart', 2097152, 65536, 1,0,0,1, 0.00125,0.01, 1749168000, 1, 0, 0),
('gemini-2.5-flash',             'Gemini 2.5 Flash',             'fast',  1048576, 32768, 1,0,0,1, 0.0003,0.001, 1748995200, 1, 0, 0),
('gemini-2.5-flash-preview',     'Gemini 2.5 Flash Preview',     'fast',  1048576, 32768, 1,0,0,1, 0.0003,0.001, 1748995200, 1, 0, 0),
('gemini-2.5-flash-preview-04-17','Gemini 2.5 Flash Preview 04-17','fast',1048576, 32768, 1,0,0,1, 0.0003,0.001, 1744848000, 1, 0, 0),
-- ── Google Gemini 2.0 ────────────────────────────────────────
('gemini-2.0-flash',             'Gemini 2.0 Flash',             'fast',  1048576, 32768, 1,0,0,1, 0.0001,0.0004, 1735689600, 1, 0, 0),
('gemini-2.0-flash-lite',        'Gemini 2.0 Flash Lite',        'fast',  1048576, 32768, 1,0,0,1, 0.000075,0.0003, 1735689600, 1, 0, 0),
('gemini-2.0-flash-exp',         'Gemini 2.0 Flash Exp',         'fast',  1048576, 32768, 1,0,0,1, 0,0, 1735689600, 1, 0, 0),
-- ── OpenAI GPT-5.x ──────────────────────────────────────────
('gpt-5.5-pro',                  'GPT-5.5 Pro',                  'smart', 128000,  16384, 1,0,0,1, 0.03,0.18, 1777051896, 1, 0, 0),
('gpt-5.5',                      'GPT-5.5',                      'smart', 128000,  16384, 1,0,0,1, 0.005,0.03, 1777051896, 1, 0, 0),
('gpt-5.4',                      'GPT-5.4',                      'smart', 128000,  16384, 1,0,0,1, 0.0025,0.015, 1776797528, 1, 0, 0),
('gpt-5.4-mini',                 'GPT-5.4 Mini',                 'fast',  128000,  16384, 1,0,0,1, 0.00075,0.0045, 1773748178, 1, 0, 0),
('gpt-5.3-codex',                'GPT-5.3 Codex',                'smart', 128000,  16384, 1,0,0,1, 0.00175,0.014, 1771959164, 1, 0, 0),
('gpt-5.2',                      'GPT-5.2',                      'smart', 128000,  16384, 1,0,0,1, 0.00175,0.014, 1768409315, 1, 0, 0),
-- ── OpenAI GPT-4o / GPT-4 Turbo (Codex 常用) ────────────────
('gpt-4o',                       'GPT-4o',                       'smart', 128000,  16384, 1,0,0,1, 0.0025,0.01, 1716854400, 1, 0, 0),
('gpt-4o-mini',                  'GPT-4o Mini',                  'fast',  128000,  16384, 1,0,0,1, 0.00015,0.0006, 1721260800, 1, 0, 0),
('gpt-4-turbo',                  'GPT-4 Turbo',                  'smart', 128000,   4096, 1,0,0,1, 0.01,0.03, 1712707200, 1, 0, 0),
('gpt-4-turbo-preview',          'GPT-4 Turbo Preview',          'smart', 128000,   4096, 1,0,0,1, 0.01,0.03, 1704326400, 1, 0, 0),
-- ── OpenAI o 系列 (Codex 思考模式) ──────────────────────────
('o1',                           'o1',                           'smart', 200000, 100000, 1,0,0,1, 0.015,0.06, 1729036800, 1, 0, 0),
('o1-mini',                      'o1 Mini',                      'fast',  128000,  65536, 1,0,0,1, 0.0011,0.0044, 1726531200, 1, 0, 0),
('o3',                           'o3',                           'smart', 200000, 100000, 1,0,0,1, 0.01,0.04, 1751500800, 1, 0, 0),
('o3-mini',                      'o3 Mini',                      'fast',  128000,  65536, 1,0,0,1, 0.0011,0.0044, 1738195200, 1, 0, 0),
('o4-mini',                      'o4 Mini',                      'fast',  200000, 100000, 1,0,0,1, 0.0011,0.0044, 1745366400, 1, 0, 0),
-- ── DeepSeek ────────────────────────────────────────────────
('deepseek-v4-pro',              'DeepSeek V4 Pro',              'smart',  128000,  8192, 1,0,0,1, 0.000435,0.00087, 1777000679, 1, 0, 0),
('deepseek-v4-flash',            'DeepSeek V4 Flash',            'fast',   128000,  8192, 1,0,0,1, 0.00014,0.00028, 1777000666, 1, 0, 0),
('ds4',                          'ds4 (local DeepSeek V4)',      'smart',  128000,  8192, 1,0,0,1, 0,0, 0,          1, 0, 0),
-- ── xAI Grok ────────────────────────────────────────────────
('grok-4.20',                    'Grok 4.20',                    'smart', 200000,  32000, 1,0,0,1, 0.003,0.015, 1774979158, 1, 0, 0),
('grok-4.20-multi-agent',        'Grok 4.20 Multi-Agent',        'smart', 200000,  32000, 1,0,0,1, 0.003,0.015, 1774979158, 1, 0, 0),
('grok-4.3',                     'Grok 4.3',                     'fast',  128000,   8192, 1,0,0,1, 0.00125,0.0025, 1777591821, 1, 0, 0),
-- ── Qwen (阿里通义) ──────────────────────────────────────────
('qwen-3-max',                   'Qwen 3 Max',                   'smart', 1000000, 16384, 1,0,0,1, 0.00034247,0.00136986, 0,          1, 0, 0),
('qwen3-max-thinking',           'Qwen 3 Max Thinking',          'smart', 1000000, 32768, 1,0,0,1, 0.00034247,0.00136986, 1770671901, 1, 0, 0),
('qwen3.7-max',                  'Qwen 3.7 Max',                 'smart', 1000000, 16384, 1,0,0,1, 0.00164384,0.00493151, 1779376861, 1, 0, 0),
('qwen3.6-flash',                'Qwen 3.6 Flash',               'fast',   131072,  8192, 1,0,0,1, 0.00016438,0.0009863, 1777261362, 1, 0, 0),
('qwen3-coder-plus',             'Qwen3 Coder Plus',             'smart',  131072,  8192, 1,0,0,1, 0.00054795,0.00219178, 1758662707, 1, 0, 0),
('qwen3-coder-next',             'Qwen3 Coder Next',             'smart',  131072,  8192, 1,0,0,1, 0.00054795,0.00219178, 1770164101, 1, 0, 0),
('qwen3-next-80b-a3b-thinking',  'Qwen3 Next 80B Thinking',      'smart',  131072,  8192, 1,0,0,1, 0.00006849,0.00027397, 1757612284, 1, 0, 0),
-- ── Kimi (月之暗面) ──────────────────────────────────────────
('kimi-k2.6-coding',             'Kimi K2.6 Coding',             'smart', 1000000, 16384, 1,0,0,1, 0.00089041,0.00369863, 0,          1, 0, 0),
('kimi-k2.6',                    'Kimi K2.6',                    'smart', 1000000, 16384, 1,0,0,1, 0.00089041,0.00369863, 1776699402, 1, 0, 0),
('kimi-k2-thinking',             'Kimi K2 Thinking',             'smart', 1000000, 32768, 1,0,0,1, 0.0006,0.0025, 1762440622, 1, 0, 0),
-- ── Doubao (字节豆包) ────────────────────────────────────────
('doubao-1.5-pro',               'Doubao 1.5 Pro',               'smart',  128000,  8192, 1,0,0,1, 0.00010959,0.00027397, 0,          1, 0, 0),
('doubao-1.5-lite',              'Doubao 1.5 Lite',              'fast',   128000,  8192, 1,0,0,1, 0.0000411,0.00008219, 0,          1, 0, 0),
-- ── GLM (智谱) ───────────────────────────────────────────────
('glm-5.1',                      'GLM 5.1',                      'smart',  128000,  8192, 1,0,0,1, 0.00082192,0.00328767, 1775578025, 1, 0, 0),
('glm-5-turbo',                  'GLM 5 Turbo',                  'fast',   128000,  8192, 1,0,0,1, 0.00068493,0.0030137, 1773583573, 1, 0, 0),
-- ── Mistral ─────────────────────────────────────────────────
('mistral-large-2512',           'Mistral Large 2512',           'smart',  200000, 32000, 1,0,0,1, 0.0005,0.0015, 1764624472, 1, 0, 0),
('mistral-small-2603',           'Mistral Small 2603',           'fast',   128000,  8192, 1,0,0,1, 0.0001,0.0003, 1773695685, 1, 0, 0),
('codestral-2508',               'Codestral 2508',               'smart',  200000, 32000, 1,0,0,1, 0.0003,0.0009, 1754079630, 1, 0, 0),
-- ── Perplexity Sonar ────────────────────────────────────────
('sonar-pro',                    'Sonar Pro',                    'smart',  200000, 32000, 1,0,0,1, 0.003,0.015, 1761854366, 1, 0, 0),
('sonar-reasoning-pro',          'Sonar Reasoning Pro',          'smart',  200000, 32000, 1,0,0,1, 0.002,0.008, 1741313308, 1, 0, 0),
('sonar-deep-research',          'Sonar Deep Research',          'smart',  200000, 32000, 1,0,0,1, 0.002,0.008, 1741311246, 1, 0, 0),
-- ── ERNIE (百度) ─────────────────────────────────────────────
('ernie-4.5-300b-a47b',          'ERNIE 4.5 300B',               'smart',  200000, 32000, 1,0,0,1, 0.00028,0.0011, 1751300139, 1, 0, 0),
('ernie-4.5-21b-a3b-thinking',   'ERNIE 4.5 21B Thinking',       'smart',  200000, 32000, 1,0,0,1, 0.0001,0.0004, 1760048887, 1, 0, 0),
('ernie-4.5-21b-a3b',            'ERNIE 4.5 21B',                'fast',   128000,  8192, 1,0,0,1, 0.0001,0.0004, 1760048887, 1, 0, 0),
-- ── MiniMax ─────────────────────────────────────────────────
('minimax-m2.7',                 'MiniMax M2.7',                 'smart',  200000, 32000, 1,0,0,1, 0.00026,0.0012, 1773836697, 1, 0, 0),
('minimax-m2.5',                 'MiniMax M2.5',                 'fast',   128000,  8192, 1,0,0,1, 0.00028767,0.00115068, 1770908502, 1, 0, 0),
-- ── NVIDIA Nemotron ─────────────────────────────────────────
('nemotron-3-super-120b-a12b',   'Nemotron 3 Super 120B',        'smart',  200000, 32000, 1,0,0,1, 0.001,0.004, 1773245239, 1, 0, 0),
('nemotron-3-nano-30b-a3b',      'Nemotron 3 Nano 30B',          'fast',   128000,  8192, 1,0,0,1, 0.0002,0.0006, 1765731275, 1, 0, 0),
-- ── Amazon Nova (Bedrock) ───────────────────────────────────
('nova-pro-v1',                  'Nova Pro V1',                  'smart',  200000, 32000, 1,0,0,1, 0.0008,0.0032, 1733436303, 1, 0, 0),
('nova-lite-v1',                 'Nova Lite V1',                 'fast',   128000,  8192, 1,0,0,1, 0.00006,0.00024, 1733437363, 1, 0, 0),
-- ── StepFun ─────────────────────────────────────────────────
('step-3.5-flash',               'Step 3.5 Flash',               'fast',   128000,  8192, 1,0,0,1, 0.00009589,0.00028767, 1769728337, 1, 0, 0),
-- ── Hunyuan (腾讯混元) ───────────────────────────────────────
('hunyuan-a13b-instruct',        'Hunyuan A13B Instruct',        'smart',  200000, 32000, 1,0,0,1, 0.00013699,0.00054795, 1751987664, 1, 0, 0);

-- ============================================================
-- SECTION 7: DML — 厂商模型映射种子数据
-- ============================================================

INSERT OR IGNORE INTO sys_provider_models (provider_id, model_id, actual_model_id) VALUES
-- ── Anthropic ───────────────────────────────────────────────
('anthropic',     'claude-opus-4-8',      'claude-opus-4-8'),
('anthropic',     'claude-opus-4-7',      'claude-opus-4-7'),
('anthropic',     'claude-opus-4-6',      'claude-opus-4-6'),
('anthropic',     'claude-opus-4-7-fast', 'claude-opus-4-7-fast'),
('anthropic',     'claude-opus-4-6-fast', 'claude-opus-4-6-fast'),
('anthropic',     'claude-sonnet-4-6',    'claude-sonnet-4-6'),
('anthropic',     'claude-haiku-4-5',     'claude-haiku-4-5'),
('custom_anthropic','claude-opus-4-8',    'claude-opus-4-8'),
('custom_anthropic','claude-opus-4-7',    'claude-opus-4-7'),
('custom_anthropic','claude-opus-4-6',    'claude-opus-4-6'),
('custom_anthropic','claude-opus-4-7-fast','claude-opus-4-7-fast'),
('custom_anthropic','claude-opus-4-6-fast','claude-opus-4-6-fast'),
('custom_anthropic','claude-sonnet-4-6',  'claude-sonnet-4-6'),
('custom_anthropic','claude-haiku-4-5',   'claude-haiku-4-5'),
('opencode',      'claude-opus-4-8',      'claude-opus-4-8'),
('opencode',      'claude-opus-4-7',      'claude-opus-4-7'),
('opencode',      'claude-opus-4-6',      'claude-opus-4-6'),
('opencode',      'claude-opus-4-7-fast', 'claude-opus-4-7-fast'),
('opencode',      'claude-opus-4-6-fast', 'claude-opus-4-6-fast'),
('opencode',      'claude-sonnet-4-6',    'claude-sonnet-4-6'),
('opencode',      'claude-haiku-4-5',     'claude-haiku-4-5'),
-- ── OpenAI GPT-5.x ──────────────────────────────────────────
('openai',        'gpt-5.5-pro',    'gpt-5.5-pro'),
('openai',        'gpt-5.5',        'gpt-5.5'),
('openai',        'gpt-5.4',        'gpt-5.4'),
('openai',        'gpt-5.4-mini',   'gpt-5.4-mini'),
('openai',        'gpt-5.3-codex',  'gpt-5.3-codex'),
('openai',        'gpt-5.2',        'gpt-5.2'),
('custom_openai', 'gpt-5.5-pro',    'gpt-5.5-pro'),
('custom_openai', 'gpt-5.5',        'gpt-5.5'),
('custom_openai', 'gpt-5.4',        'gpt-5.4'),
('custom_openai', 'gpt-5.4-mini',   'gpt-5.4-mini'),
('custom_openai', 'gpt-5.3-codex',  'gpt-5.3-codex'),
('custom_openai', 'gpt-5.2',        'gpt-5.2'),
('github_copilot','gpt-5.5-pro',    'gpt-5.5-pro'),
('github_copilot','gpt-5.5',        'gpt-5.5'),
('github_copilot','gpt-5.4',        'gpt-5.4'),
('github_copilot','gpt-5.4-mini',   'gpt-5.4-mini'),
('github_copilot','gpt-5.3-codex',  'gpt-5.3-codex'),
('github_copilot','gpt-5.2',        'gpt-5.2'),
('azure',         'gpt-5.5-pro',    'gpt-5.5-pro'),
('azure',         'gpt-5.5',        'gpt-5.5'),
('azure',         'gpt-5.4',        'gpt-5.4'),
('azure',         'gpt-5.4-mini',   'gpt-5.4-mini'),
('azure',         'gpt-5.3-codex',  'gpt-5.3-codex'),
('azure',         'gpt-5.2',        'gpt-5.2'),
-- ── OpenAI GPT-4o / Turbo ───────────────────────────────────
('openai',        'gpt-4o',              'gpt-4o'),
('openai',        'gpt-4o-mini',         'gpt-4o-mini'),
('openai',        'gpt-4-turbo',         'gpt-4-turbo'),
('openai',        'gpt-4-turbo-preview', 'gpt-4-turbo-preview'),
('custom_openai', 'gpt-4o',              'gpt-4o'),
('custom_openai', 'gpt-4o-mini',         'gpt-4o-mini'),
('custom_openai', 'gpt-4-turbo',         'gpt-4-turbo'),
('custom_openai', 'gpt-4-turbo-preview', 'gpt-4-turbo-preview'),
('github_copilot','gpt-4o',              'gpt-4o'),
('github_copilot','gpt-4o-mini',         'gpt-4o-mini'),
('azure',         'gpt-4o',              'gpt-4o'),
('azure',         'gpt-4o-mini',         'gpt-4o-mini'),
-- ── OpenAI o 系列 ────────────────────────────────────────────
('openai',        'o1',      'o1'),
('openai',        'o1-mini', 'o1-mini'),
('openai',        'o3',      'o3'),
('openai',        'o3-mini', 'o3-mini'),
('openai',        'o4-mini', 'o4-mini'),
('custom_openai', 'o1',      'o1'),
('custom_openai', 'o1-mini', 'o1-mini'),
('custom_openai', 'o3',      'o3'),
('custom_openai', 'o3-mini', 'o3-mini'),
('custom_openai', 'o4-mini', 'o4-mini'),
-- ── Google Gemini 3.x ───────────────────────────────────────
('google',                          'gemini-3.5-flash',                  'gemini-3.5-flash'),
('google',                          'gemini-3.1-pro',                    'gemini-3.1-pro'),
('google',                          'gemini-3.1-pro-preview',            'gemini-3.1-pro-preview'),
('google',                          'gemini-3.1-pro-preview-customtools','gemini-3.1-pro-preview-customtools'),
('google',                          'gemini-3.1-flash',                  'gemini-3.1-flash'),
('google',                          'gemini-3.1-flash-lite',             'gemini-3.1-flash-lite'),
('google',                          'gemini-3.1-flash-image-preview',    'gemini-3.1-flash-image-preview'),
('google',                          'gemini-3.1-flash-lite-preview',     'gemini-3.1-flash-lite-preview'),
('custom_google',                   'gemini-3.5-flash',                  'gemini-3.5-flash'),
('custom_google',                   'gemini-3.1-pro',                    'gemini-3.1-pro'),
('custom_google',                   'gemini-3.1-pro-preview',            'gemini-3.1-pro-preview'),
('custom_google',                   'gemini-3.1-pro-preview-customtools','gemini-3.1-pro-preview-customtools'),
('custom_google',                   'gemini-3.1-flash',                  'gemini-3.1-flash'),
('custom_google',                   'gemini-3.1-flash-lite',             'gemini-3.1-flash-lite'),
('custom_google',                   'gemini-3.1-flash-image-preview',    'gemini-3.1-flash-image-preview'),
('custom_google',                   'gemini-3.1-flash-lite-preview',     'gemini-3.1-flash-lite-preview'),
('gemini_enterprise_agent_platform','gemini-3.5-flash',                  'gemini-3.5-flash'),
('gemini_enterprise_agent_platform','gemini-3.1-pro',                    'gemini-3.1-pro'),
('gemini_enterprise_agent_platform','gemini-3.1-pro-preview',            'gemini-3.1-pro-preview'),
('gemini_enterprise_agent_platform','gemini-3.1-pro-preview-customtools','gemini-3.1-pro-preview-customtools'),
('gemini_enterprise_agent_platform','gemini-3.1-flash',                  'gemini-3.1-flash'),
('gemini_enterprise_agent_platform','gemini-3.1-flash-lite',             'gemini-3.1-flash-lite'),
('gemini_enterprise_agent_platform','gemini-3.1-flash-image-preview',    'gemini-3.1-flash-image-preview'),
('gemini_enterprise_agent_platform','gemini-3.1-flash-lite-preview',     'gemini-3.1-flash-lite-preview'),
('opencode',                        'gemini-3.1-pro',                    'gemini-3.1-pro'),
('opencode',                        'gemini-3.1-pro-preview',            'gemini-3.1-pro-preview'),
('opencode',                        'gemini-3.1-pro-preview-customtools','gemini-3.1-pro-preview-customtools'),
('opencode',                        'gemini-3.1-flash',                  'gemini-3.1-flash'),
('opencode',                        'gemini-3.1-flash-lite',             'gemini-3.1-flash-lite'),
('opencode',                        'gemini-3.5-flash',                  'gemini-3.5-flash'),
-- ── Google Gemini 2.5 ───────────────────────────────────────
('google',                          'gemini-2.5-pro',                'gemini-2.5-pro'),
('google',                          'gemini-2.5-pro-preview',        'gemini-2.5-pro-preview'),
('google',                          'gemini-2.5-pro-preview-05-06',  'gemini-2.5-pro-preview-05-06'),
('google',                          'gemini-2.5-pro-preview-06-05',  'gemini-2.5-pro-preview-06-05'),
('google',                          'gemini-2.5-flash',              'gemini-2.5-flash'),
('google',                          'gemini-2.5-flash-preview',      'gemini-2.5-flash-preview'),
('google',                          'gemini-2.5-flash-preview-04-17','gemini-2.5-flash-preview-04-17'),
('custom_google',                   'gemini-2.5-pro',                'gemini-2.5-pro'),
('custom_google',                   'gemini-2.5-pro-preview',        'gemini-2.5-pro-preview'),
('custom_google',                   'gemini-2.5-pro-preview-05-06',  'gemini-2.5-pro-preview-05-06'),
('custom_google',                   'gemini-2.5-pro-preview-06-05',  'gemini-2.5-pro-preview-06-05'),
('custom_google',                   'gemini-2.5-flash',              'gemini-2.5-flash'),
('custom_google',                   'gemini-2.5-flash-preview',      'gemini-2.5-flash-preview'),
('custom_google',                   'gemini-2.5-flash-preview-04-17','gemini-2.5-flash-preview-04-17'),
('gemini_enterprise_agent_platform','gemini-2.5-pro',                'gemini-2.5-pro'),
('gemini_enterprise_agent_platform','gemini-2.5-pro-preview',        'gemini-2.5-pro-preview'),
('gemini_enterprise_agent_platform','gemini-2.5-pro-preview-05-06',  'gemini-2.5-pro-preview-05-06'),
('gemini_enterprise_agent_platform','gemini-2.5-pro-preview-06-05',  'gemini-2.5-pro-preview-06-05'),
('gemini_enterprise_agent_platform','gemini-2.5-flash',              'gemini-2.5-flash'),
('gemini_enterprise_agent_platform','gemini-2.5-flash-preview',      'gemini-2.5-flash-preview'),
('gemini_enterprise_agent_platform','gemini-2.5-flash-preview-04-17','gemini-2.5-flash-preview-04-17'),
('opencode',                        'gemini-2.5-pro',                'gemini-2.5-pro'),
('opencode',                        'gemini-2.5-flash',              'gemini-2.5-flash'),
-- ── Google Gemini 2.0 ───────────────────────────────────────
('google',                          'gemini-2.0-flash',     'gemini-2.0-flash'),
('google',                          'gemini-2.0-flash-lite','gemini-2.0-flash-lite'),
('google',                          'gemini-2.0-flash-exp', 'gemini-2.0-flash-exp'),
('custom_google',                   'gemini-2.0-flash',     'gemini-2.0-flash'),
('custom_google',                   'gemini-2.0-flash-lite','gemini-2.0-flash-lite'),
('custom_google',                   'gemini-2.0-flash-exp', 'gemini-2.0-flash-exp'),
('gemini_enterprise_agent_platform','gemini-2.0-flash',     'gemini-2.0-flash'),
('gemini_enterprise_agent_platform','gemini-2.0-flash-lite','gemini-2.0-flash-lite'),
('gemini_enterprise_agent_platform','gemini-2.0-flash-exp', 'gemini-2.0-flash-exp'),
('opencode',                        'gemini-2.0-flash',     'gemini-2.0-flash'),
-- ── DeepSeek ────────────────────────────────────────────────
('deepseek',      'deepseek-v4-pro',    'deepseek-v4-pro'),
('deepseek',      'deepseek-v4-flash',  'deepseek-v4-flash'),
('deepseek',      'deepseek-v4',        'deepseek-v4'),
('deepseek',      'deepseek-chat',      'deepseek-chat'),
('deepseek',      'deepseek-reasoner',  'deepseek-reasoner'),
('custom_openai', 'deepseek-v4-pro',    'deepseek-v4-pro'),
('custom_openai', 'deepseek-v4-flash',  'deepseek-v4-flash'),
('custom_openai', 'deepseek-v4',        'deepseek-v4'),
('custom_openai', 'deepseek-chat',      'deepseek-chat'),
('custom_openai', 'deepseek-reasoner',  'deepseek-reasoner'),
('ds4',           'ds4',                'ds4'),
-- ── xAI Grok ────────────────────────────────────────────────
('xai',           'grok-4.20',               'grok-4.20'),
('xai',           'grok-4.20-multi-agent',   'grok-4.20-multi-agent'),
('xai',           'grok-4.3',                'grok-4.3'),
('custom_openai', 'grok-4.20',               'grok-4.20'),
('custom_openai', 'grok-4.20-multi-agent',   'grok-4.20-multi-agent'),
('custom_openai', 'grok-4.3',                'grok-4.3'),
-- ── Qwen ────────────────────────────────────────────────────
('dashscope',     'qwen-3-max',              'qwen-3-max'),
('dashscope',     'qwen3-max-thinking',      'qwen3-max-thinking'),
('dashscope',     'qwen3.7-max',             'qwen3.7-max'),
('dashscope',     'qwen3.6-flash',           'qwen3.6-flash'),
('dashscope',     'qwen3-coder-plus',        'qwen3-coder-plus'),
('dashscope',     'qwen3-coder-next',        'qwen3-coder-next'),
('dashscope',     'qwen3-next-80b-a3b-thinking','qwen3-next-80b-a3b-thinking'),
('custom_openai', 'qwen-3-max',              'qwen-3-max'),
('custom_openai', 'qwen3-max-thinking',      'qwen3-max-thinking'),
('custom_openai', 'qwen3.7-max',             'qwen3.7-max'),
('custom_openai', 'qwen3.6-flash',           'qwen3.6-flash'),
('custom_openai', 'qwen3-coder-plus',        'qwen3-coder-plus'),
('custom_openai', 'qwen3-coder-next',        'qwen3-coder-next'),
('custom_openai', 'qwen3-next-80b-a3b-thinking','qwen3-next-80b-a3b-thinking'),
-- ── Kimi ────────────────────────────────────────────────────
('moonshot',      'kimi-k2.6-coding',  'kimi-k2.6-coding'),
('moonshot',      'kimi-k2.6',         'kimi-k2.6'),
('moonshot',      'kimi-k2-thinking',  'kimi-k2-thinking'),
('custom_openai', 'kimi-k2.6-coding',  'kimi-k2.6-coding'),
('custom_openai', 'kimi-k2.6',         'kimi-k2.6'),
('custom_openai', 'kimi-k2-thinking',  'kimi-k2-thinking'),
-- ── Doubao ──────────────────────────────────────────────────
('doubao',        'doubao-1.5-pro',    'doubao-1.5-pro'),
('doubao',        'doubao-1.5-lite',   'doubao-1.5-lite'),
('custom_openai', 'doubao-1.5-pro',    'doubao-1.5-pro'),
('custom_openai', 'doubao-1.5-lite',   'doubao-1.5-lite'),
-- ── GLM ─────────────────────────────────────────────────────
('zhipu',         'glm-5.1',           'glm-5.1'),
('zhipu',         'glm-5-turbo',       'glm-5-turbo'),
('custom_openai', 'glm-5.1',           'glm-5.1'),
('custom_openai', 'glm-5-turbo',       'glm-5-turbo'),
-- ── Mistral ─────────────────────────────────────────────────
('mistral',       'mistral-large-2512','mistral-large-2512'),
('mistral',       'mistral-small-2603','mistral-small-2603'),
('mistral',       'codestral-2508',    'codestral-2508'),
('custom_openai', 'mistral-large-2512','mistral-large-2512'),
('custom_openai', 'mistral-small-2603','mistral-small-2603'),
('custom_openai', 'codestral-2508',    'codestral-2508'),
-- ── Sonar ───────────────────────────────────────────────────
('perplexity',    'sonar-pro',             'sonar-pro'),
('perplexity',    'sonar-reasoning-pro',   'sonar-reasoning-pro'),
('perplexity',    'sonar-deep-research',   'sonar-deep-research'),
('custom_openai', 'sonar-pro',             'sonar-pro'),
('custom_openai', 'sonar-reasoning-pro',   'sonar-reasoning-pro'),
('custom_openai', 'sonar-deep-research',   'sonar-deep-research'),
-- ── ERNIE ───────────────────────────────────────────────────
('qianfan',       'ernie-4.5-300b-a47b',       'ernie-4.5-300b-a47b'),
('qianfan',       'ernie-4.5-21b-a3b-thinking', 'ernie-4.5-21b-a3b-thinking'),
('qianfan',       'ernie-4.5-21b-a3b',          'ernie-4.5-21b-a3b'),
('custom_openai', 'ernie-4.5-300b-a47b',        'ernie-4.5-300b-a47b'),
('custom_openai', 'ernie-4.5-21b-a3b-thinking', 'ernie-4.5-21b-a3b-thinking'),
('custom_openai', 'ernie-4.5-21b-a3b',          'ernie-4.5-21b-a3b'),
-- ── MiniMax ─────────────────────────────────────────────────
('minimax',       'minimax-m2.7',  'minimax-m2.7'),
('minimax',       'minimax-m2.5',  'minimax-m2.5'),
('custom_openai', 'minimax-m2.7',  'minimax-m2.7'),
('custom_openai', 'minimax-m2.5',  'minimax-m2.5'),
-- ── NVIDIA ──────────────────────────────────────────────────
('nvidia',        'nemotron-3-super-120b-a12b',  'nemotron-3-super-120b-a12b'),
('nvidia',        'nemotron-3-nano-30b-a3b',     'nemotron-3-nano-30b-a3b'),
('custom_openai', 'nemotron-3-super-120b-a12b',  'nemotron-3-super-120b-a12b'),
('custom_openai', 'nemotron-3-nano-30b-a3b',     'nemotron-3-nano-30b-a3b'),
-- ── Bedrock ─────────────────────────────────────────────────
('bedrock',       'nova-pro-v1',   'nova-pro-v1'),
('bedrock',       'nova-lite-v1',  'nova-lite-v1'),
-- ── StepFun ─────────────────────────────────────────────────
('stepfun',       'step-3.5-flash','step-3.5-flash'),
('custom_openai', 'step-3.5-flash','step-3.5-flash'),
-- ── Hunyuan ─────────────────────────────────────────────────
('hunyuan',       'hunyuan-a13b-instruct','hunyuan-a13b-instruct'),
('custom_openai', 'hunyuan-a13b-instruct','hunyuan-a13b-instruct');

-- ============================================================
-- SECTION 8: DML — 意图路由字典初始化
-- 从 sys_models 批量生成，再补充带日期快照版本
-- ============================================================

-- 从 sys_models 批量初始化
INSERT OR IGNORE INTO sys_model_intent_dict (model_id, capability_tier, source)
SELECT model_id, capability_tier, 'seed'
FROM sys_models
WHERE capability_tier IS NOT NULL AND capability_tier != '';

-- Claude Code 常发的带日期快照版本（不在 sys_models，单独补充）
INSERT OR IGNORE INTO sys_model_intent_dict (model_id, capability_tier, source) VALUES
('claude-3-5-sonnet-20241022', 'smart', 'seed'),
('claude-3-7-sonnet-20250219', 'smart', 'seed'),
('claude-3-haiku-20240307',    'fast',  'seed'),
('claude-3-5-haiku-20241022',  'fast',  'seed'),
('claude-3-opus-20240229',     'smart', 'seed');
