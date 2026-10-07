package api

import (
	"fmt"
	"strings"
	"testing"
)

// P2-26：预算触顶那一轮**不再发带工具的模型调用**，但收尾的**不带工具**调用必须照常发
// （AI模式设计「预算与并行 · 收尾义务」：预算耗尽必须再补一次不带工具的调用）。
// 构造「一次响应回 16 个 tool_calls」（正好吃掉全部额度）：
// 修复前 —— 触顶后仍多跑一轮带工具调用，共 2 次模型请求；修复后 —— 只剩 1 次「带工具那轮」+ 1 次收尾。
// 用 newFakeModel 的请求计数断言，并检查第二次请求**不带 tools**。
func TestBudgetHitSkipsToolCallAndStillWrapsUp(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 一次响应回 16 个读工具调用（find_posts），吃满 MaxToolCalls=16
	calls := ""
	for i := 0; i < 16; i++ {
		if i > 0 {
			calls += ","
		}
		calls += fmt.Sprintf(`{"id":"c%d","type":"function","function":{"name":"find_posts","arguments":"{}"}}`, i)
	}
	fake := newFakeModel(t,
		fmt.Sprintf(`{"message":{"content":"","tool_calls":[%s]}}`, calls),
		`{"message":{"content":"这是收尾回答"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	_, body := chatTurn(t, ts, adminToken, 0, "把能查的都查一遍")
	if !strings.Contains(body, "event: turn.end") {
		t.Fatalf("本轮应有 turn.end：\n%s", body)
	}
	if !strings.Contains(body, "这是收尾回答") {
		t.Fatalf("应发出收尾回答（预算耗尽必须补一次不带工具调用）：\n%s", body)
	}

	// 模型请求总数 = 1（带工具那轮）+ 1（收尾）= 2；修复前会多一次带工具调用（3 次）
	fake.mu.Lock()
	n := len(fake.reqs)
	var firstTools, lastTools int
	if n >= 1 {
		// 收尾请求里 tools 是**显式 null**（序列化后仍带该键），故判「有无工具」要看值，
		// 不能只看键是否存在——断言非空数组长度才对。
		firstTools = len(toolArr(fake.reqs[0]["tools"]))
		lastTools = len(toolArr(fake.reqs[n-1]["tools"]))
	}
	fake.mu.Unlock()

	if n != 2 {
		t.Fatalf("模型请求数 = %d，want 2（触顶那轮不再发带工具调用，收尾调用照发）", n)
	}
	if firstTools == 0 {
		t.Fatal("第一轮应带工具（否则本用例没验到触顶场景）")
	}
	if lastTools != 0 {
		t.Fatalf("收尾调用必须不带工具（AI模式设计「预算与并行 · 收尾义务」），实带 %d 个", lastTools)
	}
}

// toolArr tools 字段的数组形态（null / 缺失都当空）。
func toolArr(v any) []any {
	arr, _ := v.([]any)
	return arr
}
