package api

import (
	"encoding/json"
	"github.com/bookandmusic/love-life/server/internal/chat"
	"net/http"
	"strings"
	"testing"
)

func TestStylesCRUD(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnv(t)

	// girl 不可访问 admin 列表（403），但 APP 端列表可达且形状一致
	res := doJSON(t, "GET", ts.URL+"/api/v1/admin/styles", girlToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("girl admin styles: want 403, got %d", res.StatusCode)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/styles", girlToken, nil)
	var appList struct {
		Items         []map[string]any `json:"items"`
		DefaultStyleI int64            `json:"default_style_id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&appList)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || len(appList.Items) != 4 || appList.DefaultStyleI != 1 {
		t.Fatalf("app styles: %d items=%d default=%d", res.StatusCode, len(appList.Items), appList.DefaultStyleI)
	}
	if appList.Items[0]["name"] != "温暖" || appList.Items[0]["builtin"] != true {
		t.Fatalf("seed builtin missing: %+v", appList.Items[0])
	}

	// 创建自定义风格 → 201 builtin=false；名称重复 → 400
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/styles", adminToken, map[string]any{"name": "诗意", "prompt": "像写诗一样说话。"})
	var created map[string]any
	_ = json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated || created["builtin"] != false {
		t.Fatalf("create: %d %+v", res.StatusCode, created)
	}
	id := int64(created["id"].(float64))
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/styles", adminToken, map[string]any{"name": "诗意", "prompt": "x"})
	var dup map[string]any
	_ = json.NewDecoder(res.Body).Decode(&dup)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || dup["error"] != "风格名称已存在" {
		t.Fatalf("dup name: %d %+v", res.StatusCode, dup)
	}

	// 校验矩阵
	for _, tc := range []struct {
		name string
		body map[string]any
		want string
	}{
		{"缺 name", map[string]any{"prompt": "x"}, "name 必填"},
		{"缺 prompt", map[string]any{"name": "x"}, "prompt 必填"},
	} {
		res := doJSON(t, "POST", ts.URL+"/api/v1/admin/styles", adminToken, tc.body)
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest || !strings.Contains(out["error"].(string), tc.want) {
			t.Fatalf("%s: %d %+v", tc.name, res.StatusCode, out)
		}
	}

	// 编辑自定义风格；编辑内置 → 400
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/styles/"+itoa(id), adminToken, map[string]any{"prompt": "像写散文一样。"})
	var updated map[string]any
	_ = json.NewDecoder(res.Body).Decode(&updated)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || updated["prompt"] != "像写散文一样。" {
		t.Fatalf("update: %d %+v", res.StatusCode, updated)
	}
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/styles/1", adminToken, map[string]any{"name": "改名"})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("update builtin: %d", res.StatusCode)
	}

	// 设默认 → 列表 default_style_id 跟随；默认风格删除被拒；切回后可删
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/styles/default", adminToken, map[string]any{"id": id})
	var afterDefault struct {
		Items         []map[string]any `json:"items"`
		DefaultStyleI int64            `json:"default_style_id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&afterDefault)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || afterDefault.DefaultStyleI != id {
		t.Fatalf("set default: %d %d", res.StatusCode, afterDefault.DefaultStyleI)
	}
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/admin/styles/"+itoa(id), adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("delete default: %d", res.StatusCode)
	}
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/styles/default", adminToken, map[string]any{"id": 1})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("restore default: %d", res.StatusCode)
	}
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/admin/styles/"+itoa(id), adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete custom: %d", res.StatusCode)
	}
	// 再删 → 404；删内置 → 400
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/admin/styles/"+itoa(id), adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("delete twice: %d", res.StatusCode)
	}
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/admin/styles/2", adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("delete builtin: %d", res.StatusCode)
	}
}

func TestSystemPromptAPI(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 未配置 → fallback 主干 + 空 updatedAt
	res := doJSON(t, "GET", ts.URL+"/api/v1/admin/system-prompt", adminToken, nil)
	var got map[string]any
	_ = json.NewDecoder(res.Body).Decode(&got)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || got["content"] != chat.SystemPromptTrunkFallback {
		t.Fatalf("get default: %d %+v", res.StatusCode, got)
	}

	// PUT 保存 → GET 返回保存值与 updatedAt
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/system-prompt", adminToken, map[string]any{"content": "自定义主干内容"})
	_ = json.NewDecoder(res.Body).Decode(&got)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || got["content"] != "自定义主干内容" || got["updatedAt"] == "" {
		t.Fatalf("put: %d %+v", res.StatusCode, got)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/system-prompt", adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&got)
	res.Body.Close()
	if got["content"] != "自定义主干内容" || got["updatedAt"] == "" {
		t.Fatalf("get after put: %+v", got)
	}

	// 校验：content 空/缺失 → 400「content 必填」
	for _, body := range []map[string]any{{}, {"content": "   "}} {
		res := doJSON(t, "PUT", ts.URL+"/api/v1/admin/system-prompt", adminToken, body)
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest || out["error"] != "content 必填" {
			t.Fatalf("put empty: %d %+v", res.StatusCode, out)
		}
	}
}
