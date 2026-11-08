package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

type recyclePageResp struct {
	Items []struct {
		ID        int64  `json:"id"`
		Type      string `json:"type"`
		DeletedAt string `json:"deletedAt"`
	} `json:"items"`
	Total   int  `json:"total"`
	HasMore bool `json:"has_more"`
}

// 回收站列表分页契约：默认 50、上限 200、非法/负数回落默认；has_more = offset+len < total。
func TestRecycleListPagination(t *testing.T) {
	ts := newTestServer(t)
	adminToken, code := loginToken(t, ts.URL, "admin", "admin-pw", "admin")
	if code != http.StatusOK {
		t.Fatalf("admin login: %d", code)
	}

	const seeded = 205
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	delAt := func(i int) string { return base.Add(time.Duration(i) * time.Second).Format("2006-01-02 15:04:05") }
	for i := 0; i < seeded; i++ {
		if _, err := ts.db.Exec(
			`INSERT INTO posts (author_id, content, deleted_at) VALUES (1, ?, ?)`,
			fmt.Sprintf("post-%d", i), delAt(i)); err != nil {
			t.Fatalf("seed post %d: %v", i, err)
		}
	}

	fetch := func(query string) recyclePageResp {
		t.Helper()
		res := doJSON(t, "GET", ts.URL+"/api/v1/admin/recycle"+query, adminToken, nil)
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %q status = %d", query, res.StatusCode)
		}
		var out recyclePageResp
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatalf("decode %q: %v", query, err)
		}
		return out
	}

	// 默认页：50 条，还有更多
	page := fetch("")
	if len(page.Items) != 50 || page.Total != seeded || !page.HasMore {
		t.Fatalf("默认页 = len %d total %d has_more %v，want 50/%d/true", len(page.Items), page.Total, page.HasMore, seeded)
	}
	if page.Items[0].DeletedAt != delAt(seeded-1) {
		t.Fatalf("首条应是最近的删除记录，实得 %s", page.Items[0].DeletedAt)
	}

	// limit 超上限夹到 200；仍不足总数 → has_more 为真
	page = fetch("?limit=9999")
	if len(page.Items) != 200 || !page.HasMore {
		t.Fatalf("limit=9999 = len %d has_more %v，want 200/true", len(page.Items), page.HasMore)
	}

	// 末页边界：offset+len == total → has_more 为假
	page = fetch("?limit=5&offset=200")
	if len(page.Items) != 5 || page.HasMore {
		t.Fatalf("offset=200 = len %d has_more %v，want 5/false", len(page.Items), page.HasMore)
	}
	// offset 越界：空页、has_more 为假
	page = fetch("?limit=5&offset=205")
	if len(page.Items) != 0 || page.HasMore {
		t.Fatalf("offset=205 = len %d has_more %v，want 0/false", len(page.Items), page.HasMore)
	}

	// 非法/负数回落默认（limit=50、offset=0）
	for _, q := range []string{"?limit=abc&offset=xyz", "?limit=-3&offset=-1"} {
		page = fetch(q)
		if len(page.Items) != 50 || page.Total != seeded || !page.HasMore {
			t.Fatalf("%s 回落 = len %d total %d has_more %v，want 50/%d/true", q, len(page.Items), page.Total, page.HasMore, seeded)
		}
	}
}
