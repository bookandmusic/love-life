package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// ActorType 常量：审计日志 actor_type 字段的取值（DESIGN §8.2）。
const (
	ActorTypeUser = "user"
	ActorTypeAI   = "ai"
	ActorTypePAT  = "pat"
)

type DB struct {
	*sql.DB

	postIndex PostIndexer
}

// PostIndexer 动态向量索引的写侧钩子（由 search.Index 实现）。
// 接口定义在 store 侧以隔离依赖：store 不 import search。
type PostIndexer interface {
	IndexPost(ctx context.Context, id int64, title, content, location string) error
	UnindexPost(ctx context.Context, id int64) error
}

// SetPostIndex 注入动态向量索引钩子；未注入（未配置嵌入模型）时索引为空操作。
// 非并发安全：须在启动 HTTP 服务与 janitor 之前调用一次。
func (d *DB) SetPostIndex(idx PostIndexer) { d.postIndex = idx }

func Open(dataDir string) (*DB, error) {
	path := filepath.Join(dataDir, "love-life.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	d := &DB{DB: db}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}
