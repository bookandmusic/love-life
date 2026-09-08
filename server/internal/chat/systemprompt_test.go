package chat

import (
	"context"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/tools"
)

func newPromptTestService(t *testing.T) (*Service, *store.DB) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewService(db, []byte("0123456789abcdef0123456789abcdef"), &tools.Deps{DB: db}), db
}

// setTrunk 写 system_prompt 主干（与 admin PUT 同款 JSON 形状）
func setTrunk(t *testing.T, db *store.DB, content string) {
	t.Helper()
	if err := db.SetSetting(context.Background(), "system_prompt", `{"content":"`+content+`","updated_at":"2026-09-08 10:00"}`); err != nil {
		t.Fatal(err)
	}
}

func TestSystemPromptAssembly(t *testing.T) {
	s, db := newPromptTestService(t)
	ctx := context.Background()

	// 库为初始状态（4 内置风格，默认 = 温暖 id=1）

	t.Run("未配置主干用 fallback + 用户风格命中", func(t *testing.T) {
		got := s.systemPrompt(ctx, "简洁")
		want := SystemPromptTrunkFallback + "输出风格偏好：回答尽量简短，直给要点，不啰嗦。"
		if got != want {
			t.Fatalf("got %q\nwant %q", got, want)
		}
	})

	t.Run("配置主干后替换", func(t *testing.T) {
		setTrunk(t, db, "自定义主干")
		got := s.systemPrompt(ctx, "温暖")
		if got != "自定义主干输出风格偏好：说话温暖贴心，多用鼓励和关心的语气，像伴侣日常聊天一样自然。" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("用户未设置风格走全局默认", func(t *testing.T) {
		got := s.systemPrompt(ctx, "")
		if got != "自定义主干输出风格偏好：说话温暖贴心，多用鼓励和关心的语气，像伴侣日常聊天一样自然。" {
			t.Fatalf("default style mismatch: %q", got)
		}
	})

	t.Run("切换全局默认后跟随", func(t *testing.T) {
		if err := db.SetDefaultStyle(ctx, 3); err != nil { // 简洁
			t.Fatal(err)
		}
		got := s.systemPrompt(ctx, "")
		if got != "自定义主干输出风格偏好：回答尽量简短，直给要点，不啰嗦。" {
			t.Fatalf("switched default mismatch: %q", got)
		}
	})

	t.Run("用户风格名已删除回退全局默认", func(t *testing.T) {
		// 用户新建自定义风格并选中，然后删除该风格
		id, err := db.CreateStyle(ctx, "诗意", "像写诗一样说话。")
		if err != nil {
			t.Fatal(err)
		}
		if got := s.systemPrompt(ctx, "诗意"); got == "" {
			t.Fatal("custom style should match by name")
		}
		if err := db.DeleteStyle(ctx, id); err != nil {
			t.Fatal(err)
		}
		got := s.systemPrompt(ctx, "诗意")
		if got != "自定义主干输出风格偏好：回答尽量简短，直给要点，不啰嗦。" {
			t.Fatalf("deleted style should fall back to default: %q", got)
		}
	})

	t.Run("库空无片段", func(t *testing.T) {
		if _, err := db.Exec(`DELETE FROM output_styles`); err != nil {
			t.Fatal(err)
		}
		if got := s.systemPrompt(ctx, "温暖"); got != "自定义主干" {
			t.Fatalf("empty library should yield trunk only: %q", got)
		}
	})
}
