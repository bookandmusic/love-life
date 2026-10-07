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
	res, err := db.Exec(`INSERT INTO provider_models (provider_id, kind, model_id, temperature, max_tokens, chain_order) VALUES (?, ?, ?, ?, ?, ?)`,
		providerID, kind, modelID, 0.7, 4096, order)
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
	if !list[1].Models[0].Temperature.Valid || list[1].Models[0].Temperature.Float64 != 0.7 {
		t.Fatalf("temperature mapping broken: %+v", list[1].Models[0].Temperature)
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
