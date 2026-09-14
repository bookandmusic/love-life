package search

import "testing"

// 嵌入文本口径：标题 + 正文 + 地点，跳过空段（空段只稀释语义、不提供信息）。
func TestEmbedText(t *testing.T) {
	cases := []struct {
		name                           string
		title, content, location, want string
	}{
		{"全空", "", "", "", ""},
		{"仅正文", "", "去看了海", "", "去看了海"},
		{"三段", "今天", "去看了海", "青岛", "今天\n去看了海\n青岛"},
		{"跳过空段", "", "去看了海", "青岛", "去看了海\n青岛"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := embedText(c.title, c.content, c.location); got != c.want {
				t.Fatalf("embedText() = %q, want %q", got, c.want)
			}
		})
	}
}
