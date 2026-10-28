package api

import (
	"log/slog"
	"net/http"
)

// 通用错误消息常量。
//
// 这些文案在 api 层大量重复（"服务器错误" 等），集中定义避免改一处漏一处；
// 业务专属文案（"风格不存在" 等）语义各异、多为一次性，保持就近书写。
const (
	// errInternal 未预期的内部错误（对外不暴露细节）。
	errInternal = "服务器错误"
	// errInvalidID 路径/查询参数中的 ID 非法。
	errInvalidID = "无效 ID"
	// errBadRequest 请求体格式错误或字段不合法。
	errBadRequest = "请求格式错误"
	// errUnauthorized 未认证或凭证失效。
	errUnauthorized = "未认证"
)

// writeErrorf 带原因的错误响应：对外只回 msg，5xx 额外把 err 记进服务日志。
//
// 为什么需要它：`writeError` 只写响应体，`err` 被就地丢弃——137 处 500 全是「服务器错误」，
// 服务端 stdout 与「服务日志」页都看不到任何线索，排障只能靠猜。
//
// 只记 5xx：4xx 是用户可预期的业务拒绝（密码错、找不到），记下来会把日志淹掉。
// 原始错误只进日志不进响应体——里面常有内网地址与 Go 术语（ARCHITECTURE
// 「明确不做与已否决项 · 实现取向」：不把模型报错原文透给客户端，此处同口径）。
func writeErrorf(w http.ResponseWriter, r *http.Request, status int, msg string, err error) {
	if status >= http.StatusInternalServerError && err != nil {
		attrs := []any{"status", status, "err", err}
		if r != nil {
			attrs = append(attrs, "method", r.Method, "path", r.URL.Path)
		}
		slog.Error("request failed", attrs...)
	}
	writeError(w, status, msg)
}
