-- 数据迁移：统一库内时间格式（回收站时间列 + 提案有效期/裁决时间 + 令牌 + 通知已读时间）
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

-- 处理 confirm_actions 的提案有效期与裁决时间（过期判定用字符串比较，格式不一致会误判未过期）
UPDATE confirm_actions
SET expires_at = datetime(expires_at)
WHERE expires_at IS NOT NULL
  AND expires_at LIKE '%T%Z';

UPDATE confirm_actions
SET resolved_at = datetime(resolved_at)
WHERE resolved_at IS NOT NULL
  AND resolved_at LIKE '%T%Z';

-- 处理 refresh_tokens 的过期时间（清理任务做字符串比较）
UPDATE refresh_tokens
SET expires_at = datetime(expires_at)
WHERE expires_at IS NOT NULL
  AND expires_at LIKE '%T%Z';

-- 处理 personal_access_tokens 的最后使用与撤销时间
UPDATE personal_access_tokens
SET last_used_at = datetime(last_used_at)
WHERE last_used_at IS NOT NULL
  AND last_used_at LIKE '%T%Z';

UPDATE personal_access_tokens
SET revoked_at = datetime(revoked_at)
WHERE revoked_at IS NOT NULL
  AND revoked_at LIKE '%T%Z';

-- 处理 notifications 的已读时间
UPDATE notifications
SET read_at = datetime(read_at)
WHERE read_at IS NOT NULL
  AND read_at LIKE '%T%Z';

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
SELECT 'map_points', COUNT(*) FROM map_points WHERE deleted_at LIKE '%T%Z'
UNION ALL
SELECT 'confirm_actions.expires_at', COUNT(*) FROM confirm_actions WHERE expires_at LIKE '%T%Z'
UNION ALL
SELECT 'refresh_tokens.expires_at', COUNT(*) FROM refresh_tokens WHERE expires_at LIKE '%T%Z'
UNION ALL
SELECT 'confirm_actions.resolved_at', COUNT(*) FROM confirm_actions WHERE resolved_at LIKE '%T%Z'
UNION ALL
SELECT 'personal_access_tokens.last_used_at', COUNT(*) FROM personal_access_tokens WHERE last_used_at LIKE '%T%Z'
UNION ALL
SELECT 'personal_access_tokens.revoked_at', COUNT(*) FROM personal_access_tokens WHERE revoked_at LIKE '%T%Z'
UNION ALL
SELECT 'notifications.read_at', COUNT(*) FROM notifications WHERE read_at LIKE '%T%Z';
