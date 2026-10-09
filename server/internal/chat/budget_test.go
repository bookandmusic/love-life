package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bookandmusic/love-life/server/internal/llm"
)

// 整轮墙钟预算必须约束**链内逐个模型的重试**，而不只是轮与轮之间。
//
// 触发本组测试的真实事故日志：链上 4 条各耗 30s 连接期超时（agnes / grok 两个模型
// 连极简请求都不回响应头），一轮 120s，而 turnWallBudget 只有 90s —— 首尾跨度 70s 时
// 第 4 条还在跑。代码注释当时称「整体长度由整轮墙钟预算控制」，在链式路径上不成立。
//
// 预算的粒度（设计如此）：检查发生在**两次尝试之间**，故单条在途尝试仍受自身的连接期
// 超时约束，最后一次尝试可以略微越过预算。所以这些测试断言「不会把链上每条都试完」，
// 用**请求计数**判断而非掐秒表——计数不受调度抖动影响。

// countingUpstream 每次请求先睡 perAttempt 再回 500（快速失败），并记录收到的请求数。
func countingUpstream(t *testing.T, perAttempt time.Duration) (*httptest.Server, *int32) {
	t.Helper()
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		select {
		case <-time.After(perAttempt):
		case <-r.Context().Done():
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// fakeTargets 造 n 个指向同一上游的链成员（openai-chat 协议）。
func fakeTargets(t *testing.T, srv *httptest.Server, n int) []*chatTarget {
	t.Helper()
	out := make([]*chatTarget, 0, n)
	for i := 0; i < n; i++ {
		c, err := llm.NewForProtocol(srv.URL, "sk-test", "m", llm.APITypeOpenAIChat)
		if err != nil {
			t.Fatalf("new client: %v", err)
		}
		out = append(out, &chatTarget{Client: c, ProviderID: int64(i + 1), Model: "m"})
	}
	return out
}

// TestChatCallHonoursTurnBudget 预算在链中途耗尽时，剩余模型不再尝试。
//
// 构造：每条尝试先睡 100ms 再失败、预算 250ms、链上 4 条。
// 修复前会试满 4 条；修复后第 4 条前预算已过，只试 3 条。
func TestChatCallHonoursTurnBudget(t *testing.T) {
	s, _ := newPromptTestService(t)
	srv, count := countingUpstream(t, 100*time.Millisecond)
	chain := fakeTargets(t, srv, 4)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	_, _, err := s.chatCall(ctx, chain,
		&llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "hi"}}}, time.Now().Add(250*time.Millisecond))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("全失败的链应返回错误")
	}
	if got := atomic.LoadInt32(count); got > 3 {
		t.Errorf("上游收到 %d 次请求，want ≤3 —— 预算未拦住链内剩余重试（链长 4，每条 100ms）", got)
	}
	if elapsed > 700*time.Millisecond {
		t.Errorf("整轮耗时 %s 表明把链上 4 条都试完了", elapsed)
	}
}

// TestChatCallStreamHonoursTurnBudget 流式路径同款约束。
func TestChatCallStreamHonoursTurnBudget(t *testing.T) {
	s, _ := newPromptTestService(t)
	srv, count := countingUpstream(t, 100*time.Millisecond)
	chain := fakeTargets(t, srv, 4)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	_, _, _, err := s.chatCallStream(ctx, httptest.NewRecorder(), nopFlusher{}, chain,
		&llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "hi"}}}, nil, time.Now().Add(250*time.Millisecond))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("全失败的链应返回错误")
	}
	if got := atomic.LoadInt32(count); got > 3 {
		t.Errorf("上游收到 %d 次请求，want ≤3 —— 流式路径预算未拦住链内剩余重试", got)
	}
	if elapsed > 700*time.Millisecond {
		t.Errorf("流式路径整轮耗时 %s 表明把链上 4 条都试完了", elapsed)
	}
}

// TestChatCallStopsAfterBudget 预算已过期时，一个模型都不该再试，且返回**显式错误**。
//
// 显式错误不可省：调用方（收尾路径）在 err==nil 时会解引用结果（len(res.Choices)），
// 而「一个都没试」的结果必然是 nil —— 返回 nil error 就是 panic。
func TestChatCallStopsAfterBudget(t *testing.T) {
	s, _ := newPromptTestService(t)
	srv, count := countingUpstream(t, time.Millisecond)
	chain := fakeTargets(t, srv, 3)

	start := time.Now()
	_, _, err := s.chatCall(context.Background(), chain,
		&llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "hi"}}}, time.Now().Add(-time.Second))
	elapsed := time.Since(start)

	if err != errTurnBudgetExhausted {
		t.Errorf("err = %v, want errTurnBudgetExhausted（必须是显式错误，否则调用方解引用 nil）", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("预算已过期却仍耗时 %s（应立刻返回）", elapsed)
	}
	if got := atomic.LoadInt32(count); got != 0 {
		t.Errorf("预算已过期却仍发出了 %d 次请求", got)
	}
}

// TestChatCallStreamStopsAfterBudget 流式路径同款。
func TestChatCallStreamStopsAfterBudget(t *testing.T) {
	s, _ := newPromptTestService(t)
	srv, count := countingUpstream(t, time.Millisecond)
	chain := fakeTargets(t, srv, 2)

	_, _, _, err := s.chatCallStream(context.Background(), httptest.NewRecorder(), nopFlusher{}, chain,
		&llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "hi"}}}, nil, time.Now().Add(-time.Second))
	if err != errTurnBudgetExhausted {
		t.Errorf("err = %v, want errTurnBudgetExhausted", err)
	}
	if got := atomic.LoadInt32(count); got != 0 {
		t.Errorf("预算已过期却仍发出了 %d 次请求", got)
	}
}

// nopFlusher 满足 http.Flusher，使流式路径能在 httptest.ResponseRecorder 上跑。
type nopFlusher struct{}

func (nopFlusher) Flush() {}
