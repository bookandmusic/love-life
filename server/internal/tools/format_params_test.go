package tools

import (
	"strings"
	"testing"
)

// 审计 params 需按 mock 契约渲染成人类可读摘要（fixtures.mjs:291-295 形态）。
func TestFormatParams(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"字符串带引号", map[string]any{"query": "海边"}, `query: "海边"`},
		{"多参按键序", map[string]any{"name": "相识纪念日", "date": "2023-05-20"}, `date: "2023-05-20", name: "相识纪念日"`},
		{"数字原样", map[string]any{"page": float64(1)}, "page: 1"},
		{"数组 JSON", map[string]any{"media_ids": []any{float64(1), float64(2)}}, "media_ids: [1,2]"},
		{"布尔原样", map[string]any{"confirmed": true}, "confirmed: true"},
		{"空参空串", map[string]any{}, ""},
		{"nil 空串", nil, ""},
		{"Schema 外参数保留", map[string]any{"unknown": "x"}, `unknown: "x"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FormatParams(c.args); got != c.want {
				t.Fatalf("FormatParams = %q, want %q", got, c.want)
			}
		})
	}
}

// 长值按同一上限截断并追加省略号：字符串保留引号配平，非字符串不补引号。
func TestFormatParamsTruncate(t *testing.T) {
	want := `content: "` + strings.Repeat("长", 60) + `…"`
	if got := FormatParams(map[string]any{"content": strings.Repeat("长", 100)}); got != want {
		t.Fatalf("字符串长值 = %q, want %q", got, want)
	}
	// 长串与短串必须走同一条转义路径：正文里的引号要转义，不能拼出破损外观。
	longQuoted := `he said "hi" ` + strings.Repeat("x", 60) // 13 + 60 = 73 rune
	wantQuoted := `content: "he said \"hi\" ` + strings.Repeat("x", 47) + `…"`
	if got := FormatParams(map[string]any{"content": longQuoted}); got != wantQuoted {
		t.Fatalf("长串转义不一致: %q, want %q", got, wantQuoted)
	}
	got := FormatParams(map[string]any{"media_ids": make([]any, 100)})
	if !strings.HasPrefix(got, "media_ids: [") || !strings.HasSuffix(got, "…") {
		t.Fatalf("非字符串长值未截断: %q", got)
	}
	if n := len([]rune(got)) - len([]rune("media_ids: ")); n != 61 {
		t.Fatalf("非字符串长值应为 60 rune + 省略号，got %d: %q", n, got)
	}
}
