package store

import (
	"database/sql"
	"testing"
)

// 删会话必须连提案行一起删：卡片与会话只经 chat_messages.card_id 关联，
// 若先删消息再按子查询删 confirm_actions，子查询恒空、提案行（preview 含用户正文）残留。
func TestDeleteChatSessionRemovesConfirmActions(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)
	ctx := t.Context()

	sessionID, err := db.CreateChatSession(ctx, 1, "待删会话")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	cardID, err := db.CreateConfirmAction(ctx, 1, "create_post", `{"content":"hi"}`,
		`{"summary":"发动态","preview":{"content":"用户正文"}}`)
	if err != nil {
		t.Fatalf("create confirm action: %v", err)
	}
	if _, err := db.SaveChatMessage(ctx, &ChatMessage{
		UserID: 1, SessionID: sessionID, TurnID: "t1", Role: "assistant",
		Parts: `[{"type":"card","action_id":1}]`, CardID: sql.NullInt64{Int64: cardID, Valid: true},
	}); err != nil {
		t.Fatalf("save message: %v", err)
	}

	ok, err := db.DeleteChatSession(ctx, 1, sessionID)
	if err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if !ok {
		t.Fatal("删除本会话应返回 true")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM confirm_actions WHERE id = ?`, cardID).Scan(&n); err != nil {
		t.Fatalf("count confirm_actions: %v", err)
	}
	if n != 0 {
		t.Fatal("会话删除后提案行应同删，不能残留 preview 中的用户正文")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE session_id = ?`, sessionID).Scan(&n); err != nil {
		t.Fatalf("count chat_messages: %v", err)
	}
	if n != 0 {
		t.Fatal("会话删除后消息应同删")
	}
}
