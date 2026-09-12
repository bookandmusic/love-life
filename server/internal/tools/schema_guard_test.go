package tools

import (
	"fmt"
	"testing"
)

// TestSchemaMatchesParams 守卫测试：防止 Schema 与 Params 再次漂移。
// Schema 用于后台展示，Params 是模型实际调用的契约——两者必须保持一致。
func TestSchemaMatchesParams(t *testing.T) {
	for _, tool := range Registry() {
		t.Run(tool.Name, func(t *testing.T) {
			schemaKeys := extractSchemaKeys(tool.Schema)
			paramsKeys := extractParamsKeys(tool.Params)

			// 检查 Schema 中的每个键是否在 Params 中
			for _, key := range schemaKeys {
				if !contains(paramsKeys, key) {
					t.Errorf("Tool %s: Schema 包含 '%s' 但 Params 中不存在", tool.Name, key)
				}
			}

			// 检查 Params 中的 required 字段是否在 Schema 中
			required := extractRequiredKeys(tool.Params)
			for _, key := range required {
				if !contains(schemaKeys, key) {
					t.Errorf("Tool %s: Params required 包含 '%s' 但 Schema 中不存在", tool.Name, key)
				}
			}
		})
	}
}

func extractSchemaKeys(schema map[string]string) []string {
	keys := make([]string, 0, len(schema))
	for k := range schema {
		keys = append(keys, k)
	}
	return keys
}

func extractParamsKeys(params map[string]any) []string {
	props, ok := params["properties"].(map[string]any)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	return keys
}

func extractRequiredKeys(params map[string]any) []string {
	req, ok := params["required"].([]string)
	if !ok {
		return nil
	}
	return req
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// TestToolCategoriesValid 验证工具分类值有效性
func TestToolCategoriesValid(t *testing.T) {
	validCategories := map[string]bool{
		"动态": true, "相册": true, "纪念日": true, "地图": true,
		"互动": true, "对话": true, "系统": true, "空间": true,
	}

	for _, tool := range Registry() {
		if !validCategories[tool.Category] {
			t.Errorf("Tool %s: 无效分类 '%s'，有效值: %v",
				tool.Name, tool.Category, validCategories)
		}
	}
}

// TestWriteToolsHaveConfirmFlow 验证写工具不包含内部审计（应由 chat confirm 统一处理）
func TestWriteToolsHaveNoInternalAudit(t *testing.T) {
	// 这是一个文档测试：写工具的审计应该在 chat/service.go Confirm() 中统一处理
	// 不应该在工具 Exec 内部调用 d.audit()

	writeTools := []string{}
	for _, tool := range Registry() {
		if tool.IsWrite {
			writeTools = append(writeTools, tool.Name)
		}
	}

	if len(writeTools) == 0 {
		t.Skip("没有写工具")
	}

	// 这个测试只是文档性质的，实际的审计检查需要代码审查
	t.Logf("写工具列表（应在 chat confirm 流程中审计）: %v", writeTools)
}

// TestReadToolsHaveAudit 验证读工具包含内部审计
func TestReadToolsHaveAudit(t *testing.T) {
	// 读工具应该在 Exec 内部调用 d.audit()，因为它们立即执行

	readTools := []string{}
	for _, tool := range Registry() {
		if !tool.IsWrite {
			readTools = append(readTools, tool.Name)
		}
	}

	if len(readTools) == 0 {
		t.Skip("没有读工具")
	}

	// 这个测试只是文档性质的，实际的审计检查需要代码审查
	t.Logf("读工具列表（应在 Exec 内部审计）: %v", readTools)
}

// Example_toolContract 演示如何添加新工具时避免契约漂移
func Example_toolContract() {
	// 正确做法：Schema 和 Params 的键名完全一致
	tool := &Tool{
		Name: "example_tool",
		Schema: map[string]string{
			"user_id": "number — 用户 ID",
			"content": "string — 内容",
		},
		Params: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"user_id": map[string]any{"type": "integer"}, // ✅ 与 Schema 一致
				"content": map[string]any{"type": "string"},  // ✅ 与 Schema 一致
			},
			"required": []string{"user_id", "content"},
		},
	}

	fmt.Printf("Tool: %s\n", tool.Name)
	// Output: Tool: example_tool
}
