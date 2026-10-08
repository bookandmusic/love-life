package store

import (
	"context"
	"testing"

	_ "modernc.org/sqlite"
)

func insertProvider(t *testing.T, db *DB, apiType, name, keyEnc string) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO model_providers (api_type, name, base_url, api_key_enc) VALUES (?, ?, ?, ?)`,
		apiType, name, "https://example.com/"+name, keyEnc)
	if err != nil {
		t.Fatalf("insert provider %s: %v", name, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func insertModel(t *testing.T, db *DB, providerID int64, kind, modelID string, order int) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO provider_models (provider_id, kind, model_id, max_tokens, thinking, thinking_levels, context_window, chain_order) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		providerID, kind, modelID, 4096, "medium", "off,low,medium,high", 200000, order)
	if err != nil {
		t.Fatalf("insert model %s: %v", modelID, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestListProviders(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	walt := insertProvider(t, db, "openai-chat", "walt", "enc1")
	ada := insertProvider(t, db, "anthropic", "ada", "enc2")
	insertProvider(t, db, "openai-chat", "molly", "")
	insertModel(t, db, ada, "chat", "claude-sonnet", 1)
	insertModel(t, db, ada, "chat", "claude-haiku", 0)
	insertModel(t, db, ada, "embedding", "bge-m3", 0)

	list, err := db.ListProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("providers = %d, want 3", len(list))
	}
	// 列表按创建序（链序只属于模型条目，不再挂在供应商上）
	if list[0].Name != "walt" || list[1].Name != "ada" || list[2].Name != "molly" {
		t.Fatalf("provider order = %s,%s,%s", list[0].Name, list[1].Name, list[2].Name)
	}
	// 模型条目随行装配：kind、链序分组成员化（同 kind 内按 chain_order, id）
	if len(list[1].Models) != 3 {
		t.Fatalf("ada models = %+v", list[1].Models)
	}
	if list[1].Models[0].Kind != "chat" || list[1].Models[0].ModelID != "claude-haiku" {
		t.Fatalf("first chat entry = %+v", list[1].Models[0])
	}
	if list[1].Models[2].Kind != "embedding" {
		t.Fatalf("last entry = %+v", list[1].Models[2])
	}
	if !list[1].Models[0].MaxTokens.Valid || list[1].Models[0].MaxTokens.Int64 != 4096 {
		t.Fatalf("max_tokens mapping broken: %+v", list[1].Models[0].MaxTokens)
	}
	if list[1].Models[0].Thinking != ThinkingMedium || list[1].Models[0].ThinkingLevels != "off,low,medium,high" {
		t.Fatalf("thinking mapping broken: %+v", list[1].Models[0])
	}
	if !list[1].Models[0].ContextWindow.Valid || list[1].Models[0].ContextWindow.Int64 != 200000 {
		t.Fatalf("context_window mapping broken: %+v", list[1].Models[0].ContextWindow)
	}
	if len(list[2].Models) != 0 {
		t.Fatal("providers without models must have empty Models slice")
	}
	_ = walt
}

func TestListModelsChains(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	// chat 链：跨供应商按 chain_order 排；未配 Key 的供应商不入 chat 链
	nokey := insertProvider(t, db, "openai-chat", "nokey", "")
	ada := insertProvider(t, db, "anthropic", "ada", "enc2")
	walt := insertProvider(t, db, "openai-chat", "walt", "enc1")
	insertModel(t, db, nokey, "chat", "gpt-hidden", -1)
	insertModel(t, db, walt, "chat", "gpt-4o", 0)
	insertModel(t, db, ada, "chat", "claude", 1)

	chat, err := db.ListModels(ctx, "chat")
	if err != nil {
		t.Fatal(err)
	}
	if len(chat) != 2 {
		t.Fatalf("chat chain = %d, want 2（未配 Key 的 nokey 不入链）", len(chat))
	}
	if chat[0].Model.ModelID != "gpt-4o" || chat[0].Provider.ID != walt {
		t.Fatalf("chat[0] = %+v", chat[0])
	}
	if chat[1].Model.ModelID != "claude" || chat[1].Provider.APIType != "anthropic" {
		t.Fatalf("chat[1] = %+v", chat[1])
	}

	// 嵌入链：仅 openai-chat 供应商
	insertModel(t, db, walt, "embedding", "bge-m3", 0)
	insertModel(t, db, ada, "embedding", "claude-embed", 0)
	emb, err := db.ListModels(ctx, "embedding")
	if err != nil {
		t.Fatal(err)
	}
	if len(emb) != 1 || emb[0].Model.ModelID != "bge-m3" || emb[0].Provider.ID != walt {
		t.Fatalf("embedding chain = %+v, want only walt/bge-m3（anthropic 不入嵌入链）", emb)
	}

	// 链头
	head, err := db.GetChatHead(ctx)
	if err != nil || head.Model.ModelID != "gpt-4o" {
		t.Fatalf("head = %+v err=%v", head, err)
	}
	// 空库新连接：删除全部条目后 ErrNotFound
	if _, err := db.Exec(`DELETE FROM provider_models`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetChatHead(ctx); err != ErrNotFound {
		t.Fatalf("empty chain: err = %v, want ErrNotFound", err)
	}
}

func TestReorderModels(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	p := insertProvider(t, db, "openai-chat", "walt", "enc1")
	m1 := insertModel(t, db, p, "chat", "a", 0)
	m2 := insertModel(t, db, p, "chat", "b", 1)
	m3 := insertModel(t, db, p, "chat", "c", 2)
	// 反转链序
	if err := db.ReorderModels(ctx, "chat", []int64{m3, m1, m2}); err != nil {
		t.Fatal(err)
	}
	chain, err := db.ListModels(ctx, "chat")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{chain[0].Model.ModelID, chain[1].Model.ModelID, chain[2].Model.ModelID}
	want := []string{"c", "a", "b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chain = %v, want %v", got, want)
		}
	}
}

// 思考档位的写入侧归一：update 路径传空串（客户端「未传」的落值）必须落成 off，
// 不允许库里出现 ” 这第三种表示（列默认值是 'off'，两者必须一致）。
func TestNormalizeThinkingFallsBackToOff(t *testing.T) {
	cases := map[string]string{
		"":         ThinkingOff, // 未传 / 空串
		"off":      ThinkingOff,
		"low":      ThinkingLow,
		"medium":   ThinkingMedium,
		"high":     ThinkingHigh,
		"HIGH":     ThinkingOff, // 大小写不宽容：取值面收敛，避免同一档两种写法
		"ultra":    ThinkingOff, // 非法值在 API 层已拒；到此处一律兜 off，绝不写脏值
		" minimal": ThinkingOff,
	}
	for in, want := range cases {
		if got := normalizeThinking(in); got != want {
			t.Errorf("normalizeThinking(%q) = %q, want %q", in, got, want)
		}
	}
}

// 档位集合的写入侧归一：去重、按 off/low/medium/high 固定顺序、恒含 off。
// 集合是「该模型支持哪些档位」的声明，顺序必须稳定——否则库里同一集合会有多种写法。
func TestNormalizeThinkingLevels(t *testing.T) {
	cases := map[string]string{
		"":                    ThinkingOff, // 未传 → 只有关
		"off":                 ThinkingOff,
		"medium,off,low":      "off,low,medium", // 乱序输入按固定顺序落地
		"high,high,low":       "off,low,high",   // 去重，且自动补 off
		"low, medium , high":  "off,low,medium,high",
		"off,low,medium,high": "off,low,medium,high",
	}
	for in, want := range cases {
		got, ok := NormalizeThinkingLevels(in)
		if !ok || got != want {
			t.Errorf("NormalizeThinkingLevels(%q) = %q,%v want %q,true", in, got, ok, want)
		}
	}

	// 非法取值必须显式报错：静默兜 off 会把「不支持思考的模型」伪装成「用户选了关」
	for _, bad := range []string{"ultra", "off,ultra", "HIGH"} {
		if got, ok := NormalizeThinkingLevels(bad); ok {
			t.Errorf("NormalizeThinkingLevels(%q) = %q, want !ok", bad, got)
		}
	}
}

// 档位必须在声明集内：声明集是发给供应商的取值面，含不在集合里的档位会被拒或静默降级。
func TestThinkingLevelAllowed(t *testing.T) {
	cases := []struct {
		levels, level string
		want          bool
	}{
		{"off", ThinkingOff, true},
		{"off", ThinkingLow, false},
		{"off,low,high", ThinkingLow, true},
		{"off,low,high", ThinkingMedium, false}, // Gemini 3 Pro 没有「中」
		{"off,low,high", ThinkingHigh, true},
	}
	for _, c := range cases {
		if got := ThinkingLevelAllowed(c.levels, c.level); got != c.want {
			t.Errorf("ThinkingLevelAllowed(%q,%q) = %v, want %v", c.levels, c.level, got, c.want)
		}
	}
}
