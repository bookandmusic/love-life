package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// P2-5：流式进行中删会话必须被拒（turn 未终结），本轮终结后才可删。
// 未终结就删会把会话行在本轮落库之前抹掉——本轮的内容随即写进一个不存在的会话。
func TestDeleteSessionRejectedWhileTurnActive(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newSSEFakeModel(t, "第一段", "第二段")
	seedFakeProvider(t, ts, fake.server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/v1/chat/message",
		strings.NewReader(`{"content":"这轮还没答完"}`))
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

	// 边读边取 session_id
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var got string
	var sessionID int64
	deadline := time.Now().Add(10 * time.Second)
	for sc.Scan() && time.Now().Before(deadline) {
		line := sc.Text()
		got += line + "\n"
		if strings.HasPrefix(line, "data: ") && strings.Contains(got, "event: session") {
			var ev struct {
				SessionID int64 `json:"session_id"`
			}
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev) == nil && ev.SessionID > 0 {
				sessionID = ev.SessionID
			}
		}
		if sessionID > 0 && strings.Contains(got, "event: turn.start") {
			break
		}
	}
	if sessionID == 0 {
		t.Fatalf("未取到 session_id（SSE：%s）", got)
	}
	select {
	case <-fake.blocked: // 上游仍在挂着，本轮确定未终结
	case <-time.After(10 * time.Second):
		t.Fatal("fake 上游未进入阻塞态")
	}

	dres := doJSON(t, http.MethodDelete, fmt.Sprintf("%s/api/v1/chat/sessions/%d", ts.URL, sessionID), adminToken, nil)
	dres.Body.Close()
	if dres.StatusCode != http.StatusConflict {
		t.Fatalf("流式中删会话 = %d，want 409", dres.StatusCode)
	}

	// 请求 ctx 取消（客户端断开）→ 本轮落库并终结 → 此时才能删
	cancel()
	select {
	case <-fake.downstream:
	case <-time.After(10 * time.Second):
		t.Fatal("上游 ctx 未结束")
	}
	var lastCode int
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until); {
		dres = doJSON(t, http.MethodDelete, fmt.Sprintf("%s/api/v1/chat/sessions/%d", ts.URL, sessionID), adminToken, nil)
		lastCode = dres.StatusCode
		dres.Body.Close()
		if lastCode == http.StatusOK {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if lastCode != http.StatusOK {
		t.Fatalf("本轮终结后删会话 = %d，want 200（登记未随本轮结束清除？）", lastCode)
	}
}
