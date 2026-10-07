package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// 工具清单形状 + 启停 + 契约错误文案
func TestMcpToolsListAndPatch(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnv(t)

	res := doJSON(t, "GET", ts.URL+"/api/v1/admin/mcp-tools", girlToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("girl: want 403, got %d", res.StatusCode)
	}

	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/mcp-tools", adminToken, nil)
	var list []map[string]any
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || len(list) != 16 {
		t.Fatalf("list: %d len=%d", res.StatusCode, len(list))
	}
	first := list[0]
	for _, k := range []string{"name", "description", "category", "enabled", "schema"} {
		if _, ok := first[k]; !ok {
			t.Fatalf("missing key %s: %+v", k, first)
		}
	}
	if first["enabled"] != true {
		t.Fatalf("default should be enabled: %+v", first)
	}

	// 停用
	res = doJSON(t, "PATCH", ts.URL+"/api/v1/admin/mcp-tools/find_posts", adminToken, map[string]any{"enabled": false})
	var patched map[string]any
	_ = json.NewDecoder(res.Body).Decode(&patched)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || patched["enabled"] != false || patched["name"] != "find_posts" {
		t.Fatalf("patch: %d %+v", res.StatusCode, patched)
	}

	// 列表反映停用
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/mcp-tools", adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	for _, it := range list {
		if it["name"] == "find_posts" && it["enabled"] != false {
			t.Fatalf("disable not persisted: %+v", it)
		}
	}

	// 缺 enabled → 400 契约文案
	res = doJSON(t, "PATCH", ts.URL+"/api/v1/admin/mcp-tools/find_posts", adminToken, map[string]any{})
	var e map[string]any
	_ = json.NewDecoder(res.Body).Decode(&e)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || e["error"] != "enabled 必填（boolean）" {
		t.Fatalf("missing enabled: %d %+v", res.StatusCode, e)
	}

	// 不存在 → 404 契约文案
	res = doJSON(t, "PATCH", ts.URL+"/api/v1/admin/mcp-tools/nope_tool", adminToken, map[string]any{"enabled": true})
	_ = json.NewDecoder(res.Body).Decode(&e)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound || e["error"] != "工具不存在" {
		t.Fatalf("missing tool: %d %+v", res.StatusCode, e)
	}
}
