package tools

import (
	"testing"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/store"
)

func findTool(t *testing.T, name string) *Tool {
	t.Helper()
	for _, tool := range Registry() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not registered", name)
	return nil
}

func newToolDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// created_by 外键引用 users(id)
	hash, _ := auth.HashPassword("pw")
	if _, err := db.CreateUser(t.Context(), &store.User{Username: "girl", PasswordHash: hash, Role: "girl"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return db
}

// create_map_point：创建 / 同名复用 / 审计落库；delete_map_point：软删 / 二次删报错。
func TestMapPointTools(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db}
	ctx := t.Context()

	create := findTool(t, "create_map_point")
	res, err := create.Exec(ctx, d, 1, map[string]any{"name": "杭州·西湖", "latitude": 30.2467, "longitude": 120.4599, "visit_date": "2026-09-03", "description": "湖边散步"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	data := res.Data.(map[string]any)
	if data["created"] != true || data["point_id"].(int64) == 0 {
		t.Fatalf("create data: %+v", data)
	}
	pointID := data["point_id"].(int64)

	// 同名复用
	res, err = create.Exec(ctx, d, 1, map[string]any{"name": "杭州·西湖", "latitude": 1.0, "longitude": 1.0})
	if err != nil {
		t.Fatalf("reuse: %v", err)
	}
	data = res.Data.(map[string]any)
	if data["created"] != false || data["point_id"].(int64) != pointID {
		t.Fatalf("reuse data: %+v (want id=%d)", data, pointID)
	}

	// name 空 → 报错
	if _, err = create.Exec(ctx, d, 1, map[string]any{}); err == nil {
		t.Fatal("empty name: want error")
	}

	// 缺坐标 → 报错（DESIGN §6.3：latitude/longitude 必填）
	if _, err = create.Exec(ctx, d, 1, map[string]any{"name": "无名地"}); err == nil {
		t.Fatal("missing latitude/longitude: want error")
	}

	// RegistryForPAT：删除类（AppOnly）工具不出现在 PAT 通道工具表
	for _, tool := range RegistryForPAT() {
		if tool.AppOnly {
			t.Fatalf("RegistryForPAT leaked AppOnly tool %q", tool.Name)
		}
	}
	if findTool(t, "delete_map_point").AppOnly != true {
		t.Fatal("delete_map_point must be AppOnly")
	}

	// 删除 → 成功；再删 → 报错
	del := findTool(t, "delete_map_point")
	if res, err = del.Exec(ctx, d, 1, map[string]any{"point_id": float64(pointID)}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err = del.Exec(ctx, d, 1, map[string]any{"point_id": float64(pointID)}); err == nil {
		t.Fatal("re-delete: want error")
	}

	// 审计：create×2 + delete×1
	var audits int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs WHERE actor_type='ai' AND action LIKE '%map_point'`).Scan(&audits); err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if audits != 3 {
		t.Fatalf("audit rows = %d, want 3", audits)
	}
}
