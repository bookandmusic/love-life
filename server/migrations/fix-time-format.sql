-- 数据迁移：统一回收站时间格式
-- 问题：历史数据可能混用 RFC3339 (2026-09-12T10:00:00Z) 和 datetime (2026-09-12 10:00:00)
-- 目标：统一为 SQLite datetime 格式
-- 执行时机：部署前在生产数据库上执行一次

-- 处理 posts 表的 RFC3339 格式数据
UPDATE posts
SET deleted_at = datetime(deleted_at)
WHERE deleted_at IS NOT NULL
  AND deleted_at LIKE '%T%Z';

-- 处理 albums 表的 RFC3339 格式数据
UPDATE albums
SET deleted_at = datetime(deleted_at)
WHERE deleted_at IS NOT NULL
  AND deleted_at LIKE '%T%Z';

-- 处理 anniversaries 表的 RFC3339 格式数据
UPDATE anniversaries
SET deleted_at = datetime(deleted_at)
WHERE deleted_at IS NOT NULL
  AND deleted_at LIKE '%T%Z';

-- 处理 media 表的 RFC3339 格式数据（级联自 posts）
UPDATE media
SET deleted_at = datetime(deleted_at)
WHERE deleted_at IS NOT NULL
  AND deleted_at LIKE '%T%Z';

-- 处理 comments 表的 RFC3339 格式数据
UPDATE comments
SET deleted_at = datetime(deleted_at)
WHERE deleted_at IS NOT NULL
  AND deleted_at LIKE '%T%Z';

-- 处理 map_points 表的 RFC3339 格式数据
UPDATE map_points
SET deleted_at = datetime(deleted_at)
WHERE deleted_at IS NOT NULL
  AND deleted_at LIKE '%T%Z';

-- 验证：检查是否还有 RFC3339 格式残留
SELECT
  'posts' as table_name,
  COUNT(*) as rfc3339_count
FROM posts
WHERE deleted_at LIKE '%T%Z'
UNION ALL
SELECT 'albums', COUNT(*) FROM albums WHERE deleted_at LIKE '%T%Z'
UNION ALL
SELECT 'anniversaries', COUNT(*) FROM anniversaries WHERE deleted_at LIKE '%T%Z'
UNION ALL
SELECT 'media', COUNT(*) FROM media WHERE deleted_at LIKE '%T%Z'
UNION ALL
SELECT 'comments', COUNT(*) FROM comments WHERE deleted_at LIKE '%T%Z'
UNION ALL
SELECT 'map_points', COUNT(*) FROM map_points WHERE deleted_at LIKE '%T%Z';
