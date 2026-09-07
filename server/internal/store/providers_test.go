package store

import (
	"context"
	"testing"

	_ "modernc.org/sqlite"
)

func insertProvider(t *testing.T, db *DB, apiType, name, keyEnc, defModel string, sort int) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO model_providers (api_type, name, base_url, api_key_enc, default_model, sort_order) VALUES (?, ?, ?, ?, ?, ?)`,
		apiType, name, "https://example.com/"+name, keyEnc, defModel, sort)
	if err != nil {
		t.Fatalf("insert provider %s: %v", name, err)
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

	insertProvider(t, db, "openai-chat", "walt", "enc1", "gpt-4o", 5)
	b := insertProvider(t, db, "anthropic", "ada", "enc2", "", 0)
	insertProvider(t, db, "openai-chat", "molly", "", "", 2)
	if _, err := db.Exec(`INSERT INTO provider_models (provider_id, model_id, temperature, max_tokens) VALUES (?, ?, ?, ?)`, b, "claude-sonnet", 0.7, 4096); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO provider_models (provider_id, model_id) VALUES (?, ?)`, b, "claude-haiku"); err != nil {
		t.Fatal(err)
	}

	list, err := db.ListProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("providers = %d, want 3", len(list))
	}
	// 链序 sort_order, id：ada(0) → molly(2) → walt(5)
	if list[0].Name != "ada" || list[1].Name != "molly" || list[2].Name != "walt" {
		t.Fatalf("chain order = %s,%s,%s", list[0].Name, list[1].Name, list[2].Name)
	}
	// 模型条目随行装配（provider 内部按 id 序）
	if len(list[0].Models) != 2 || list[0].Models[0].ModelID != "claude-sonnet" || list[0].Models[1].ModelID != "claude-haiku" {
		t.Fatalf("ada models = %+v", list[0].Models)
	}
	if !list[0].Models[0].Temperature.Valid || list[0].Models[0].Temperature.Float64 != 0.7 {
		t.Fatalf("temperature mapping broken: %+v", list[0].Models[0].Temperature)
	}
	if list[0].Models[1].Temperature.Valid || list[0].Models[1].MaxTokens.Valid {
		t.Fatal("empty params must stay NULL (调用不传)")
	}
	if len(list[1].Models) != 0 || len(list[2].Models) != 0 {
		t.Fatal("providers without models must have empty Models slice")
	}
}

func TestGetChatProvider(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	// 空库 → ErrNotFound
	if _, err := db.GetChatProvider(ctx); err != ErrNotFound {
		t.Fatalf("empty: err = %v, want ErrNotFound", err)
	}
	// L2 前仅 openai-chat 有适配器：anthropic 独占 → ErrNotFound
	insertProvider(t, db, "anthropic", "ada", "enc2", "claude", 0)
	if _, err := db.GetChatProvider(ctx); err != ErrNotFound {
		t.Fatalf("anthropic-only: err = %v, want ErrNotFound", err)
	}
	// 未配 Key（api_key_enc 空串）不算可用
	insertProvider(t, db, "openai-chat", "nokey", "", "gpt-4o", 1)
	if _, err := db.GetChatProvider(ctx); err != ErrNotFound {
		t.Fatalf("no-key: err = %v, want ErrNotFound", err)
	}
	// 链序靠前的无 Key openai-chat 与非适配协议都跳过，选中链头可用者
	waltID := insertProvider(t, db, "openai-chat", "walt", "enc1", "gpt-4o", 2)
	p, err := db.GetChatProvider(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != waltID || p.DefaultModel != "gpt-4o" {
		t.Fatalf("picked %+v, want walt", p)
	}
}
