package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// ---- AD8 个人访问令牌 PAT（DESIGN §8.2）----
//
// 单实例双通道的外部通道凭证：PAT 仅绑定情侣用户（girl/boy），admin 不签发；
// 明文 token 仅在生成响应中出现一次，库内只存 SHA-256 哈希。

// ErrPATAdminForbidden 尝试为 admin 角色签发 PAT（铁律：admin 仅后台操作）。
var ErrPATAdminForbidden = errors.New("PAT 仅可绑定情侣用户（girl/boy），admin 不签发令牌")

// PAT 令牌明文前缀（可辨识来源，便于日志与排障）。
const patPrefix = "llp_"

// PersonalAccessToken PAT 行。
type PersonalAccessToken struct {
	ID         int64
	Name       string
	UserID     int64
	Username   string // 绑定用户显示名（昵称优先）
	Tier       string
	Status     string
	CreatedAt  string
	LastUsedAt sql.NullString
}

// hashPAT 明文 → 库内哈希（令牌为高熵随机串，SHA-256 足够，无需加盐慢哈希）。
func hashPAT(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// CreatePAT 签发令牌。admin 角色拒绝；返回明文（仅此一次）。
func (d *DB) CreatePAT(ctx context.Context, name string, userID int64, tier string) (string, *PersonalAccessToken, error) {
	var role string
	if err := d.QueryRowContext(ctx, `SELECT role FROM users WHERE id = ?`, userID).Scan(&role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, ErrNotFound
		}
		return "", nil, err
	}
	if role == "admin" {
		return "", nil, ErrPATAdminForbidden
	}

	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, err
	}
	plain := patPrefix + hex.EncodeToString(buf)

	res, err := d.ExecContext(ctx,
		`INSERT INTO personal_access_tokens (name, user_id, tier, token_hash) VALUES (?, ?, ?, ?)`,
		name, userID, tier, hashPAT(plain))
	if err != nil {
		return "", nil, err
	}
	id, _ := res.LastInsertId()

	tok, err := d.GetPAT(ctx, id)
	if err != nil {
		return "", nil, err
	}
	return plain, tok, nil
}

const patColumns = `p.id, p.name, p.user_id, COALESCE(NULLIF(u.nickname, ''), u.username), p.tier, p.status, p.created_at, p.last_used_at`

func scanPAT(row interface{ Scan(...any) error }) (*PersonalAccessToken, error) {
	var t PersonalAccessToken
	if err := row.Scan(&t.ID, &t.Name, &t.UserID, &t.Username, &t.Tier, &t.Status, &t.CreatedAt, &t.LastUsedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

// GetPAT 按 id 读令牌。
func (d *DB) GetPAT(ctx context.Context, id int64) (*PersonalAccessToken, error) {
	return scanPAT(d.QueryRowContext(ctx,
		`SELECT `+patColumns+` FROM personal_access_tokens p JOIN users u ON u.id = p.user_id WHERE p.id = ?`, id))
}

// ListPATs 全部令牌，最新在前（后台列表）。
func (d *DB) ListPATs(ctx context.Context) ([]*PersonalAccessToken, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT `+patColumns+` FROM personal_access_tokens p JOIN users u ON u.id = p.user_id ORDER BY p.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*PersonalAccessToken, 0)
	for rows.Next() {
		t, err := scanPAT(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokePAT 吊销令牌；不存在 → ErrNotFound（契约 404「令牌不存在」）。
func (d *DB) RevokePAT(ctx context.Context, id int64) (*PersonalAccessToken, error) {
	res, err := d.ExecContext(ctx,
		`UPDATE personal_access_tokens SET status = 'revoked', revoked_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return d.GetPAT(ctx, id)
}

// AuthenticatePAT 校验明文令牌并返回绑定的用户（外部 MCP 通道鉴权）。
// 仅 active 令牌可用；校验通过时顺带更新 last_used_at。
func (d *DB) AuthenticatePAT(ctx context.Context, plain string) (*PersonalAccessToken, error) {
	t, err := scanPAT(d.QueryRowContext(ctx,
		`SELECT `+patColumns+` FROM personal_access_tokens p JOIN users u ON u.id = p.user_id
		 WHERE p.token_hash = ? AND p.status = 'active'`, hashPAT(plain)))
	if err != nil {
		return nil, err
	}
	_, _ = d.ExecContext(ctx,
		`UPDATE personal_access_tokens SET last_used_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), t.ID)
	return t, nil
}
