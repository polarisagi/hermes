-- SECTION 4: DML — 厂商字典种子数据
-- ============================================================

INSERT INTO sys_providers (provider_id, provider_name, description, display_order) VALUES
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
('agent_platform','Agent Platform（原Vertex AI ）',      NULL, 23),
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
('custom_openai',                   '自定义 (OpenAI 兼容)',                  NULL, 35)
ON CONFLICT(provider_id) DO UPDATE SET provider_name = excluded.provider_name, description = excluded.description, display_order = excluded.display_order;

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
('agent_platform_adc',         'agent_platform', 'Agent Platform（原Vertex AI ） ADC',      'google',     'https://aiplatform.googleapis.com/v1/projects/{project_id}/locations/{region}/publishers/google',                           'adc',    'Authorization',                '["project_id", "region"]',                      0),
('agent_platform_key',         'agent_platform', 'Agent Platform（原Vertex AI ） Key',      'google',     'https://aiplatform.googleapis.com/v1/projects/{project_id}/locations/{region}/publishers/google',                           'query',  'key',                          '["api_key", "project_id", "region"]',           0),
('agent_platform_anthropic_adc','agent_platform','Agent Platform（原Vertex AI ） Anthropic ADC','anthropic','https://aiplatform.googleapis.com/v1/projects/{project_id}/locations/{region}/publishers/anthropic',                          'adc',    'Authorization',                '["project_id", "region"]',                      0),
('agent_platform_anthropic_key','agent_platform','Agent Platform（原Vertex AI ） Anthropic Key','anthropic','https://aiplatform.googleapis.com/v1/projects/{project_id}/locations/{region}/publishers/anthropic',                          'query',  'key',                          '["api_key", "project_id", "region"]',           0),
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
('claude-opus-4-8',              'Claude Opus 4.8',              'smart', 200000,  32000, 1,0,0,1, 0.005,0.025, 1779926400, 1, 1779926400, 0),
('claude-opus-4-7',              'Claude Opus 4.7',              'smart', 200000,  32000, 1,0,0,1, 0.005,0.025, 1776211200, 1, 1776211200, 0),
('claude-opus-4-6',              'Claude Opus 4.6',              'smart', 200000,  32000, 1,0,0,1, 0.005,0.025, 1772323200, 1, 1772323200, 0),
('claude-opus-4-7-fast',         'Claude Opus 4.7 Fast',         'fast',  200000,  32000, 1,0,0,1, 0.03,0.15, 1776211200, 1, 1776211200, 0),
('claude-opus-4-6-fast',         'Claude Opus 4.6 Fast',         'fast',  200000,  32000, 1,0,0,1, 0.03,0.15, 1772323200, 1, 1772323200, 0),
('claude-sonnet-4-6',            'Claude Sonnet 4.6',            'smart', 200000,  64000, 1,0,0,1, 0.003,0.015, 1772668800, 1, 1772668800, 0),
('claude-haiku-4-5',             'Claude Haiku 4.5',             'fast',  200000,  32000, 1,0,0,1, 0.001,0.005, 1770681600, 1, 1770681600, 0),
-- ── Google Gemini 3.x (2026 旗舰预览) ───────────────────────
('gemini-3.5-flash',             'Gemini 3.5 Flash',             'fast',  1048576, 32768, 1,0,0,1, 0.0015,0.009, 1779193800, 1, 1779193800, 0),
('gemini-3.1-pro',               'Gemini 3.1 Pro',               'smart', 2097152, 65536, 1,0,0,1, 0.002,0.012, 1772045923, 1, 1772045923, 0),
('gemini-3.1-pro-preview',       'Gemini 3.1 Pro Preview',       'smart', 2097152, 65536, 1,0,0,1, 0.002,0.012, 1772045923, 1, 1772045923, 0),
('gemini-3.1-pro-preview-customtools','Gemini 3.1 Pro Preview Customtools','smart',2097152,65536,1,0,0,1,0.002,0.012,1772045923,1,1772045923,0),
('gemini-3.1-flash',             'Gemini 3.1 Flash',             'fast',  1048576, 32768, 1,0,0,1, 0.0005,0.002, 1778168828, 1, 1778168828, 0),
('gemini-3.1-flash-lite',        'Gemini 3.1 Flash Lite',        'fast',  1048576, 32768, 1,0,0,1, 0.00025,0.0015, 1778168828, 1, 1778168828, 0),
-- ── Google Gemini 2.0 ────────────────────────────────────────
-- ── OpenAI GPT-5.x ──────────────────────────────────────────
('gpt-5.5-pro',                  'GPT-5.5 Pro',                  'smart', 128000,  16384, 1,0,0,1, 0.03,0.18, 1777051896, 1, 1777051896, 0),
('gpt-5.5',                      'GPT-5.5',                      'smart', 128000,  16384, 1,0,0,1, 0.005,0.03, 1777051896, 1, 1777051896, 0),
('gpt-5.4',                      'GPT-5.4',                      'smart', 128000,  16384, 1,0,0,1, 0.0025,0.015, 1776797528, 1, 1776797528, 0),
('gpt-5.4-mini',                 'GPT-5.4 Mini',                 'fast',  128000,  16384, 1,0,0,1, 0.00075,0.0045, 1773748178, 1, 1773748178, 0),
('gpt-5.3-codex',                'GPT-5.3 Codex',                'smart', 128000,  16384, 1,0,0,1, 0.00175,0.014, 1771959164, 1, 1771959164, 0),
('gpt-5.2',                      'GPT-5.2',                      'smart', 128000,  16384, 1,0,0,1, 0.00175,0.014, 1768409315, 1, 1768409315, 0),
-- ── OpenAI GPT-4o / GPT-4 Turbo (Codex 常用) ────────────────
-- ── OpenAI o 系列 (Codex 思考模式) ──────────────────────────
-- ── DeepSeek ────────────────────────────────────────────────
('deepseek-v4-pro',              'DeepSeek V4 Pro',              'smart',  128000,  8192, 1,0,0,1, 0.000435,0.00087, 1777000679, 1, 1777000679, 0),
('deepseek-v4-flash',            'DeepSeek V4 Flash',            'fast',   128000,  8192, 1,0,0,1, 0.00014,0.00028, 1777000666, 1, 1777000666, 0),
('ds4',                          'ds4 (local DeepSeek V4)',      'smart',  128000,  8192, 1,0,0,1, 0,0, 1767225600, 1, 1767225600, 0),
-- ── xAI Grok ────────────────────────────────────────────────
('grok-4.20',                    'Grok 4.20',                    'smart', 200000,  32000, 1,0,0,1, 0.003,0.015, 1774979158, 1, 1774979158, 0),
('grok-4.20-multi-agent',        'Grok 4.20 Multi-Agent',        'smart', 200000,  32000, 1,0,0,1, 0.003,0.015, 1774979158, 1, 1774979158, 0),
('grok-4.3',                     'Grok 4.3',                     'fast',  128000,   8192, 1,0,0,1, 0.00125,0.0025, 1777591821, 1, 1777591821, 0),
-- ── Qwen (阿里通义) ──────────────────────────────────────────
('qwen-3-max',                   'Qwen 3 Max',                   'smart', 1000000, 16384, 1,0,0,1, 0.00034247,0.00136986, 1776643200, 1, 1776643200, 0),
('qwen3-max-thinking',           'Qwen 3 Max Thinking',          'smart', 1000000, 32768, 1,0,0,1, 0.00034247,0.00136986, 1770671901, 1, 1770671901, 0),
('qwen3.7-max',                  'Qwen 3.7 Max',                 'smart', 1000000, 16384, 1,0,0,1, 0.00164384,0.00493151, 1779376861, 1, 1779376861, 0),
('qwen3.6-flash',                'Qwen 3.6 Flash',               'fast',   131072,  8192, 1,0,0,1, 0.00016438,0.0009863, 1777261362, 1, 1777261362, 0),
('qwen3-coder-plus',             'Qwen3 Coder Plus',             'smart',  131072,  8192, 1,0,0,1, 0.00054795,0.00219178, 1758662707, 1, 1758662707, 0),
('qwen3-coder-next',             'Qwen3 Coder Next',             'smart',  131072,  8192, 1,0,0,1, 0.00054795,0.00219178, 1770164101, 1, 1770164101, 0),
('qwen3-next-80b-a3b-thinking',  'Qwen3 Next 80B Thinking',      'smart',  131072,  8192, 1,0,0,1, 0.00006849,0.00027397, 1757612284, 1, 1757612284, 0),
-- ── Kimi (月之暗面) ──────────────────────────────────────────
('kimi-k2.6-coding',             'Kimi K2.6 Coding',             'smart', 1000000, 16384, 1,0,0,1, 0.00089041,0.00369863, 1775779200, 1, 1775779200, 0),
('kimi-k2.6',                    'Kimi K2.6',                    'smart', 1000000, 16384, 1,0,0,1, 0.00089041,0.00369863, 1776699402, 1, 1776699402, 0),
('kimi-k2-thinking',             'Kimi K2 Thinking',             'smart', 1000000, 32768, 1,0,0,1, 0.0006,0.0025, 1762440622, 1, 1762440622, 0),
-- ── Doubao (字节豆包) ────────────────────────────────────────
('doubao-1.5-pro',               'Doubao 1.5 Pro',               'smart',  128000,  8192, 1,0,0,1, 0.00010959,0.00027397, 1777939200, 1, 1777939200, 0),
('doubao-1.5-lite',              'Doubao 1.5 Lite',              'fast',   128000,  8192, 1,0,0,1, 0.0000411,0.00008219, 1777939200, 1, 1777939200, 0),
-- ── GLM (智谱) ───────────────────────────────────────────────
('glm-5.1',                      'GLM 5.1',                      'smart',  128000,  8192, 1,0,0,1, 0.00082192,0.00328767, 1775578025, 1, 1775578025, 0),
('glm-5-turbo',                  'GLM 5 Turbo',                  'fast',   128000,  8192, 1,0,0,1, 0.00068493,0.0030137, 1773583573, 1, 1773583573, 0),
-- ── Mistral ─────────────────────────────────────────────────
('mistral-large-2512',           'Mistral Large 2512',           'smart',  200000, 32000, 1,0,0,1, 0.0005,0.0015, 1764624472, 1, 1764624472, 0),
('mistral-small-2603',           'Mistral Small 2603',           'fast',   128000,  8192, 1,0,0,1, 0.0001,0.0003, 1773695685, 1, 1773695685, 0),
('codestral-2508',               'Codestral 2508',               'smart',  200000, 32000, 1,0,0,1, 0.0003,0.0009, 1754079630, 1, 1754079630, 0),
-- ── Perplexity Sonar ────────────────────────────────────────
('sonar-pro',                    'Sonar Pro',                    'smart',  200000, 32000, 1,0,0,1, 0.003,0.015, 1761854366, 1, 1761854366, 0),
('sonar-reasoning-pro',          'Sonar Reasoning Pro',          'smart',  200000, 32000, 1,0,0,1, 0.002,0.008, 1741313308, 1, 1741313308, 0),
('sonar-deep-research',          'Sonar Deep Research',          'smart',  200000, 32000, 1,0,0,1, 0.002,0.008, 1741311246, 1, 1741311246, 0),
-- ── ERNIE (百度) ─────────────────────────────────────────────
('ernie-4.5-300b-a47b',          'ERNIE 4.5 300B',               'smart',  200000, 32000, 1,0,0,1, 0.00028,0.0011, 1751300139, 1, 1751300139, 0),
('ernie-4.5-21b-a3b-thinking',   'ERNIE 4.5 21B Thinking',       'smart',  200000, 32000, 1,0,0,1, 0.0001,0.0004, 1760048887, 1, 1760048887, 0),
('ernie-4.5-21b-a3b',            'ERNIE 4.5 21B',                'fast',   128000,  8192, 1,0,0,1, 0.0001,0.0004, 1760048887, 1, 1760048887, 0),
-- ── MiniMax ─────────────────────────────────────────────────
('minimax-m2.7',                 'MiniMax M2.7',                 'smart',  200000, 32000, 1,0,0,1, 0.00026,0.0012, 1773836697, 1, 1773836697, 0),
('minimax-m2.5',                 'MiniMax M2.5',                 'fast',   128000,  8192, 1,0,0,1, 0.00028767,0.00115068, 1770908502, 1, 1770908502, 0),
-- ── NVIDIA Nemotron ─────────────────────────────────────────
('nemotron-3-super-120b-a12b',   'Nemotron 3 Super 120B',        'smart',  200000, 32000, 1,0,0,1, 0.001,0.004, 1773245239, 1, 1773245239, 0),
('nemotron-3-nano-30b-a3b',      'Nemotron 3 Nano 30B',          'fast',   128000,  8192, 1,0,0,1, 0.0002,0.0006, 1765731275, 1, 1765731275, 0),
-- ── Amazon Nova (Bedrock) ───────────────────────────────────
('nova-pro-v1',                  'Nova Pro V1',                  'smart',  200000, 32000, 1,0,0,1, 0.0008,0.0032, 1733436303, 1, 1733436303, 0),
('nova-lite-v1',                 'Nova Lite V1',                 'fast',   128000,  8192, 1,0,0,1, 0.00006,0.00024, 1733437363, 1, 1733437363, 0),
-- ── StepFun ─────────────────────────────────────────────────
('step-3.5-flash',               'Step 3.5 Flash',               'fast',   128000,  8192, 1,0,0,1, 0.00009589,0.00028767, 1769728337, 1, 1769728337, 0),
-- ── Hunyuan (腾讯混元) ───────────────────────────────────────
('hunyuan-a13b-instruct',        'Hunyuan A13B Instruct',        'smart',  200000, 32000, 1,0,0,1, 0.00013699,0.00054795, 1751987664, 1, 1751987664, 0);

-- ============================================================
-- SECTION 7: DML — 厂商模型映射种子数据
-- ============================================================

INSERT INTO sys_provider_models (provider_id, model_id, actual_model_id) VALUES
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
-- ── OpenAI o 系列 ────────────────────────────────────────────
-- ── Google Gemini 3.x ───────────────────────────────────────
('google',                          'gemini-3.5-flash',                  'gemini-3.5-flash'),
('google',                          'gemini-3.1-pro',                    'gemini-3.1-pro'),
('google',                          'gemini-3.1-pro-preview',            'gemini-3.1-pro-preview'),
('google',                          'gemini-3.1-pro-preview-customtools','gemini-3.1-pro-preview-customtools'),
('google',                          'gemini-3.1-flash',                  'gemini-3.1-flash'),
('google',                          'gemini-3.1-flash-lite',             'gemini-3.1-flash-lite'),
('custom_google',                   'gemini-3.5-flash',                  'gemini-3.5-flash'),
('custom_google',                   'gemini-3.1-pro',                    'gemini-3.1-pro'),
('custom_google',                   'gemini-3.1-pro-preview',            'gemini-3.1-pro-preview'),
('custom_google',                   'gemini-3.1-pro-preview-customtools','gemini-3.1-pro-preview-customtools'),
('custom_google',                   'gemini-3.1-flash',                  'gemini-3.1-flash'),
('custom_google',                   'gemini-3.1-flash-lite',             'gemini-3.1-flash-lite'),
('agent_platform','gemini-3.5-flash',                  'gemini-3.5-flash'),
('agent_platform','gemini-3.1-pro-preview',            'gemini-3.1-pro-preview'),
('agent_platform','gemini-3.1-pro-preview-customtools','gemini-3.1-pro-preview-customtools'),
('agent_platform','gemini-3.1-flash',                  'gemini-3.1-flash'),
('agent_platform','gemini-3.1-flash-lite',             'gemini-3.1-flash-lite'),
('opencode',                        'gemini-3.1-pro',                    'gemini-3.1-pro'),
('opencode',                        'gemini-3.1-pro-preview',            'gemini-3.1-pro-preview'),
('opencode',                        'gemini-3.1-pro-preview-customtools','gemini-3.1-pro-preview-customtools'),
('opencode',                        'gemini-3.1-flash',                  'gemini-3.1-flash'),
('opencode',                        'gemini-3.1-flash-lite',             'gemini-3.1-flash-lite'),
('opencode',                        'gemini-3.5-flash',                  'gemini-3.5-flash'),
-- ── Google Gemini 2.0 ───────────────────────────────────────
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
('custom_openai', 'hunyuan-a13b-instruct','hunyuan-a13b-instruct')
ON CONFLICT(provider_id, model_id) DO UPDATE SET actual_model_id = excluded.actual_model_id;

-- ============================================================
-- SECTION 8: DML — 意图路由字典初始化
-- 从 sys_models 批量生成，再补充带日期快照版本
-- ============================================================

-- 从 sys_models 批量初始化
INSERT INTO sys_model_intent_dict (model_id, capability_tier, source)
SELECT model_id, capability_tier, 'seed'
FROM sys_models
WHERE capability_tier IS NOT NULL AND capability_tier != ''
ON CONFLICT(model_id) DO UPDATE SET capability_tier = excluded.capability_tier, source = excluded.source;

-- Claude Code 常发的带日期快照版本（不在 sys_models，单独补充）
INSERT INTO sys_model_intent_dict (model_id, capability_tier, source) VALUES
('claude-3-5-sonnet-20241022', 'smart', 'seed'),
('claude-3-7-sonnet-20250219', 'smart', 'seed'),
('claude-3-haiku-20240307',    'fast',  'seed'),
('claude-3-5-haiku-20241022',  'fast',  'seed'),
('claude-3-opus-20240229',     'smart', 'seed')
ON CONFLICT(model_id) DO UPDATE SET capability_tier = excluded.capability_tier, source = excluded.source;
