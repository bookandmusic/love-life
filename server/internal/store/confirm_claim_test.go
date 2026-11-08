package store

import (
	"errors"
	"sync"
	"testing"
)

// 并发裁决只能有一个赢家：认领靠 `UPDATE ... WHERE status='pending'` 的受影响行数判定。
// 谁拿到 nil 谁就去执行写工具，多一个赢家就多一份重复数据。
func TestResolveConfirmActionHasSingleWinner(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)
	ctx := t.Context()

	id, err := db.CreateConfirmAction(ctx, 1, "create_post", `{"content":"hi"}`,
		`{"summary":"发动态","preview":{"content":"用户正文"}}`)
	if err != nil {
		t.Fatalf("create confirm action: %v", err)
	}

	const racers = 16
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			switch err := db.ResolveConfirmAction(ctx, id, "confirmed"); {
			case err == nil:
				mu.Lock()
				wins++
				mu.Unlock()
			case errors.Is(err, ErrConfirmResolved):
			default:
				t.Errorf("认领失败必须是 ErrConfirmResolved，实际 %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if wins != 1 {
		t.Fatalf("并发认领成功 %d 次，必须恰好 1 次", wins)
	}
}
