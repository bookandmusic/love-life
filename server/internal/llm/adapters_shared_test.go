package llm

// newToolCall 测试 helper：构造规范化 ToolCall（Function 是内联匿名结构体，
// 字面量冗长，封装一下）。
func newToolCall(id, name, arguments string) ToolCall {
	tc := ToolCall{ID: id, Type: "function"}
	tc.Function.Name = name
	tc.Function.Arguments = arguments
	return tc
}
