package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrate 顺序执行尚未应用的 schema 文件；不引入第三方库，20 行内嵌实现即够（单机单库）。
//
// 少数迁移需要重建表（SQLite 改约束 / 删外键的唯一手段）。重建时若外键检查开着，
// DROP TABLE 会因「有子表引用它」失败；而 PRAGMA foreign_keys 在事务内是**空操作**，
// 不能写在迁移 SQL 里。这类迁移在首行写标记 `-- migrate:no-fk-check`，
// 由本函数在**事务外**先关掉外键检查，跑完再打开。
func (d *DB) migrate() error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return err
	}

	entries, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)

	for _, name := range entries {
		version := strings.TrimSuffix(filepath.Base(name), ".sql")
		var applied int
		if err := d.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		content, err := migrationFS.ReadFile(name)
		if err != nil {
			return err
		}
		noFK := strings.HasPrefix(strings.TrimSpace(string(content)), migrateNoFKCheck)
		// 必须钉在**同一条连接**上做「关外键 → 事务内重建 → 查完整性 → 开外键」：
		// PRAGMA foreign_keys 是连接级状态，而 database/sql 是连接池，
		// 连续几次 d.Exec 完全可能落到不同连接上——那样关了等于没关，
		// DROP TABLE 会连带删掉子表（album_items）里引用它的行，直到 foreign_key_check 才发现。
		if noFK {
			if err := d.runMigrationNoFK(version, string(content)); err != nil {
				return err
			}
			continue
		}
		if err := d.runMigration(version, string(content)); err != nil {
			return err
		}
	}
	return nil
}

// migrateNoFKCheck 迁移文件首行的标记：本迁移需要临时关闭外键检查（表重建）。
const migrateNoFKCheck = "-- migrate:no-fk-check"

// runMigrationNoFK 在单条连接上执行「需要关外键检查」的迁移。
// 顺序不能变：PRAGMA 必须在 Begin 之前（事务内改 foreign_keys 是空操作）。
func (d *DB) runMigrationNoFK(version, content string) error {
	conn, err := d.Conn(context.Background())
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	// 无论成败都要把外键检查恢复回来，否则后续所有写入都不受约束。
	restore := func() error {
		_, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=ON`)
		return err
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		_ = restore()
		return err
	}
	if _, err := tx.ExecContext(ctx, content); err != nil {
		_ = tx.Rollback()
		_ = restore()
		return fmt.Errorf("migration %s: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
		_ = tx.Rollback()
		_ = restore()
		return fmt.Errorf("migration %s: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		_ = restore()
		return err
	}

	// 重建后核对外键完整性：有悬挂引用就报错，不带着坏数据继续跑。
	rows, err := conn.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		_ = restore()
		return err
	}
	bad := rows.Next()
	// 必须查 rows.Err()：PRAGMA 执行到一半失败时 Next() 返回 false，
	// 不查就会把「检查没跑完」当成「没有坏行」放行。
	scanErr := rows.Err()
	rows.Close()
	if err := restore(); err != nil {
		return err
	}
	if scanErr != nil {
		return fmt.Errorf("migration %s: foreign_key_check 执行失败: %w", version, scanErr)
	}
	if bad {
		return fmt.Errorf("migration %s: 重建后存在外键悬挂引用（foreign_key_check）", version)
	}
	return nil
}

func (d *DB) runMigration(version, content string) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(content); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("migration %s: %w", version, err)
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("migration %s: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}
