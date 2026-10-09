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
	_, _, err := s.chatCall(ctx, chain,
		&llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "hi"}}}, time.Now().Add(250*time.Millisecond))

	if err == nil {
		t.Fatal("全失败的链应返回错误")
	}
	// 只断请求数：它是「预算有没有拦住剩余链」的直接因果证据（去掉 deadline 检查即变 4）。
	// 不断言耗时——4 次尝试约 400ms，掐秒表既分不开 3 与 4 次，又会在慢机器上假失败。
	if got := atomic.LoadInt32(count); got > 3 {
		t.Errorf("上游收到 %d 次请求，want ≤3 —— 预算未拦住链内剩余重试（链长 4，每条 100ms）", got)
	}
}

// TestChatCallStreamHonoursTurnBudget 流式路径同款约束。
func TestChatCallStreamHonoursTurnBudget(t *testing.T) {
	s, _ := newPromptTestService(t)
	srv, count := countingUpstream(t, 100*time.Millisecond)
	chain := fakeTargets(t, srv, 4)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rec := httptest.NewRecorder()
	_, _, _, err := s.chatCallStream(ctx, rec, rec, chain,
		&llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "hi"}}}, nil, time.Now().Add(250*time.Millisecond))

	if err == nil {
		t.Fatal("全失败的链应返回错误")
	}
	if got := atomic.LoadInt32(count); got > 3 {
		t.Errorf("上游收到 %d 次请求，want ≤3 —— 流式路径预算未拦住链内剩余重试", got)
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

	rec := httptest.NewRecorder()
	_, _, _, err := s.chatCallStream(context.Background(), rec, rec, chain,
		&llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "hi"}}}, nil, time.Now().Add(-time.Second))
	if err != errTurnBudgetExhausted {
		t.Errorf("err = %v, want errTurnBudgetExhausted", err)
	}
	if got := atomic.LoadInt32(count); got != 0 {
		t.Errorf("预算已过期却仍发出了 %d 次请求", got)
	}
}

// TestChatCallStreamStopsAfterStreamedDelta 已下发增量后即使预算已过，也不换模型续写。
//
// 这是 AI模式设计「模型网关」的硬不变式：用户绝不能看到半句话被另一个模型接下去。
// 它与「预算到期就不再试下一个」在**同一次失败**上相遇（尝试中途预算刚过、同时已下发
// 增量），故必须断言增量优先——否则链上后一个模型会接着写，用户看到拼接的半句话。
func TestChatCallStreamStopsAfterStreamedDelta(t *testing.T) {
	s, _ := newPromptTestService(t)

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// 先真下发一个增量
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"半句话\"}}]}\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// 再掐断连接（不送 [DONE]）：adapter 走 sc.err() 报错，且此时已产出 deltas。
		// 用 ErrAbortHandler 直接断连，模拟上游中途断流。
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(srv.Close)

	chain := fakeTargets(t, srv, 3) // 链上还有备用模型
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 预算给足（不因预算而停），专测「已下发增量」这一条是否独立生效。
	// 经 onDelta 计数：错误路径会把 deltas 丢弃（返回值里拿不到），
	// 而这条不变式看的正是「是否真的下发过增量」。
	var streamed int32
	rec := httptest.NewRecorder()
	_, _, _, err := s.chatCallStream(ctx, rec, rec, chain,
		&llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "hi"}}},
		func(llm.Delta) { atomic.AddInt32(&streamed, 1) }, time.Now().Add(10*time.Second))

	if err == nil {
		t.Fatal("上游中途断流应返回错误")
	}
	if got := atomic.LoadInt32(&streamed); got == 0 {
		t.Fatal("本用例前提是「已下发过增量」，但一个都没收到 —— 测不到那条不变式")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("上游被请求 %d 次，want 1 —— 已下发增量后不得换模型续写（用户会看到拼接的半句话）", got)
	}
}
