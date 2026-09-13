-- 001_init.sql — 全量初始 schema（压缩版）
-- 来源：原 001~013 迁移演进收敛（2026-09-06 重新初始化提交时合并）。
-- 实体与字段以 DESIGN 第 12 章为准；含全部最终修正：
--   media.kind 收 audio（011）；album_items UNIQUE(media_id) 一文件一相册（011）；
--   anniversaries.type solar|lunar（012）；webhook_channels 空间级无用户归属（007）；
--   chat_messages.media 附件回显列（013）。
-- 本文件一次性建出最新库：新部署空库直接应用，无历史升级路径。

-- ============ 账号与配置（DESIGN 12.1） ============
CREATE TABLE users (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    username            TEXT    NOT NULL UNIQUE,
    password_hash       TEXT    NOT NULL,
    role                TEXT    NOT NULL CHECK (role IN ('admin', 'girl', 'boy')),
    nickname            TEXT    NOT NULL DEFAULT '',
    avatar_media_id     INTEGER,
    birthday            TEXT,
    ai_style            TEXT    NOT NULL DEFAULT '文艺',
    chat_retention_days INTEGER NOT NULL DEFAULT 30,
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

-- ============ 内容（DESIGN 12.2） ============
CREATE TABLE posts (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    author_id              INTEGER NOT NULL REFERENCES users(id),
    title                  TEXT,
    content                TEXT    NOT NULL,
    mood                   TEXT,
    point_id               INTEGER,
    location_name          TEXT,
    related_anniversary_id INTEGER,
    created_at             TEXT    NOT NULL DEFAULT (datetime('now')),
    deleted_at             TEXT,
    -- 删除者归属（AD5 回收站「删除者」列，DESIGN §9）；软删除时与 deleted_at 一并写入
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

CREATE TABLE albums (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    type           TEXT    NOT NULL DEFAULT 'manual' CHECK (type IN ('manual', 'monthly')),
    name           TEXT    NOT NULL,
    description    TEXT,
    month_key      TEXT,
    is_hidden      INTEGER NOT NULL DEFAULT 0,
    cover_media_id INTEGER,
    created_at     TEXT    NOT NULL DEFAULT (datetime('now')),
    -- 创建者：手动相册为真实用户；月度相册由系统生成、无创建者，故可空
    -- （不可加 NOT NULL，否则每月 1 号的 checkMonthlyAlbum 会失败）
    created_by     INTEGER REFERENCES users(id),
    deleted_at     TEXT,
    deleted_by      INTEGER REFERENCES users(id),
    deleted_by_type TEXT    CHECK (deleted_by_type IN ('user', 'ai'))
);
CREATE UNIQUE INDEX idx_albums_month ON albums(month_key) WHERE type = 'monthly' AND month_key IS NOT NULL;

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
    remind_days_before INTEGER NOT NULL DEFAULT 1,
    created_at         TEXT    NOT NULL DEFAULT (datetime('now')),
    -- 创建者：纪念日无系统生成路径，恒为用户创建
    created_by         INTEGER NOT NULL REFERENCES users(id),
    deleted_at         TEXT,
    deleted_by         INTEGER REFERENCES users(id),
    deleted_by_type    TEXT    CHECK (deleted_by_type IN ('user', 'ai'))
);

-- ============ 通知与 AI（DESIGN 12.3） ============
CREATE TABLE webhook_channels (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT    NOT NULL DEFAULT '',
    method        TEXT    NOT NULL DEFAULT 'POST' CHECK (method IN ('GET', 'POST', 'PUT')),
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

CREATE TABLE chat_messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id),
    role       TEXT    NOT NULL CHECK (role IN ('user', 'assistant', 'tool_result')),
    content    TEXT    NOT NULL,
    card_id    INTEGER,
    media      TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_chat_messages_user ON chat_messages(user_id, id DESC);

CREATE TABLE confirm_actions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id),
    tool_name   TEXT    NOT NULL,
    params_json TEXT    NOT NULL,
    status      TEXT    NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'confirmed', 'cancelled', 'expired')),
    expires_at  TEXT    NOT NULL,
    resolved_at TEXT,
    created_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_confirm_actions_user ON confirm_actions(user_id, id DESC);

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

-- 个人访问令牌 PAT（AD8，DESIGN §8.2）：单实例双通道的外部通道凭证。
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

-- 回收站恢复日志（AD5）：操作人/时间/恢复内容摘要；不可删除，永久保留（DESIGN §5/§9）
CREATE TABLE restore_logs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    operator   TEXT    NOT NULL,
    summary    TEXT    NOT NULL,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_restore_logs_created ON restore_logs(created_at DESC);

-- ============ 情侣空间与地图（DESIGN 12.1 / 4.7） ============
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

-- 模型供应商（DESIGN §12.1）：api_type 决定协议，sort_order = 故障切换链顺序；
-- default_model 存模型 ID 字符串（供应商内唯一）；api_key_enc 加密存储。
CREATE TABLE model_providers (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    api_type      TEXT    NOT NULL DEFAULT 'openai-chat'
                  CHECK (api_type IN ('openai-chat', 'openai-responses', 'anthropic', 'gemini')),
    name          TEXT    NOT NULL,
    base_url      TEXT    NOT NULL,
    api_key_enc   TEXT    NOT NULL DEFAULT '',
    default_model TEXT    NOT NULL DEFAULT '',
    sort_order    INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- 模型级参数（temperature/max_tokens 可空 = 调用不传，用供应商默认）
CREATE TABLE provider_models (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    provider_id INTEGER NOT NULL REFERENCES model_providers(id) ON DELETE CASCADE,
    model_id    TEXT    NOT NULL,
    temperature REAL,
    max_tokens  INTEGER,
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE (provider_id, model_id)
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
-- 内置风格固化（REQUIREMENTS §4.1「文艺 / 幽默 / 简洁」，与 mock fixtures.styles 逐字一致）；
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
