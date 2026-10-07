package api

// 意外断连的落库守卫（PRODUCT「输入与运行控制」：连接意外断开不删消息、
// **保留已产生内容并标「已中断」**；AI模式设计「停止与断连」与「落库时机」：
// 中途断连不产生半截行，已下发的内容按 parts 完整保留）。
//
// 现成夹具测不到这条路径：newFakeModel 只回 application/json，走 adapter 的
// 「非事件流降级回放」分支，永远返回 nil error；而旧的 TestChatDisconnectMarksInterrupted
// 是直接改库伪造 interrupted part，从不进入服务端的断连分支。
// 本文件自建一个**可流式、可中途阻塞**的 fake 上游，并真正取消请求 context。

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// sseFakeModel 流式假上游：先吐若干个含 content 的 delta 帧（带 Flush），
// 随后阻塞直到上游请求 context 结束（模拟「模型还在吐字时客户端断了」）。
type sseFakeModel struct {
	server     *httptest.Server
	calls      atomic.Int32
	deltas     []string
	blocked    chan struct{} // 进入阻塞态后关闭（测试据此判断可以安全 cancel）
	downstream chan struct{} // 上游 ctx 结束时关闭
}

func newSSEFakeModel(t *testing.T, deltas ...string) *sseFakeModel {
	t.Helper()
	f := &sseFakeModel{deltas: deltas, blocked: make(chan struct{}), downstream: make(chan struct{})}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		for _, d := range f.deltas {
			frame, _ := json.Marshal(map[string]any{
				"choices": []map[string]any{{"delta": map[string]any{"content": d}, "index": 0}},
			})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			if fl != nil {
				fl.Flush()
			}
			time.Sleep(10 * time.Millisecond) // 让下游确实逐帧收到，而不是一次性攒完
		}
		close(f.blocked)
		<-r.Context().Done() // 客户端断开 → 上游 ctx 取消
		close(f.downstream)
	}))
	t.Cleanup(f.server.Close)
	return f
}

// TestChatDisconnectPersistsStreamedContent：流式中途取消请求 →
//   - 已下发的正文按 parts 落成 assistant 行（不是「只剩用户消息」）；
//   - 用户消息带 interrupted 标记；
//   - 不发 error / turn.end（连接已断，写了也送不出去）。
func TestChatDisconnectPersistsStreamedContent(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newSSEFakeModel(t, "海边的", "日落", "真美")
	seedFakeProvider(t, ts, fake.server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/v1/chat/message",
		strings.NewReader(`{"content":"给我讲讲海边"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	// 读到手上有至少一个 answer.delta 后再取消：保证「已产生内容」非空
	sc := bufio.NewScanner(res.Body)
	var got, sid string
	deadline := time.Now().Add(10 * time.Second)
	for sc.Scan() && time.Now().Before(deadline) {
		line := sc.Text()
		got += line + "\n"
		if strings.HasPrefix(line, "data:") && strings.Contains(got, "event: session") {
			var payload map[string]any
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload) == nil {
				if v, ok := payload["session_id"].(float64); ok {
					sid = fmt.Sprintf("%d", int64(v))
				}
			}
		}
		if strings.Contains(got, "event: answer.delta") {
			break
		}
	}
	// 确认上游已进入阻塞态（delta 都吐完了），此刻取消 = 模型还在输出时断线
	select {
	case <-fake.blocked:
	case <-time.After(10 * time.Second):
		t.Fatal("fake 上游未进入阻塞态")
	}
	cancel()
	select {
	case <-fake.downstream:
	case <-time.After(10 * time.Second):
		t.Fatal("上游请求 context 未随客户端断开而取消")
	}

	if sid == "" {
		t.Fatalf("未取到 session（SSE 片段：%s）", got)
	}

	// 落库有等待：断连分支用独立 2s context 写库，轮询等待
	var (
		parts  string
		nAssis int
	)
	for i := 0; i < 60; i++ {
		if err := ts.db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(parts),'') FROM chat_messages
			WHERE session_id = ? AND role = 'assistant'`, sid).Scan(&nAssis, &parts); err != nil {
			t.Fatal(err)
		}
		if nAssis > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if nAssis == 0 {
		t.Fatal("断连后应保留一条 assistant 行（已产生内容不得随函数返回丢弃）")
	}
	if !strings.Contains(parts, "海边的") {
		t.Fatalf("assistant 行未含已下发的正文，parts=%s", parts)
	}

	// 用户消息带 interrupted（标记在 assistant 行之后追加，同样轮询等待）
	var userParts string
	marked := false
	for i := 0; i < 60; i++ {
		if err := ts.db.QueryRow(`SELECT parts FROM chat_messages
			WHERE session_id = ? AND role = 'user' ORDER BY id DESC LIMIT 1`, sid).Scan(&userParts); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(userParts, "interrupted") {
			marked = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !marked {
		t.Fatalf("用户消息应带 interrupted 标记，parts=%s", userParts)
	}

	// 断连分支不发 error / turn.end
	if strings.Contains(got, "event: error") {
		t.Fatalf("断连不应发 error 事件：\n%s", got)
	}
}

// TestChatUserStopRollsBackEvenIfDisconnectBranchHits：用户点「停止」时客户端会
// 先 abort 请求（服务端 r.Context() 随之取消），在服务端与「意外断连」现象相同，
// 但语义相反——停止 = **整轮回滚**。中止登记必须让断连分支不落任何 assistant 行，
// 否则用户明确要求回滚的那轮会留下孤儿行。
//
// 构造真实的并发：流式请求进行中（模型阻塞）→ 调 Abort（置位 + 整轮 DELETE）→
// 再取消请求 ctx（触发服务端断连分支）→ 该轮必须一行不剩。
func TestChatUserStopRollsBackEvenIfDisconnectBranchHits(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newSSEFakeModel(t, "第一段", "第二段")
	seedFakeProvider(t, ts, fake.server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/v1/chat/message",
		strings.NewReader(`{"content":"这轮要被停止"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	// 边读边抓 turn_id；收到 answer.delta 说明内容已下发
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	got, turnID := "", ""
	deadline := time.Now().Add(10 * time.Second)
	for sc.Scan() && time.Now().Before(deadline) {
		line := sc.Text()
		got += line + "\n"
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, "turn_id") {
			var ev struct {
				TurnID string `json:"turn_id"`
			}
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev) == nil && ev.TurnID != "" {
				turnID = ev.TurnID
			}
		}
		if turnID != "" && strings.Contains(got, "event: answer.delta") {
			break
		}
	}
	if turnID == "" {
		t.Fatalf("未取到 turn_id（SSE：%s）", got)
	}
	select {
	case <-fake.blocked:
	case <-time.After(10 * time.Second):
		t.Fatal("fake 上游未进入阻塞态")
	}

	// 用户点「停止」：Abort 置位中止意图 + 整轮回滚（DELETE 全轮行）
	ares := doJSON(t, "POST", ts.URL+"/api/v1/chat/turns/"+turnID+"/abort", adminToken, map[string]any{})
	ares.Body.Close()

	// 随后请求 ctx 取消（客户端 abort() 的效果）→ 服务端进入断连分支
	cancel()
	select {
	case <-fake.downstream:
	case <-time.After(10 * time.Second):
		t.Fatal("上游请求 context 未随客户端断开而取消")
	}

	// 给断连分支足够时间跑完（它应该因中止登记直接返回）
	time.Sleep(1500 * time.Millisecond)

	var n int
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE turn_id = ?`, turnID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("用户停止后该轮必须整轮回滚（一行不剩），实得 %d 行", n)
	}
	var ctxN int
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM chat_context WHERE turn_id = ?`, turnID).Scan(&ctxN); err != nil {
		t.Fatal(err)
	}
	if ctxN != 0 {
		t.Fatalf("用户停止后模型上下文也应整轮回滚，实得 %d 行", ctxN)
	}
}

// TestChatDisconnectKeepsCurrentRoundThinkingInOrder：多轮场景下断连要保住**本轮**思考，
// 且按发生顺序排在更早各轮的步骤之后。
//
// 回归的是两个真实缺陷（首次实现对单轮 content-only 场景通过，故漏掉）：
//  1. 本轮思考被丢弃——断连分支在「本轮 append 思考 part」之前就返回，
//     而当时的去重判断误以为 turnParts 里已有思考，于是整段本轮思考没落库；
//  2. 顺序错——把本轮思考**插到最前**会让 parts 变成「思考 → 更早的工具步骤」，
//     违反 AI模式设计「消息 parts 契约」的 思考 → 工具 → 正文 顺序。
//
// 构造：轮 1 = JSON（无思考，一次 find_posts）→ 落一条 tool part；
// 轮 2 = SSE（吐两段 reasoning 后阻塞）→ 取消请求，命中断连分支。
func TestChatDisconnectKeepsCurrentRoundThinkingInOrder(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	fake := newMultiRoundSSEFakeModel(t,
		`{"choices":[{"message":{"content":"","reasoning_content":"R1-更早的思考","tool_calls":[{"id":"f1","type":"function","function":{"name":"find_posts","arguments":"{\"query\":\"海边\"}"}}]}}]}`,
		[]string{"本轮思考-甲", "本轮思考-乙"},
	)
	seedFakeProvider(t, ts, fake.server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/v1/chat/message",
		strings.NewReader(`{"content":"帮我找找海边的动态"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	// 读到第二段思考说明本轮思考已下发，此时上游即将进入阻塞态
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	got := ""
	for sc.Scan() {
		got += sc.Text() + "\n"
		if strings.Contains(got, "本轮思考-乙") {
			break
		}
	}
	select {
	case <-fake.blocked:
	case <-time.After(10 * time.Second):
		t.Fatal("fake 上游未进入阻塞态")
	}
	cancel()
	select {
	case <-fake.downstream:
	case <-time.After(10 * time.Second):
		t.Fatal("上游请求 context 未随客户端断开而取消")
	}

	// 等断连分支落库
	var parts string
	for i := 0; i < 60; i++ {
		if err := ts.db.QueryRow(`SELECT COALESCE(MAX(parts),'') FROM chat_messages
			WHERE role = 'assistant' AND session_id = (SELECT MAX(session_id) FROM chat_messages)`).Scan(&parts); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(parts, "thinking") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	var list []map[string]any
	if err := json.Unmarshal([]byte(parts), &list); err != nil {
		t.Fatalf("parts 不是 JSON 数组：%v (%s)", err, parts)
	}
	// 缺陷 1：本轮思考必须在
	var thinkingText string
	for _, p := range list {
		if p["type"] == "thinking" {
			thinkingText, _ = p["text"].(string)
		}
	}
	if !strings.Contains(thinkingText, "本轮思考-甲") || !strings.Contains(thinkingText, "本轮思考-乙") {
		t.Fatalf("本轮已下发的思考必须落库，实得 thinking=%q（parts=%s）", thinkingText, parts)
	}
	// 缺陷 2：顺序 = 更早各轮的步骤在前，**本轮**思考在后。
	// 取最后一个 thinking（= 本轮的）与最后一个 tool 比较：把本轮思考插到最前会使其落在 tool 之前。
	lastToolIdx, curThinkIdx := -1, -1
	for i, p := range list {
		switch p["type"] {
		case "tool":
			lastToolIdx = i
		case "thinking":
			curThinkIdx = i
		}
	}
	if lastToolIdx >= 0 && curThinkIdx < lastToolIdx {
		t.Fatalf("顺序错误：本轮思考(index %d) 排在更早工具步骤(index %d) 之前，parts=%s", curThinkIdx, lastToolIdx, parts)
	}
}

// newMultiRoundSSEFakeModel 造一个两轮假上游：第一次调用回 JSON（可带工具调用），
// 第二次起回 SSE（吐完 deltas 后阻塞直到上游 ctx 结束）。
// 用于覆盖「多轮 + 中途断连」——单轮 content-only 的假上游测不到本轮思考丢失。
func newMultiRoundSSEFakeModel(t *testing.T, firstJSON string, laterDeltas []string) *multiRoundFake {
	t.Helper()
	f := &multiRoundFake{firstJSON: firstJSON, deltas: laterDeltas,
		blocked: make(chan struct{}), downstream: make(chan struct{})}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.calls.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(f.firstJSON))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		for _, d := range f.deltas {
			frame, _ := json.Marshal(map[string]any{
				"choices": []map[string]any{{"delta": map[string]any{"reasoning_content": d}, "index": 0}},
			})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			if fl != nil {
				fl.Flush()
			}
			time.Sleep(10 * time.Millisecond)
		}
		close(f.blocked)
		<-r.Context().Done()
		close(f.downstream)
	}))
	t.Cleanup(f.server.Close)
	return f
}

type multiRoundFake struct {
	server     *httptest.Server
	calls      atomic.Int32
	firstJSON  string
	deltas     []string
	blocked    chan struct{}
	downstream chan struct{}
}

// TestChatDisconnectDuringBudgetWrapUpPersists：预算收尾是循环之外的**第二个**断连出口。
// 若收尾调用途中断连，也必须把已累积的工具步骤落成 assistant 行并给用户消息标中断——
// 否则写库因请求 ctx 已取消而静默失败，本轮工具步骤全丢且没有任何中断标记。
//
// 构造：轮 1 回一个含 20 个 tool_call 的响应（超过上限 16 → 执行前截断、budgetHit 置位、
// 16 个工具执行完即 break）→ 轮 2 即收尾调用，阻塞直到客户端断开。
func TestChatDisconnectDuringBudgetWrapUpPersists(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newBudgetWrapFakeModel(t)
	seedFakeProvider(t, ts, fake.server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/v1/chat/message",
		strings.NewReader(`{"content":"把所有动态都翻一遍"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	// 读到 tool.end 说明工具已在执行；等收尾调用进入阻塞态再断开
	go func() { _, _ = io.Copy(io.Discard, res.Body) }()
	select {
	case <-fake.wrapUpStarted:
	case <-time.After(20 * time.Second):
		t.Fatal("未进入预算收尾调用")
	}
	cancel()
	select {
	case <-fake.downstream:
	case <-time.After(10 * time.Second):
		t.Fatal("上游收尾请求未随客户端断开而取消")
	}

	var sid int64
	if err := ts.db.QueryRow(`SELECT MAX(session_id) FROM chat_messages`).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	var parts string
	for i := 0; i < 80; i++ {
		_ = ts.db.QueryRow(`SELECT COALESCE(MAX(parts),'') FROM chat_messages
			WHERE session_id = ? AND role = 'assistant'`, sid).Scan(&parts)
		if strings.Contains(parts, "tool") || strings.Contains(parts, "interrupted") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	// 用户消息必须带中断标记（此前该出口永远不标）
	var userParts string
	if err := ts.db.QueryRow(`SELECT parts FROM chat_messages
		WHERE session_id = ? AND role = 'user' ORDER BY id DESC LIMIT 1`, sid).Scan(&userParts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(userParts, "interrupted") {
		t.Fatalf("预算收尾断连也必须标 interrupted，user parts=%s", userParts)
	}
}

// newBudgetWrapFakeModel：轮 1 回 20 个 tool_call（触发预算截断），轮 2（收尾调用）阻塞到断开。
type budgetWrapFake struct {
	server         *httptest.Server
	wrapUpStarted  chan struct{}
	downstream     chan struct{}
	downstreamOnce sync.Once
	calls          atomic.Int32
}

func newBudgetWrapFakeModel(t *testing.T) *budgetWrapFake {
	t.Helper()
	f := &budgetWrapFake{wrapUpStarted: make(chan struct{}), downstream: make(chan struct{})}
	var calls strings.Builder
	for i := 0; i < 20; i++ {
		if i > 0 {
			calls.WriteString(",")
		}
		fmt.Fprintf(&calls, `{"id":"c%02d","type":"function","function":{"name":"find_posts","arguments":"{\"query\":\"x\"}"}}`, i)
	}
	first := `{"choices":[{"message":{"content":"","tool_calls":[` + calls.String() + `]}}]}`
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 必须把请求体读完：net/http 只有在请求体读尽后才启动「后台读」来探测
		// 客户端断开，否则 r.Context() 不会随断开取消，本用例会挂住。
		_, _ = io.Copy(io.Discard, r.Body)
		if f.calls.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(first))
			return
		}
		close(f.wrapUpStarted)
		<-r.Context().Done() // 收尾调用阻塞到断开；不写任何响应体
		f.downstreamOnce.Do(func() { close(f.downstream) })
	}))
	t.Cleanup(f.server.Close)
	return f
}
