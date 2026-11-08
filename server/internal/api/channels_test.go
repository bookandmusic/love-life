package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// 统一选路接口（GET /api/v1/channels）：
// APP 只做「按权重降序逐个探活取第一个通的」，所以顺序与权重形状是契约。

type channelsResp struct {
	Items []struct {
		Kind      string `json:"kind"`
		URL       string `json:"url"`
		Name      string `json:"name"`
		Weight    int    `json:"weight"`
		Cookie    string `json:"cookie"`
		ExpiresAt string `json:"expires_at"`
	} `json:"items"`
	BuiltinWeight int `json:"builtin_weight"`
}

func addBackup(t *testing.T, ts *testEnv, adminToken, name, url string, weight *int, enabled bool) {
	t.Helper()
	payload := map[string]any{"name": name, "url": url, "enabled": enabled}
	if weight != nil {
		payload["weight"] = *weight
	}
	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/backup-addresses", adminToken, payload)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("前置：新增备用地址应 200，实得 %d", res.StatusCode)
	}
}

func getChannels(t *testing.T, ts *testEnv, token string) channelsResp {
	t.Helper()
	res := doJSON(t, "GET", ts.URL+"/api/v1/channels", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("选路接口应 200，实得 %d", res.StatusCode)
	}
	var out channelsResp
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	return out
}

// 权重降序是契约本身：APP 拿到就按这个顺序探活，顺序错了优先级就失效。
func TestChannelsSortedByWeightDesc(t *testing.T) {
	ts, _, _ := newPostTestEnv(t)
	adminToken, _ := loginToken(t, ts.URL, "admin", "admin-pw", "admin")
	girlToken, _ := loginToken(t, ts.URL, "girl", "girl-pw", "app")

	low, high := 10, 80
	addBackup(t, ts, adminToken, "低", "https://low.example.com", &low, true)
	addBackup(t, ts, adminToken, "高", "https://high.example.com", &high, true)

	got := getChannels(t, ts, girlToken)
	if len(got.Items) != 2 {
		t.Fatalf("应返回 2 条通道，实得 %d", len(got.Items))
	}
	if got.Items[0].URL != "https://high.example.com" {
		t.Fatalf("第一条应是权重高的，实得 %s", got.Items[0].URL)
	}
	if got.Items[1].URL != "https://low.example.com" {
		t.Fatalf("第二条应是权重低的，实得 %s", got.Items[1].URL)
	}
	if got.BuiltinWeight != channelWeightBuiltin {
		t.Fatalf("内置权重应下发 %d，实得 %d", channelWeightBuiltin, got.BuiltinWeight)
	}
}

// 停用的地址不参与选路；未给权重的按默认值。
func TestChannelsSkipDisabledAndDefaultWeight(t *testing.T) {
	ts, _, _ := newPostTestEnv(t)
	adminToken, _ := loginToken(t, ts.URL, "admin", "admin-pw", "admin")
	girlToken, _ := loginToken(t, ts.URL, "girl", "girl-pw", "app")

	addBackup(t, ts, adminToken, "停用", "https://off.example.com", nil, false)
	addBackup(t, ts, adminToken, "启用", "https://on.example.com", nil, true)

	got := getChannels(t, ts, girlToken)
	if len(got.Items) != 1 {
		t.Fatalf("停用地址不应出现，实得 %d 条", len(got.Items))
	}
	if got.Items[0].Weight != defaultChannelWeight {
		t.Fatalf("未给权重应回落 %d，实得 %d", defaultChannelWeight, got.Items[0].Weight)
	}
	// 非绿联通道不带 cookie：APP 只在 kind == uglink 时发 Cookie 头
	if got.Items[0].Kind != "backup" || got.Items[0].Cookie != "" {
		t.Fatalf("备用通道不应带 cookie：%+v", got.Items[0])
	}
}

// 无任何后端通道是正常状态（APP 回落内置地址），不是错误。
func TestChannelsEmptyIsOK(t *testing.T) {
	ts, _, _ := newPostTestEnv(t)
	girlToken, _ := loginToken(t, ts.URL, "girl", "girl-pw", "app")

	got := getChannels(t, ts, girlToken)
	if len(got.Items) != 0 {
		t.Fatalf("无配置时应返回空数组，实得 %d 条", len(got.Items))
	}
	if got.BuiltinWeight != channelWeightBuiltin {
		t.Fatalf("空状态下仍要下发内置权重")
	}
}

func TestNormalizeWeight(t *testing.T) {
	cases := map[int]int{
		0:    defaultChannelWeight, // 无 weight 字段
		-5:   defaultChannelWeight, // 脏值
		1:    1,
		50:   50,
		100:  100,
		9999: maxChannelWeight, // 越界夹住
	}
	for in, want := range cases {
		if got := normalizeWeight(in); got != want {
			t.Fatalf("normalizeWeight(%d) = %d，期望 %d", in, got, want)
		}
	}
}

// 权重范围与保留值：**唯一性不在这里管**——用户裁定「添加时就该在前端拦住重复」，
// 服务端只拒越界与占用内置值，避免把界面该做的事搬到接口层。
func TestWeightRangeEnforced(t *testing.T) {
	ts, _, _ := newPostTestEnv(t)
	adminToken, _ := loginToken(t, ts.URL, "admin", "admin-pw", "admin")

	// 100 是内置的 → 400
	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/backup-addresses", adminToken, map[string]any{
		"name": "甲", "url": "https://a.example.com", "enabled": true, "weight": 100,
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("占用内置权重应 400，实得 %d", res.StatusCode)
	}

	// 越界（0 / 1000）→ 400
	for _, w := range []int{0, 1000} {
		res = doJSON(t, "POST", ts.URL+"/api/v1/admin/backup-addresses", adminToken, map[string]any{
			"name": "乙", "url": "https://b.example.com", "enabled": true, "weight": w,
		})
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("权重 %d 越界应 400，实得 %d", w, res.StatusCode)
		}
	}

	// 1 与 999 在域内 → 通过
	w1, w999 := 1, 999
	addBackup(t, ts, adminToken, "下界", "https://low.example.com", &w1, true)
	addBackup(t, ts, adminToken, "上界", "https://high.example.com", &w999, true)

	// 大于内置的 200 也通过（1–999 全区间可用）
	w200 := 200
	addBackup(t, ts, adminToken, "优先于内置", "https://over.example.com", &w200, true)
}

// 上界是 999：500 原样保留，1000 夹到上界。
func TestWeightUpperBoundIs999(t *testing.T) {
	if got := normalizeWeight(500); got != 500 {
		t.Fatalf("500 应原样保留，实得 %d", got)
	}
	if got := normalizeWeight(1000); got != maxChannelWeight {
		t.Fatalf("1000 应夹到 %d，实得 %d", maxChannelWeight, got)
	}
}
