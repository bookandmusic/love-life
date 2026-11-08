package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/media"
	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/tools"
)

// newChatServiceWithMedia 带真实上传管线的 chat Service（临时区落 t.TempDir）。
func newChatServiceWithMedia(t *testing.T) (*Service, *tools.Deps) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mp := media.New(t.TempDir(), []byte("0123456789abcdef0123456789abcdef"), 500<<20)
	deps := &tools.Deps{DB: db, MP: mp}
	return NewService(db, []byte("0123456789abcdef0123456789abcdef"), deps), deps
}

// tempMedia 造一个已收尾的临时上传，返回 token。
func tempMedia(t *testing.T, deps *tools.Deps, name string, size int, ownerID int64) string {
	t.Helper()
	up, err := deps.MP.SaveTemp(bytes.NewReader(make([]byte, size)), name, ownerID)
	if err != nil {
		t.Fatalf("SaveTemp(%s, %d): %v", name, size, err)
	}
	got, finalized, err := deps.MP.UploadState(up.Token, ownerID)
	if err != nil || !finalized || got != int64(size) {
		t.Fatalf("UploadState = (%d,%v,%v)，want (%d,true,nil)", got, finalized, err, size)
	}
	return up.Token
}

// 确认阶段的附件合计按临时区**实际文件体积**复核，不相信消息阶段客户端上报的 size。
func TestCheckMediaBudgetUsesActualFileSize(t *testing.T) {
	svc, deps := newChatServiceWithMedia(t)
	const userID = int64(1)

	// 单个 1MB：通过
	single := tempMedia(t, deps, "a.mp4", 1<<20, userID)
	if err := svc.checkMediaBudget(userID, []string{single}); err != nil {
		t.Fatalf("单个 1MB 应通过，实得 %v", err)
	}

	// 两个合计 9MB（5MB + 4MB）：超过 8MB 上限
	big1 := tempMedia(t, deps, "b.mp4", 5<<20, userID)
	big2 := tempMedia(t, deps, "c.mp4", 4<<20, userID)
	err := svc.checkMediaBudget(userID, []string{big1, big2})
	if err == nil || !strings.Contains(err.Error(), "附件总体积过大") {
		t.Fatalf("合计 9MB 应被拒，实得 %v", err)
	}

	// 他人 token / 未收尾 token 不计入合计（工具执行阶段会按 failed 拒绝，预算层不误判）
	if err := svc.checkMediaBudget(userID+1, []string{single}); err != nil {
		t.Fatalf("他人 token 不应计入合计，实得 %v", err)
	}
	up, err := deps.MP.BeginUpload("d.mp4", userID)
	if err != nil {
		t.Fatalf("BeginUpload: %v", err)
	}
	if err := svc.checkMediaBudget(userID, []string{up.Token}); err != nil {
		t.Fatalf("未收尾 token 不应计入合计，实得 %v", err)
	}
	// 混入无效 token 不影响超限判定
	if err := svc.checkMediaBudget(userID, []string{big1, big2, "not-a-token"}); err == nil {
		t.Fatal("含无效 token 时合计仍应超限")
	}

	if err := svc.checkMediaBudget(userID, nil); err != nil {
		t.Fatalf("无附件应通过，实得 %v", err)
	}
}

// 超限必须在**原子认领之前**拒绝：卡片仍是 pending，不能被吃掉。
func TestResolveProposalRejectsOverBudgetBeforeClaim(t *testing.T) {
	svc, deps := newChatServiceWithMedia(t)
	db := deps.DB
	ctx := t.Context()

	if _, err := db.CreateUser(ctx, &store.User{Username: "u1", PasswordHash: "x", Role: "admin", Nickname: "u1"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	sessID, err := db.CreateChatSession(ctx, 1, "附件预算")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	actionID, err := db.CreateConfirmAction(ctx, 1, "budget_tool", "{}", "{}")
	if err != nil {
		t.Fatalf("create confirm action: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO chat_messages (session_id, user_id, role, card_id) VALUES (?, 1, 'assistant', ?)`,
		sessID, actionID); err != nil {
		t.Fatalf("link card: %v", err)
	}

	executed := false
	svc.reg["budget_tool"] = &tools.Tool{
		Name: "budget_tool", IsWrite: true,
		Run: func(context.Context, *tools.Deps, int64, map[string]any, tools.ExecMode) (*tools.ToolResult, error) {
			executed = true
			return &tools.ToolResult{UserText: "完成"}, nil
		},
	}

	tokens := []string{
		tempMedia(t, deps, "x.mp4", 5<<20, 1),
		tempMedia(t, deps, "y.mp4", 4<<20, 1),
	}
	body, _ := json.Marshal(map[string]any{"decision": "approve", "media_tokens": tokens})

	tm, err := auth.LoadOrCreateSecret(t.TempDir())
	if err != nil {
		t.Fatalf("auth secret: %v", err)
	}
	token, err := tm.IssueAccessToken(1, "admin", "test")
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	handler := auth.NewService(db, tm).RequireAuth(http.HandlerFunc(svc.ResolveProposal))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/proposals/"+strconv.FormatInt(actionID, 10), bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("id", strconv.FormatInt(actionID, 10))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "附件总体积过大") {
		t.Fatalf("resolve = %d %s，want 400 附件总体积过大", w.Code, w.Body.String())
	}
	if executed {
		t.Fatal("超限时不得执行工具")
	}
	a, err := db.GetConfirmAction(ctx, actionID)
	if err != nil {
		t.Fatalf("get action: %v", err)
	}
	if a.Status != "pending" {
		t.Fatalf("卡片状态 = %q，want pending（超限不得消耗认领）", a.Status)
	}
}
