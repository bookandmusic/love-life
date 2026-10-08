-- 001_init.sql — 全量初始 schema（压缩版）
-- 来源：原 001~013 迁移演进收敛（2026-09-06 重新初始化提交时合并）。
-- 实体与字段以 ARCHITECTURE「数据模型」为准；含全部最终修正：
--   media.kind 收 audio（011）；album_items UNIQUE(media_id) 一文件一相册（011）；
--   anniversaries.type solar|lunar（012）；webhook_channels 空间级无用户归属（007）；
--   chat_messages 附件与回显统一由 parts 承载（不再有 media / content 列）；
--   chat_sessions 表 + chat_messages.session_id 多会话归属、role 收窄 user|assistant、
--   chat_messages.turn_id 按轮回滚（2026-09-22）；
--   confirm_actions.preview_json 提案自构造预览落库、chat_result_sets 结果集句柄（2026-09-23）；
--   provider_models.kind + chain_order 模型按类型分链、chat_context 上下文事实源（2026-09-26）；
--   provider_models.thinking 按模型的思考档位（off/low/medium/high，默认 off）；
--   provider_models 参数面收敛为 max_tokens / thinking / thinking_levels / context_window
--   （删 temperature，加支持的思考档位与上下文窗口）（2026-10-08）。
-- 本文件一次性建出最新库：新部署空库直接应用，无历史升级路径。

-- ============ 账号与配置（ARCHITECTURE「数据模型」） ============
CREATE TABLE users (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    username            TEXT    NOT NULL UNIQUE,
    password_hash       TEXT    NOT NULL,
    role                TEXT    NOT NULL CHECK (role IN ('admin', 'girl', 'boy')),
    nickname            TEXT    NOT NULL DEFAULT '',
    birthday            TEXT,
    avatar_path         TEXT,
    ai_style            TEXT    NOT NULL DEFAULT '文艺',
    created_at          TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE refresh_tokens (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id),
    token_hash TEXT    NOT NULL UNIQUE,
    expires_at TEXT    NOT NULL,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_refresh_tokens_user ON refresh_tokens(user_id);

-- ============ 内容（ARCHITECTURE「数据模型」） ============
CREATE TABLE posts (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    author_id              INTEGER NOT NULL REFERENCES users(id),
    content                TEXT    NOT NULL,
    mood                   TEXT,
    point_id               INTEGER,
    location_name          TEXT,
    created_at             TEXT    NOT NULL DEFAULT (datetime('now')),
    deleted_at             TEXT,
    -- 删除者归属（回收站「删除者」列）；软删除时与 deleted_at 一并写入
    deleted_by             INTEGER REFERENCES users(id),
    deleted_by_type        TEXT    CHECK (deleted_by_type IN ('user', 'ai'))
);
CREATE INDEX idx_posts_created ON posts(created_at DESC);
CREATE INDEX idx_posts_author ON posts(author_id);

CREATE TABLE media (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    post_id    INTEGER NOT NULL REFERENCES posts(id),
    kind       TEXT    NOT NULL CHECK (kind IN ('photo', 'video', 'audio')),
    file_path  TEXT    NOT NULL,
    thumb_path TEXT,
    width      INTEGER NOT NULL DEFAULT 0,
    height     INTEGER NOT NULL DEFAULT 0,
    size       INTEGER NOT NULL DEFAULT 0,
    duration   REAL,
    taken_at   TEXT,
    created_at TEXT    NOT NULL DEFAULT (datetime('now')),
    deleted_at TEXT,
    deleted_by      INTEGER REFERENCES users(id),
    deleted_by_type TEXT    CHECK (deleted_by_type IN ('user', 'ai'))
);
CREATE INDEX idx_media_post ON media(post_id);

CREATE TABLE likes (
    post_id    INTEGER NOT NULL REFERENCES posts(id),
    user_id    INTEGER NOT NULL REFERENCES users(id),
    created_at TEXT    NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (post_id, user_id)
);

CREATE TABLE favorites (
    post_id    INTEGER NOT NULL REFERENCES posts(id),
    user_id    INTEGER NOT NULL REFERENCES users(id),
    created_at TEXT    NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (post_id, user_id)
);

CREATE TABLE comments (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    post_id    INTEGER NOT NULL REFERENCES posts(id),
    author_id  INTEGER NOT NULL REFERENCES users(id),
    content    TEXT    NOT NULL,
    created_at TEXT    NOT NULL DEFAULT (datetime('now')),
    deleted_at TEXT,
    deleted_by      INTEGER REFERENCES users(id),
    deleted_by_type TEXT    CHECK (deleted_by_type IN ('user', 'ai'))
);
CREATE INDEX idx_comments_post ON comments(post_id, id DESC);

-- 相册只有手动相册实体；集合视图里的按月分组由查询即时聚合
-- （该月未归属媒体的按月计数），不落月度行（PRODUCT「相册（独立底部 tab）」）。
CREATE TABLE albums (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT    NOT NULL,
    description    TEXT,
    cover_media_id INTEGER,
    created_at     TEXT    NOT NULL DEFAULT (datetime('now')),
    created_by     INTEGER REFERENCES users(id),
    deleted_at     TEXT,
    deleted_by      INTEGER REFERENCES users(id),
    deleted_by_type TEXT    CHECK (deleted_by_type IN ('user', 'ai'))
);

CREATE TABLE album_items (
    album_id  INTEGER NOT NULL REFERENCES albums(id),
    media_id  INTEGER NOT NULL REFERENCES media(id),
    added_by  INTEGER NOT NULL REFERENCES users(id),
    added_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (album_id, media_id)
);
CREATE UNIQUE INDEX idx_album_items_media ON album_items(media_id); -- 一文件一相册（011 收敛）

CREATE TABLE map_points (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL,
    lat         REAL    NOT NULL DEFAULT 0,
    lng         REAL    NOT NULL DEFAULT 0,
    city        TEXT    NOT NULL DEFAULT '',
    visit_date  TEXT,
    description TEXT    NOT NULL DEFAULT '',
    source      TEXT    NOT NULL DEFAULT 'manual' CHECK (source IN ('post', 'manual', 'ai')),
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    deleted_at  TEXT,
    created_by  INTEGER NOT NULL REFERENCES users(id),
    deleted_by      INTEGER REFERENCES users(id),
    deleted_by_type TEXT    CHECK (deleted_by_type IN ('user', 'ai'))
);
CREATE INDEX idx_map_points_name ON map_points(name);
CREATE INDEX idx_map_points_deleted ON map_points(deleted_at);

CREATE TABLE anniversaries (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    name               TEXT    NOT NULL,
    date               TEXT    NOT NULL,
    type               TEXT    NOT NULL DEFAULT 'solar' CHECK (type IN ('solar', 'lunar')),
    is_leap            INTEGER NOT NULL DEFAULT 0,
    remind_days_before INTEGER NOT NULL DEFAULT 1,
    created_at         TEXT    NOT NULL DEFAULT (datetime('now')),
    -- 创建者：纪念日无系统生成路径，恒为用户创建
    created_by         INTEGER NOT NULL REFERENCES users(id),
    deleted_at         TEXT,
    deleted_by         INTEGER REFERENCES users(id),
    deleted_by_type    TEXT    CHECK (deleted_by_type IN ('user', 'ai'))
);

-- ============ 通知与 AI（ARCHITECTURE「数据模型」） ============
CREATE TABLE webhook_channels (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT    NOT NULL DEFAULT '',
    method        TEXT    NOT NULL DEFAULT 'POST' CHECK (method IN ('GET', 'POST')),
    url           TEXT    NOT NULL,
    headers_json  TEXT    NOT NULL DEFAULT '{}',
    body_template TEXT    NOT NULL DEFAULT '',
    events_json   TEXT    NOT NULL DEFAULT '[]',
    created_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE notifications (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    recipient_id  INTEGER NOT NULL REFERENCES users(id),
    event_type    TEXT    NOT NULL,
    target_type   TEXT,
    target_id     INTEGER,
    snapshot_json TEXT,
    is_read       INTEGER NOT NULL DEFAULT 0,
    read_at       TEXT,
    created_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_notifications_recipient ON notifications(recipient_id, created_at DESC);

-- 一条消息 = 一行，parts 是有序 JSON 数组（一轮助手消息的思考、工具步骤、正文、
-- 确认卡全在同一行，ARCHITECTURE「数据模型」ChatMessage）。附件只存本机元数据
-- {name,mime,size,w?,h?}（无 ref、无 base64，AI模式设计「图片处理与存储」）。
CREATE TABLE chat_messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id),
    -- 归属会话（多会话）：0 = 懒创建前的兜底，正常消息恒由服务端落真实会话 id；
    -- 不加 REFERENCES（0 兜底语义），归属一致性由服务层保证
    session_id INTEGER NOT NULL DEFAULT 0,
    -- role 收窄为 user / assistant；tool 步骤、思考链、卡片全部进 parts
    role       TEXT    NOT NULL CHECK (role IN ('user', 'assistant')),
    -- 有序 parts：[{type:text|thinking|tool|card|attachment|quote|interrupted,...}]
    parts      TEXT    NOT NULL DEFAULT '[]',
    -- 确认卡指针（card part 只存 action_id；渲染与回显读 confirm_actions.preview_json）
    card_id    INTEGER,
    -- 用户引用的资源（quote part 回显）：[{result_id, group?, ids?, kind?, id?}]
    quoted     TEXT    NOT NULL DEFAULT '',
    -- 所属对话轮次：服务端每轮生成一次，同一轮的所有消息共享。
    -- 「停止生成」按轮整轮回滚：只删除 (user_id, turn_id) 命中的行，
    -- 不会误删下一轮；旧行与异常路径下为空串（不参与回滚）
    turn_id    TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_chat_messages_user ON chat_messages(user_id, id DESC);
CREATE INDEX idx_chat_messages_session ON chat_messages(session_id, id DESC);

-- 模型上下文（模型事实源，与 chat_messages 展示存储彻底分离，ARCHITECTURE「数据模型」ChatContext）：
-- 追加写；组装时按 seq 取、按 tokens 裁剪。kind 四分类定淘汰策略；
-- pinned=1（已裁决提案往返）永不淘汰。
CREATE TABLE chat_context (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id),
    session_id INTEGER NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
    turn_id    TEXT    NOT NULL DEFAULT '',
    seq        INTEGER NOT NULL,
    role       TEXT    NOT NULL CHECK (role IN ('system', 'user', 'assistant', 'tool')),
    content    TEXT    NOT NULL,
    -- assistant 轮的 tool_calls（name+arguments 原样存，回放时还原协议配对）
    tool_calls_json TEXT,
    tool_call_id   TEXT,
    tool_name      TEXT,
    tokens      INTEGER NOT NULL DEFAULT 0,
    kind        TEXT    NOT NULL CHECK (kind IN ('system', 'dialogue', 'read_result', 'proposal')),
    pinned      INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_chat_context_session ON chat_context(session_id, seq);

-- 会话：懒创建（首条消息送达时建，标题=首条消息摘要）；updated_at 随消息推进，
-- 抽屉按其倒序；硬删 = 连同消息事务删除（聊天为过程性数据，不进回收站）
CREATE TABLE chat_sessions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id),
    title      TEXT    NOT NULL DEFAULT '新的会话',
    created_at TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_chat_sessions_user ON chat_sessions(user_id, updated_at DESC);

CREATE TABLE confirm_actions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id      INTEGER NOT NULL REFERENCES users(id),
    tool_name    TEXT    NOT NULL,
    params_json  TEXT    NOT NULL,
    -- 提案自构造的目标形态预览（Proposal JSON：Kind/Summary/Preview/Warnings/Attachments）。
    -- 工具在 Exec 内构造（非事后从 params 反推），出卡即落库：刷新/跨设备回显直接读，永不反推
    preview_json TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'confirmed', 'cancelled', 'expired', 'failed')),
    expires_at   TEXT    NOT NULL,
    resolved_at  TEXT,
    created_at   TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_confirm_actions_user ON confirm_actions(user_id, id DESC);

-- 结果集句柄（D10）：读工具执行时把结果 id 列表存下来，返回 result_id（r_N）；
-- 提案工具用 source={result_id, group?} 引用整组资源，出提案时即展开为具体 id——
-- 模型不搬运 id 列表（80 张规模必漏必造）。落库而非内存：提案 15 分钟 TTL 期间可能跨重启，
-- 且会话硬删 / janitor 清理需能跟着走表。随会话硬删同删。
CREATE TABLE chat_result_sets (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id INTEGER NOT NULL,
    result_id  TEXT    NOT NULL,
    kind       TEXT    NOT NULL,
    ids_json   TEXT    NOT NULL,
    -- 分组信息（find_media group_by 时）：[{key, label, ids}]，无分组为空
    groups_json TEXT   NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_chat_result_sets_session ON chat_result_sets(session_id, id DESC);

CREATE TABLE audit_logs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_type    TEXT    NOT NULL CHECK (actor_type IN ('user', 'ai', 'pat')),
    actor_id      INTEGER NOT NULL,
    action        TEXT    NOT NULL,
    params_digest TEXT    NOT NULL DEFAULT '',
    -- 人类可读参数全文（AD7 审计页展示，如 content: "周末出游"）；params_digest 保留供检索
    params        TEXT    NOT NULL DEFAULT '',
    result        TEXT    NOT NULL DEFAULT '',
    -- 模型响应耗时，仅 ai/pat 调用有值；user 直接写库为 NULL（契约 routes.mjs audit）
    latency_ms    INTEGER,
    created_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_audit_logs_created ON audit_logs(created_at DESC);

-- 个人访问令牌 PAT（对外集成）：单实例双通道的外部通道凭证。
-- 仅绑定情侣用户（girl/boy），admin 不签发；明文只在生成响应中出现一次，库内只存哈希。
CREATE TABLE personal_access_tokens (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT    NOT NULL,
    user_id      INTEGER NOT NULL REFERENCES users(id),
    tier         TEXT    NOT NULL CHECK (tier IN ('readonly', 'readwrite')),
    token_hash   TEXT    NOT NULL UNIQUE,
    status       TEXT    NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    last_used_at TEXT,
    revoked_at   TEXT
);
CREATE INDEX idx_pat_hash ON personal_access_tokens(token_hash);

-- AI 调用统计（L5）：每次模型调用落一行，供仪表盘 ai-usage 近 14 天聚合与审计 latency。
CREATE TABLE ai_usage_logs (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    provider_id       INTEGER,
    model             TEXT    NOT NULL DEFAULT '',
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens      INTEGER NOT NULL DEFAULT 0,
    latency_ms        INTEGER NOT NULL DEFAULT 0,
    status            TEXT    NOT NULL DEFAULT 'success' CHECK (status IN ('success', 'failed')),
    created_at        TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_ai_usage_created ON ai_usage_logs(created_at DESC);

-- 回收站恢复日志：操作人/时间/恢复内容摘要；不可删除，永久保留（ARCHITECTURE「回收站机制」）
CREATE TABLE restore_logs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    operator   TEXT    NOT NULL,
    summary    TEXT    NOT NULL,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_restore_logs_created ON restore_logs(created_at DESC);

-- ============ 情侣空间与地图（ARCHITECTURE「数据模型」） ============
CREATE TABLE space (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    name            TEXT    NOT NULL DEFAULT '我们的空间',
    signature       TEXT    NOT NULL DEFAULT '',
    love_start_date TEXT
);
INSERT INTO space (id) VALUES (1);

CREATE TABLE map_sources (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    type           TEXT    NOT NULL CHECK (type IN ('public', 'tianditu')),
    url_template   TEXT    NOT NULL DEFAULT '',
    tianditu_key   TEXT    NOT NULL DEFAULT '',
    tianditu_layer TEXT    NOT NULL DEFAULT 'vec_w',
    enabled        INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE UNIQUE INDEX idx_map_sources_enabled ON map_sources(enabled) WHERE enabled = 1;

-- 模型供应商（ARCHITECTURE「数据模型」ModelProvider）：api_type 决定协议；api_key_enc 加密存储。
-- 用哪个模型、按什么顺序重试，全部由模型条目的链序决定，供应商本身无默认模型。
CREATE TABLE model_providers (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    api_type    TEXT    NOT NULL DEFAULT 'openai-chat'
                CHECK (api_type IN ('openai-chat', 'openai-responses', 'anthropic', 'gemini')),
    name        TEXT    NOT NULL,
    base_url    TEXT    NOT NULL,
    api_key_enc TEXT    NOT NULL DEFAULT '',
    created_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- 模型条目：kind 决定属于哪条链（chat / embedding），chain_order 是同类型内的全局链序
-- （跨供应商，重试即按它依次走）；嵌入类型只允许 openai-chat 供应商（应用层校验）。
-- max_tokens / context_window 可空 = 调用不传，用供应商默认（context_window 空则用服务端兜底预算）。
-- 不设 temperature：现役模型的思考恒开，anthropic 侧与 temperature 互斥、同传即 400，
-- 留着只会形成「配了不生效」的静默错觉（详见 AI模式设计「模型网关与配置中心」）。
CREATE TABLE provider_models (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    provider_id INTEGER NOT NULL REFERENCES model_providers(id) ON DELETE CASCADE,
    kind        TEXT    NOT NULL CHECK (kind IN ('chat', 'embedding')),
    model_id    TEXT    NOT NULL,
    max_tokens  INTEGER,
    -- 思考档位：off（缺省）/ low / medium / high。现役模型关不掉思考，故 off = 该协议的最低档
    -- （仍可能产生少量思考 token）；档位由用户按模型决定，因为开启会产生思考 token 计费。
    -- 用 NOT NULL DEFAULT 'off' 而非可空：off 与「未设置」同义，避免一个语义两种表示。
    thinking    TEXT    NOT NULL DEFAULT 'off',
    -- 该模型支持的思考档位（off/low/medium/high 的子集，逗号分隔且按固定顺序），缺省只有 off。
    -- 存在的理由：档位取值是模型属性而非协议属性（如 Gemini 3 Pro 只接受 low/high，发 medium 即 400）。
    -- 放配置面声明而不在代码里判模型名：供应商别名会让名称匹配落空，落空即报错或静默失效。
    thinking_levels TEXT NOT NULL DEFAULT 'off',
    -- 模型上下文窗口（token）。空 = 用服务端兜底预算；消费者是上下文组装的裁剪预算。
    context_window INTEGER,
    chain_order INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE (provider_id, kind, model_id)
);

-- 输出风格库：内置固化（builtin=1）+ 后台自定义；全局默认落 app_settings.default_style_id
CREATE TABLE output_styles (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL UNIQUE,
    prompt     TEXT    NOT NULL,
    builtin    INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
-- 内置风格固化（PRODUCT「设置」输出风格「文艺 / 幽默 / 简洁」，与 mock fixtures.styles 逐字一致）；
-- 自然自增即 id 1/2/3，「文艺」= id 1 与下方 default_style_id 对应。
INSERT INTO output_styles (name, prompt, builtin, sort_order) VALUES
    ('文艺', '语气文艺有文采，善用比喻和意象，像写给彼此的信一样温柔。', 1, 1),
    ('幽默', '语气幽默活泼，可以适当开玩笑和自嘲，营造轻松氛围。', 1, 2),
    ('简洁', '回答尽量简短，直给要点，不啰嗦。', 1, 3);

-- 全局单值配置（如 default_style_id）
CREATE TABLE app_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
INSERT INTO app_settings (key, value) VALUES ('default_style_id', '1');
