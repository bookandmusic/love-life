package chat

import (
	"context"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/tools"
)

// AD4 生效性：后台停用工具后，该工具必须从下发模型的清单中消失（而非仅页面展示），
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
	if len(base) != 26 {
		t.Fatalf("baseline specs = %d, want 26", len(base))
	}
	found := false
	for _, s := range base {
		if s.Function.Name == "search_posts" {
			found = true
		}
	}
	if !found {
		t.Fatal("search_posts should be offered by default")
	}

	if err := db.SetToolEnabled(ctx, "search_posts", false); err != nil {
		t.Fatal(err)
	}
	after := svc.toolSpecs(ctx)
	if len(after) != 25 {
		t.Fatalf("after disable = %d, want 25", len(after))
	}
	for _, s := range after {
		if s.Function.Name == "search_posts" {
			t.Fatal("disabled tool must NOT be offered to the model")
		}
	}

	if err := db.SetToolEnabled(ctx, "search_posts", true); err != nil {
		t.Fatal(err)
	}
	if n := len(svc.toolSpecs(ctx)); n != 26 {
		t.Fatalf("after re-enable = %d, want 26", n)
	}
}
