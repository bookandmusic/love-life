package chat

import (
	"strings"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/tools"
)

// 引用条数上限由服务端收口：客户端只让选 3 条，直接调接口可绕过。
func TestValidateQuotedRejectsOverLimit(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := NewService(db, []byte("0123456789abcdef0123456789abcdef"), &tools.Deps{DB: db})

	refs := []quoteRef{{}, {}, {}, {}}
	_, _, _, err = svc.validateQuoted(t.Context(), 1, refs)
	if err == nil || !strings.Contains(err.Error(), "引用最多 3 条") {
		t.Fatalf("4 条引用应被拒，实得 %v", err)
	}

	// 3 条通过条数检查，落进逐条格式校验（空的 ref 不是合法引用）
	_, _, _, err = svc.validateQuoted(t.Context(), 1, refs[:3])
	if err == nil || !strings.Contains(err.Error(), "引用格式无效") {
		t.Fatalf("3 条应进入逐条校验（上限不得提前一条），实得 %v", err)
	}
}
