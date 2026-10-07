package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/tools"
)

// 裁决执行期间，该会话必须与「流式进行中」一样不可删。
//
// 回归的缺陷：ResolveProposal 在**原子认领之后**才执行工具并写回执。那时卡片已不是
// pending（CountPendingProposals 拦不住），也不是一个 turn——删会话能从「工具执行」
// 与「回执落库」之间穿过，回执落进已删会话（chat_messages.session_id 刻意无外键，
// 落库不报错，只留下永不清理的孤儿行）。
func TestResolveRegistersActiveSession(t *testing.T) {
	s, db := newPromptTestService(t)
	ctx := context.Background()

	// chat_messages.user_id 有外键，先建用户
	if _, err := db.CreateUser(ctx, &store.User{Username: "u1", PasswordHash: "x", Role: "admin", Nickname: "u1"}); err != nil {
		t.Fatal(err)
	}
	sessID, err := db.CreateChatSession(ctx, 1, "并发用例")
	if err != nil {
		t.Fatal(err)
	}
	actionID, err := db.CreateConfirmAction(ctx, 1, "slow_tool", "{}", "{}")
	if err != nil {
		t.Fatal(err)
	}
	// 让卡片归属该会话（SessionIDOfCard 读 chat_messages.card_id）
	if _, err := db.ExecContext(ctx,
		`INSERT INTO chat_messages (session_id, user_id, role, card_id) VALUES (?, 1, 'assistant', ?)`,
		sessID, actionID); err != nil {
		t.Fatal(err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	// 用一个「执行中会阻塞」的工具把窗口撑开，在窗口内观察登记状态
	s.reg["slow_tool"] = &tools.Tool{
		Name:    "slow_tool",
		IsWrite: true,
		Run: func(context.Context, *tools.Deps, int64, map[string]any, tools.ExecMode) (*tools.ToolResult, error) {
			once.Do(func() { close(entered) })
			<-release
			return &tools.ToolResult{UserText: "完成"}, nil
		},
	}

	// 走真实鉴权中间件注入 Claims（chat 包内拿不到 auth 的私有 context key）
	tm, err := auth.LoadOrCreateSecret(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	token, err := tm.IssueAccessToken(1, "admin", "test")
	if err != nil {
		t.Fatal(err)
	}
	handler := auth.NewService(db, tm).RequireAuth(http.HandlerFunc(s.ResolveProposal))

	done := make(chan struct{})
	go func() {
		defer close(done)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/proposals/1",
			strings.NewReader(`{"decision":"approve"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		req.SetPathValue("id", strconv.FormatInt(actionID, 10))
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}()

	<-entered // 工具执行中（回执尚未落库）——正是原缺陷的窗口

	// 关键断言：窗口内该会话被登记为活跃，删会话必须被拒
	s.turnMu.Lock()
	active := s.sessionTurnActive(1, sessID)
	s.turnMu.Unlock()
	if !active {
		close(release)
		<-done
		t.Fatal("工具执行期间会话未登记为活跃——删会话能从执行与回执之间穿过")
	}

	close(release)
	<-done

	// 执行结束后必须注销，否则会话就再也删不掉了（把竞态换成锁死）
	s.turnMu.Lock()
	stillActive := s.sessionTurnActive(1, sessID)
	s.turnMu.Unlock()
	if stillActive {
		t.Fatal("裁决结束后登记未注销——会话将永远删不掉")
	}
}
