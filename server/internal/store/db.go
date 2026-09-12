package store

import (
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
}

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
	d := &DB{db}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}
