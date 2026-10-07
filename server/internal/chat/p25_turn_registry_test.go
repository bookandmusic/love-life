package chat

import (
	"testing"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// P2-5：本轮登记前须确认会话仍在（且属于本人），且「终结」后不再算活跃。
// 会话存在性确认与登记同锁，故删会话无法插在两者之间把消息写进已删会话。
func TestBeginTurnRequiresLiveOwnedSession(t *testing.T) {
	s, db := newPromptTestService(t)
	ctx := t.Context()
	hash, _ := auth.HashPassword("pw")
	uid, err := db.CreateUser(ctx, &store.User{Username: "girl", PasswordHash: hash, Role: "girl"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateUser(ctx, &store.User{Username: "boy", PasswordHash: hash, Role: "boy"})
	if err != nil {
		t.Fatal(err)
	}
	sid, err := db.CreateChatSession(ctx, uid, "会话")
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := s.beginTurn(ctx, uid, sid, "t1"); !ok {
		t.Fatal("会话存在时应登记成功")
	}
	if !s.sessionTurnActive(uid, sid) {
		t.Fatal("已登记的本轮应算未终结")
	}
	if _, ok := s.beginTurn(ctx, uid, sid, "t2"); !ok {
		t.Fatal("同会话多轮并存不应被拒")
	}
	if _, ok := s.beginTurn(ctx, other, sid, "t3"); ok {
		t.Fatal("他人会话不得登记")
	}
	if _, ok := s.beginTurn(ctx, uid, sid+999, "t4"); ok {
		t.Fatal("不存在的会话不得登记")
	}
	if _, err := db.DeleteChatSession(ctx, uid, sid); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.beginTurn(ctx, uid, sid, "t5"); ok {
		t.Fatal("会话已删后不得登记（否则消息会落进已删会话）")
	}

	s.endTurn(uid, "t1")
	s.endTurn(uid, "t2")
	if s.sessionTurnActive(uid, sid) {
		t.Fatal("本轮终结后不得再算活跃")
	}
}
