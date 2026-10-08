package chat

import (
	"context"
	"fmt"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/store"
)

func int64p(v int64) *int64 { return &v }

// seedBudgetUser 建一个用户：chat_sessions.user_id 有外键，没有用户建不了会话。
func seedBudgetUser(t *testing.T, db *store.DB) int64 {
	t.Helper()
	id, err := db.CreateUser(context.Background(), &store.User{
		Username: "girl", PasswordHash: "x", Role: "girl", Nickname: "小女孩",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return id
}

// TestContextBudget 预算 = 链首模型窗口 − 输出预留；未声明窗口回落兜底。
// 取链首是刻意的：上下文在选链后只组装一次，链内换模型不重建（AI模式设计「上下文组装」）。
func TestContextBudget(t *testing.T) {
	cases := []struct {
		name  string
		chain []*chatTarget
		want  int
	}{
		{"空链回落", nil, 0},
		{"未声明窗口回落", []*chatTarget{{ContextWindow: nil}}, 0},
		{
			"窗口 − max_tokens",
			[]*chatTarget{{ContextWindow: int64p(100000), MaxTokens: int64p(8192)}},
			100000 - 8192,
		},
		{
			"未配 max_tokens 用 4096 预留",
			[]*chatTarget{{ContextWindow: int64p(50000)}},
			50000 - 4096,
		},
		{
			"链首决定预算（链内后续模型窗口不参与）",
			[]*chatTarget{{ContextWindow: int64p(200000), MaxTokens: int64p(4096)}, {ContextWindow: int64p(8000)}},
			200000 - 4096,
		},
		{
			"窗口小于输出预留时兜到下限 1024",
			[]*chatTarget{{ContextWindow: int64p(2048), MaxTokens: int64p(4096)}},
			1024,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := contextBudget(c.chain); got != c.want {
				t.Fatalf("contextBudget = %d, want %d", got, c.want)
			}
		})
	}
}

// TestBuildModelHistoryFallsBackToDefaultBudget 未声明窗口时用兜底预算，
// 且旧条目被淘汰后留下残留摘要（否则模型不知道自己「忘了什么」）。
func TestBuildModelHistoryFallsBackToDefaultBudget(t *testing.T) {
	s, db := newPromptTestService(t)
	ctx := context.Background()
	uid := seedBudgetUser(t, db)
	sess, err := db.CreateChatSession(ctx, uid, "预算测试")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	// 造足够多的历史行：远超兜底预算（24000 tokens）
	for i := 0; i < 40; i++ {
		if err := db.AppendContext(ctx, uid, sess, fmt.Sprintf("%d", i+1), "user",
			fmt.Sprintf("第 %d 轮问题", i), "", "", "", 2000, "dialogue", false); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	// tokenBudget = 0 → 走兜底 24000，应只保留约 12 条
	msgs, summary := s.buildModelHistory(ctx, uid, sess, 0)
	if len(msgs) == 0 {
		t.Fatal("兜底预算下不该裁成空")
	}
	if len(msgs) >= 40 {
		t.Fatalf("兜底预算未生效：保留了 %d 条", len(msgs))
	}
	// 最新一条必须在（从最新往回收）
	last := msgs[len(msgs)-1]
	if last.Content != "第 39 轮问题" {
		t.Fatalf("最新条目丢失: %q", last.Content)
	}
	_ = summary
}

// TestBuildModelHistoryRespectsSmallWindow 小窗口必须真的裁掉更多历史，
// 这是 context_window 参数面的唯一消费者——配了不生效就等于没加这个字段。
func TestBuildModelHistoryRespectsSmallWindow(t *testing.T) {
	s, db := newPromptTestService(t)
	ctx := context.Background()
	uid := seedBudgetUser(t, db)
	sess, err := db.CreateChatSession(ctx, uid, "小窗测试")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for i := 0; i < 40; i++ {
		if err := db.AppendContext(ctx, uid, sess, fmt.Sprintf("%d", i+1), "user",
			fmt.Sprintf("第 %d 轮问题", i), "", "", "", 2000, "dialogue", false); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	big, _ := s.buildModelHistory(ctx, uid, sess, 100000)
	small, _ := s.buildModelHistory(ctx, uid, sess, 6000)
	if len(small) >= len(big) {
		t.Fatalf("小预算未裁更多：small=%d big=%d", len(small), len(big))
	}
	if len(big) != 40 {
		t.Fatalf("大预算不该裁：%d", len(big))
	}
}
