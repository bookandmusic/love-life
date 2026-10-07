package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/bookandmusic/love-life/server/internal/geo"
)

// MapPoint 地图点位（双来源同池——动态带地点自动同步 / 手动与 AI 创建；
// 同名复用同一点位，坐标以先创建者为准，不做邻近合并）。
type MapPoint struct {
	ID          int64
	Name        string
	Lat         float64
	Lng         float64
	City        string // 写时判定落库；空 = 未判定（不进城市列表）
	VisitDate   sql.NullString
	Description string
	Source      string // post | manual | ai
	CreatedAt   string
	CreatedBy   int64
}

// resolveCity 城市判定：坐标 point-in-polygon 优先，未命中回退「·」前缀，再未命中留空。
func resolveCity(name string, lat, lng float64) string {
	if ix, err := geo.Default(); err == nil {
		if c := ix.Locate(lat, lng); c != "" {
			return c
		}
	}
	return geo.FallbackCity(name)
}

const mapPointColumns = `id, name, lat, lng, city, visit_date, description, source, created_at, created_by`

func scanMapPoint(row interface{ Scan(...any) error }) (*MapPoint, error) {
	var p MapPoint
	if err := row.Scan(&p.ID, &p.Name, &p.Lat, &p.Lng, &p.City, &p.VisitDate, &p.Description, &p.Source, &p.CreatedAt, &p.CreatedBy); err != nil {
		return nil, err
	}
	return &p, nil
}

// ListPoints 全量点位（软删过滤，按创建时间倒序）。
func (d *DB) ListPoints(ctx context.Context) ([]*MapPoint, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT `+mapPointColumns+` FROM map_points WHERE deleted_at IS NULL ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*MapPoint
	for rows.Next() {
		p, err := scanMapPoint(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// SearchPoints 按名称/描述模糊搜索未删点位。
// limit <= 0 表示不限（find_places 翻页要取全量再切片：SQL 先 LIMIT 再跳 offset
// 会让 offset 超过 limit 的页永远为空）。
func (d *DB) SearchPoints(ctx context.Context, query string, limit int) ([]*MapPoint, error) {
	like := "%" + query + "%"
	sql := `SELECT ` + mapPointColumns + ` FROM map_points
		WHERE deleted_at IS NULL AND (name LIKE ? OR description LIKE ?)
		ORDER BY created_at DESC, id DESC`
	args := []any{like, like}
	if limit > 0 {
		sql += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := d.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*MapPoint
	for rows.Next() {
		p, err := scanMapPoint(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// GetPoint 单个点位（含已删，供软删前校验）；不存在返回 ErrNotFound。
func (d *DB) GetPoint(ctx context.Context, id int64) (*MapPoint, error) {
	p, err := scanMapPoint(d.QueryRowContext(ctx,
		`SELECT `+mapPointColumns+` FROM map_points WHERE id = ?`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return p, nil
}

// GetPointActive 单个未删点位；不存在或已软删返回 ErrNotFound。
// 供「不该看见回收站数据」的读取路径使用（AI 工具 get_record / 删除预览）——
// ARCHITECTURE「删除与恢复流程」：AI Agent 无法搜索 / 访问已删除内容。
// 注意不要用 GetPoint 代替：那条「含已删」语义是软删前取行（拿名称与创建者）所必需的。
func (d *DB) GetPointActive(ctx context.Context, id int64) (*MapPoint, error) {
	p, err := scanMapPoint(d.QueryRowContext(ctx,
		`SELECT `+mapPointColumns+` FROM map_points WHERE id = ? AND deleted_at IS NULL`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return p, nil
}

// FindPointByName 同名未删点位（同名复用键 = 地点名）。
func (d *DB) FindPointByName(ctx context.Context, name string) (*MapPoint, error) {
	p, err := scanMapPoint(d.QueryRowContext(ctx,
		`SELECT `+mapPointColumns+` FROM map_points WHERE name = ? AND deleted_at IS NULL ORDER BY id LIMIT 1`, name))
	if err != nil {
		return nil, err
	}
	return p, nil
}

// CreatePoint 新建点位；城市写时判定落库。
func (d *DB) CreatePoint(ctx context.Context, p *MapPoint) (int64, error) {
	p.City = resolveCity(p.Name, p.Lat, p.Lng)
	res, err := d.ExecContext(ctx,
		`INSERT INTO map_points (name, lat, lng, city, visit_date, description, source, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Lat, p.Lng, p.City, p.VisitDate, p.Description, p.Source, p.CreatedBy)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// PointInput 创建/复用点位的输入（双来源同池共用入口：动态同步 / REST 手动 / AI 工具）。
type PointInput struct {
	Name        string
	Lat, Lng    float64
	VisitDate   string
	Description string
	Source      string // post | manual | ai
	UserID      int64
}

// FindOrCreatePoint 同名复用语义：命中即返回既有点位（created=false，不改坐标）；
// 未命中建点（坐标以先创建者为准）。动态同步、手动创建、AI 报点共用此入口。
func (d *DB) FindOrCreatePoint(ctx context.Context, in PointInput) (*MapPoint, bool, error) {
	if hit, err := d.FindPointByName(ctx, in.Name); err == nil {
		return hit, false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	id, err := d.CreatePoint(ctx, &MapPoint{
		Name: in.Name, Lat: in.Lat, Lng: in.Lng, VisitDate: NullStr(in.VisitDate),
		Description: in.Description, Source: in.Source, CreatedBy: in.UserID,
	})
	if err != nil {
		return nil, false, err
	}
	p, err := d.GetPoint(ctx, id)
	return p, true, err
}

// SoftDeletePoint 软删点位（进回收站体系）；不存在或已删返回 ErrNotFound。
func (d *DB) SoftDeletePoint(ctx context.Context, id int64, actorType string, actorID int64) error {
	res, err := d.ExecContext(ctx,
		`UPDATE map_points SET deleted_at = datetime('now'), deleted_by = ?, deleted_by_type = ? WHERE id = ? AND deleted_at IS NULL`,
		actorID, actorType, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CityCount 城市聚合项（城市列表 = 点位 city 落库值的自然统计）。
type CityCount struct {
	City  string `json:"city"`
	Count int    `json:"count"`
}

// PointCities 城市列表：按 city 分组计数（未判定 city 的点位不计入）。
func (d *DB) PointCities(ctx context.Context) ([]CityCount, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT city, COUNT(*) FROM map_points
		WHERE deleted_at IS NULL AND city != ''
		GROUP BY city ORDER BY COUNT(*) DESC, city`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []CityCount
	for rows.Next() {
		var c CityCount
		if err := rows.Scan(&c.City, &c.Count); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}
