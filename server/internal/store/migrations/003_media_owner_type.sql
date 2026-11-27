-- migrate:no-fk-check
--
-- 媒体归属改为 (owner_type, owner_id) 两列，并记录「级联删除」来源。
--
-- owner_type 取 store 侧的 MediaOwnerPost / MediaOwnerComment 常量。
-- 刻意不加 CHECK：新增可挂图的实体（纪念日、点位…）只需在 Go 侧加一个常量，
-- media 表结构不动。取值没有用户输入路径，拼错的代价只是该媒体归属解析为空，
-- 不会写入错误关联——用「加实体零迁移」换掉「拼错即被拒」值得。
--
-- cascade_deleted 区分两种删除来源：因父实体（动态/评论）被删而连带删除的置 1，
-- 恢复父实体时只捞这批。用户先前单独删掉的图（0）不会被父实体的恢复一并复活——
-- 那是两个独立的删除意图，复活会让用户看到自己明确删过的照片又出现了。
--
-- 必须重建表：SQLite 去不掉列上的外键。post_id 上的 REFERENCES posts(id)
-- 会让写入评论 id 时报 FOREIGN KEY constraint failed；改约束只能建新表搬数据。
-- 父表被 album_items.media_id 引用，重建期间必须关外键检查，
-- 故本文件首行的 -- migrate:no-fk-check 由 migrate.go 在事务外执行。
CREATE TABLE media_new (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_type TEXT    NOT NULL DEFAULT 'post',
    owner_id   INTEGER NOT NULL,
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
    deleted_by_type TEXT    CHECK (deleted_by_type IN ('user', 'ai')),
    -- 1 = 因父实体被删而连带删除（恢复父实体时一并恢复）；0 = 用户单独删的
    cascade_deleted INTEGER NOT NULL DEFAULT 0
);
INSERT INTO media_new (id, owner_type, owner_id, kind, file_path, thumb_path, width, height, size, duration, taken_at, created_at, deleted_at, deleted_by, deleted_by_type)
SELECT id, 'post', post_id, kind, file_path, thumb_path, width, height, size, duration, taken_at, created_at, deleted_at, deleted_by, deleted_by_type FROM media;
DROP TABLE media;
ALTER TABLE media_new RENAME TO media;
CREATE INDEX idx_media_owner ON media(owner_type, owner_id);