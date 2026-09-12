package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
)

// ---- AD4 MCP 工具启停状态 ----
//
// 启停状态存于 app_settings.mcp_disabled_tools（JSON 数组，元素为工具名）。
// 选择「禁用集合」而非「启用集合」：新注册的工具默认启用，无需迁移数据，
// 也避免新工具因未登记而被静默停用。

const settingMCPDisabledTools = "mcp_disabled_tools"

// DisabledTools 读取被停用的工具名集合。
func (d *DB) DisabledTools(ctx context.Context) (map[string]bool, error) {
	out := map[string]bool{}
	raw, err := d.GetSetting(ctx, settingMCPDisabledTools)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return out, nil
		}
		return nil, err
	}
	if raw == "" {
		return out, nil
	}
	var names []string
	if err := json.Unmarshal([]byte(raw), &names); err != nil {
		// 值损坏时按「全部启用」处理，不因配置脏数据阻断工具调用。
		return out, nil
	}
	for _, n := range names {
		out[n] = true
	}
	return out, nil
}

// SetToolEnabled 启停单个工具（enabled=false 记入禁用集合）。
func (d *DB) SetToolEnabled(ctx context.Context, name string, enabled bool) error {
	cur, err := d.DisabledTools(ctx)
	if err != nil {
		return err
	}
	if enabled {
		delete(cur, name)
	} else {
		cur[name] = true
	}
	names := make([]string, 0, len(cur))
	for n := range cur {
		names = append(names, n)
	}
	// 排序保证落库值稳定（便于比对与测试）
	sort.Strings(names)
	b, err := json.Marshal(names)
	if err != nil {
		return err
	}
	return d.SetSetting(ctx, settingMCPDisabledTools, string(b))
}
