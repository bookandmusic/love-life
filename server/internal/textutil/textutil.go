// Package textutil 文本工具：API 与 AI 工具层共用的快照截断（单一来源，勿在调用方复刻）。
package textutil

// Trunc 按 rune 截断到 n 个字符，超出部分直接丢弃（AI 工具快照口径：不追加省略号）。
func Trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// TruncMark 截断后追加省略号「…」（REST 通知快照口径：提示内容被截断）。
func TruncMark(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
