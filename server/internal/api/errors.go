package api

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
