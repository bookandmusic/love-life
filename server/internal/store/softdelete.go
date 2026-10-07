package store

import (
	"context"
	"database/sql"
	"fmt"
)

// 软删的事务边界：单条软删（如 REST 删一条动态）与整批软删（AI 批量删除）共用同一段
// SQL 与同一套关系数据释放口径，只是执行口不同（直连 *DB 或批量事务内的 *sql.Tx）。

// sqlExec 事务与连接共有的最小执行口（*sql.Tx 与 *DB 均满足）。
type sqlExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// softDeleteKind 在给定执行口上软删一条，并同口释放该 kind 的关系数据：
// 动态级联其媒体（媒体只能随动态存在）；相册清空 album_items（软删即释放媒体归属，
// 客户端设计「删除交互统一规范」：照片回到未归属状态）。未命中未删记录返回 ErrNotFound。
// kind → 表取自 recycleSources 的登记（可回收表清单只此一份，不另设 kind → 表映射）。
func softDeleteKind(ctx context.Context, ex sqlExec, kind string, id int64, actorType string, actorID int64) error {
	table := recycleTableByKind(kind)
	if table == "" {
		return fmt.Errorf("未知删除类型: %s", kind)
	}
	res, err := ex.ExecContext(ctx,
		`UPDATE `+table+` SET deleted_at = datetime('now'), deleted_by = ?, deleted_by_type = ? WHERE id = ? AND deleted_at IS NULL`,
		actorID, actorType, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	switch kind {
	case "post":
		_, err := ex.ExecContext(ctx,
			`UPDATE media SET deleted_at = datetime('now'), deleted_by = ?, deleted_by_type = ? WHERE post_id = ? AND deleted_at IS NULL`,
			actorID, actorType, id)
		return err
	case "album":
		_, err := ex.ExecContext(ctx, `DELETE FROM album_items WHERE album_id = ?`, id)
		return err
	}
	return nil
}

// softDeleteKindTx 单条软删，自开事务：把删除与其关系数据释放绑成一个原子步
// （否则更新成功、释放失败会留下「已软删但媒体归属仍被占用」的相册——读路径已隐藏它，
// 归属却静默占着，无法排查）。只有要释放关系数据的 kind 需要它；单条 UPDATE 本身就是原子的，
// 评论 / 纪念日 / 点位直接走 softDeleteKind(ctx, d, …)，不空包一层事务。
func (d *DB) softDeleteKindTx(ctx context.Context, kind string, id int64, actorType string, actorID int64) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := softDeleteKind(ctx, tx, kind, id, actorType, actorID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if kind == "post" {
		d.unindexPost(ctx, id)
	}
	return nil
}

// SoftDeleteBatch 单事务内按 kind 逐条软删：任一条失败整批回滚，不留半删状态。
// 向量索引在提交后摘除（索引失败只告警，不回滚删除）。
func (d *DB) SoftDeleteBatch(ctx context.Context, kind string, ids []int64, actorType string, actorID int64) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range ids {
		if err := softDeleteKind(ctx, tx, kind, id, actorType, actorID); err != nil {
			return fmt.Errorf("#%d：%w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if kind == "post" {
		for _, id := range ids {
			d.unindexPost(ctx, id)
		}
	}
	return nil
}
