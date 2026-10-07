package chat

import (
	"context"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/tools"
)

// 生效性：后台停用工具后，该工具必须从下发模型的清单中消失（而非仅页面展示），
// 重新启用后恢复。这是「工具启停」功能的真正语义所在。
func TestDisabledToolNotOffered(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := NewService(db, []byte("0123456789abcdef0123456789abcdef"), &tools.Deps{DB: db})
	ctx := context.Background()

	base := svc.toolSpecs(ctx)
	if len(base) != 16 {
		t.Fatalf("baseline specs = %d, want 16（读 7 / 提案 8 / 删除 1）", len(base))
	}
	found := false
	for _, s := range base {
		if s.Function.Name == "find_posts" {
			found = true
		}
	}
	if !found {
		t.Fatal("find_posts should be offered by default")
	}

	if err := db.SetToolEnabled(ctx, "find_posts", false); err != nil {
		t.Fatal(err)
	}
	after := svc.toolSpecs(ctx)
	if len(after) != 15 {
		t.Fatalf("after disable = %d, want 15", len(after))
	}
	for _, s := range after {
		if s.Function.Name == "find_posts" {
			t.Fatal("disabled tool must NOT be offered to the model")
		}
	}

	if err := db.SetToolEnabled(ctx, "find_posts", true); err != nil {
		t.Fatal(err)
	}
	if n := len(svc.toolSpecs(ctx)); n != 16 {
		t.Fatalf("after re-enable = %d, want 15", n)
	}
}
