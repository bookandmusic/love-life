package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/tools"
)

func newEnv(t *testing.T) (*store.DB, *Service, int64, int64) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := t.Context()
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash, role, nickname) VALUES (1,'admin','x','admin','Admin')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash, role, nickname) VALUES (2,'girl','x','girl','Girl')`); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, &tools.Deps{DB: db})
	_, _, err = db.CreatePAT(ctx, "t", 2, "readonly")
	if err != nil {
		t.Fatal(err)
	}
	return db, svc, 1, 2
}

func call(t *testing.T, svc *Service, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", bytes.NewBufferString(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	svc.Handle(rec, req)
	return rec
}

// 未带/错误 PAT → 401；admin 不可签发 PAT。
func TestMCPAuthAndPATRules(t *testing.T) {
	db, svc, adminID, _ := newEnv(t)
	ctx := t.Context()

	if rec := call(t, svc, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: want 401, got %d", rec.Code)
	}
	if rec := call(t, svc, "llp_deadbeef", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token: want 401, got %d", rec.Code)
	}
	// admin 不签发 PAT（铁律）
	if _, _, err := db.CreatePAT(ctx, "x", adminID, "readonly"); err == nil {
		t.Fatal("admin must not be able to hold a PAT")
	}
}

// 档位过滤：readonly 只见读工具；删除类（AppOnly）对任何档位都不开放。
func TestMCPTierFiltering(t *testing.T) {
	db, svc, _, _ := newEnv(t)
	ctx := t.Context()

	ro, _, err := db.CreatePAT(ctx, "ro", 2, "readonly")
	if err != nil {
		t.Fatal(err)
	}
	rw, _, err := db.CreatePAT(ctx, "rw2", 2, "readwrite")
	if err != nil {
		t.Fatal(err)
	}

	list := func(tok string) []map[string]any {
		rec := call(t, svc, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("list status %d", rec.Code)
		}
		var resp struct {
			Result struct {
				Tools []map[string]any `json:"tools"`
			} `json:"result"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp.Result.Tools
	}

	roTools, rwTools := list(ro), list(rw)
	// 16 工具分档：读 7（两通道均开放）、提案 8（readwrite 档直接执行）、删除 1（AppOnly 永不出现）
	if len(roTools) != 7 {
		t.Fatalf("readonly tools = %d, want 7", len(roTools))
	}
	if len(rwTools) != 15 {
		t.Fatalf("readwrite tools = %d, want 15（读 7 + 提案 8）", len(rwTools))
	}
	for _, tools := range [][]map[string]any{roTools, rwTools} {
		for _, tl := range tools {
			if tl["name"] == "propose_delete" {
				t.Fatalf("删除类工具 %v 不得对任何令牌开放", tl["name"])
			}
		}
	}
	// readonly 不得含提案类工具
	for _, tl := range roTools {
		if strings.HasPrefix(tl["name"].(string), "propose_") {
			t.Fatalf("readonly must not expose propose tools: %v", tl["name"])
		}
	}
}

// 越权调用：readonly 令牌显式调用写工具 → 拒绝（不能只靠清单隐藏）。
func TestMCPWriteToolBlockedForReadonly(t *testing.T) {
	db, svc, _, _ := newEnv(t)
	ro, _, err := db.CreatePAT(t.Context(), "ro2", 2, "readonly")
	if err != nil {
		t.Fatal(err)
	}
	rec := call(t, svc, ro, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create_post","arguments":{"content":"x"}}}`)
	var resp struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error == nil {
		t.Fatalf("readonly calling write tool must be rejected: %s", rec.Body.String())
	}
}

// 后台停用的工具对外部通道同样不可见/不可调用（一致性）。
func TestMCPDisabledToolHidden(t *testing.T) {
	db, svc, _, _ := newEnv(t)
	ctx := t.Context()
	ro, _, err := db.CreatePAT(ctx, "ro3", 2, "readonly")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetToolEnabled(ctx, "search_posts", false); err != nil {
		t.Fatal(err)
	}
	rec := call(t, svc, ro, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	if bytes.Contains(rec.Body.Bytes(), []byte(`"search_posts"`)) {
		t.Fatal("disabled tool must not be listed")
	}
	rec = call(t, svc, ro, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"search_posts","arguments":{"query":"x"}}}`)
	var resp struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error == nil {
		t.Fatalf("disabled tool call must be rejected: %s", rec.Body.String())
	}
}

// initialize 返回协议版本与 tools 能力。
func TestMCPInitialize(t *testing.T) {
	db, svc, _, _ := newEnv(t)
	ro, _, err := db.CreatePAT(t.Context(), "ro4", 2, "readonly")
	if err != nil {
		t.Fatal(err)
	}
	rec := call(t, svc, ro, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("protocolVersion")) {
		t.Fatalf("initialize: %d %s", rec.Code, rec.Body.String())
	}
}

// 一致性守卫：本包的档位过滤（allowed）与 tools.RegistryForPAT 是同一规则的
// 两处实现，必须始终一致——否则一条通道收紧、另一条放宽，删除类工具就可能从
// 宽松的一侧泄露出去。此测试把「不许漂移」钉死。
func TestTierRuleMatchesRegistryForPAT(t *testing.T) {
	patVisible := map[string]bool{}
	for _, tl := range tools.RegistryForPAT() {
		patVisible[tl.Name] = true
	}
	for _, tl := range tools.Registry() {
		// readwrite 档的可见集合必须与 RegistryForPAT 完全一致（两者都排除 AppOnly）
		if got, want := allowed(tl, "readwrite"), patVisible[tl.Name]; got != want {
			t.Errorf("工具 %s: allowed(readwrite)=%v 但 RegistryForPAT=%v —— 两处规则已漂移", tl.Name, got, want)
		}
		// 删除类工具对任何档位都不可见
		if tl.AppOnly && (allowed(tl, "readonly") || allowed(tl, "readwrite")) {
			t.Errorf("删除类工具 %s 不得对任何档位开放", tl.Name)
		}
	}
}

// 工具注解守卫（MCP 2025-06-18）：每个工具的 annotations 必须按 IsWrite/AppOnly
// 三分类正确派生，且 openWorldHint 全为 false（本服务全部操作本地封闭数据集）。
// 此测试钉死派生规则，防止注册表演进时注解口径漂移。
func TestToolAnnotations(t *testing.T) {
	for _, tl := range tools.Registry() {
		a := toolAnnotations(tl)
		// openWorldHint 对所有工具固定 false
		if got, _ := a["openWorldHint"].(bool); got {
			t.Errorf("工具 %s: openWorldHint 必须为 false", tl.Name)
		}
		switch {
		case !tl.IsWrite:
			// 读工具：readOnlyHint=true、destructiveHint=false
			if got, _ := a["readOnlyHint"].(bool); !got {
				t.Errorf("读工具 %s: readOnlyHint 必须为 true", tl.Name)
			}
			if got, _ := a["destructiveHint"].(bool); got {
				t.Errorf("读工具 %s: destructiveHint 必须为 false", tl.Name)
			}
		case tl.AppOnly:
			// 删除类：destructiveHint=true（与默认一致，显式标注）
			if got, _ := a["destructiveHint"].(bool); !got {
				t.Errorf("删除类工具 %s: destructiveHint 必须为 true", tl.Name)
			}
			if got, _ := a["readOnlyHint"].(bool); got {
				t.Errorf("删除类工具 %s: readOnlyHint 必须为 false", tl.Name)
			}
		default:
			// 写工具（创建/可撤销）：destructiveHint=false、readOnlyHint=false
			if got, _ := a["destructiveHint"].(bool); got {
				t.Errorf("写工具 %s: destructiveHint 必须为 false（非删除类）", tl.Name)
			}
			if got, _ := a["readOnlyHint"].(bool); got {
				t.Errorf("写工具 %s: readOnlyHint 必须为 false", tl.Name)
			}
		}
	}
}

// tools/list 响应必须带上 annotations 字段（派生取值由 TestToolAnnotations 钉死，
// 此处只做集成层断言：字段存在 + openWorldHint 全为 false）。
func TestMCPListIncludesAnnotations(t *testing.T) {
	db, svc, _, _ := newEnv(t)
	ctx := t.Context()
	rw, _, err := db.CreatePAT(ctx, "rw_ann", 2, "readwrite")
	if err != nil {
		t.Fatal(err)
	}
	rec := call(t, svc, rw, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status %d", rec.Code)
	}
	var resp struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Result.Tools) == 0 {
		t.Fatal("no tools listed")
	}
	for _, tl := range resp.Result.Tools {
		name, _ := tl["name"].(string)
		ann, ok := tl["annotations"].(map[string]any)
		if !ok {
			t.Errorf("工具 %s 缺少 annotations 字段", name)
			continue
		}
		if v, _ := ann["openWorldHint"].(bool); v {
			t.Errorf("工具 %s openWorldHint 必须为 false", name)
		}
	}
}
