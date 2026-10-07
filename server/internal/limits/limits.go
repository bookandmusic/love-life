// Package limits 承接**跨语言共享的上限值**：单一数据源是同目录的 media.json，
// Go 侧 go:embed 加载、APP 侧直接 import 同一份文件。
//
// 为什么单独立包：这些上限同时被服务端（校验）与 APP（选择器提示、UI 拦截）使用，
// 各自写一份必然漂移（历史上单条动态媒体数后端 18、前端 9，互不相认）。
// 放在叶子包（不 import 其它 internal 包）以免引入依赖环。
package limits

import (
	_ "embed"
	"encoding/json"
)

//go:embed media.json
var mediaJSON []byte

type mediaLimits struct {
	MaxMediaPerPost        int `json:"maxMediaPerPost"`
	MaxMediaPerChatMessage int `json:"maxMediaPerChatMessage"`
}

var media = mustParseMedia(mediaJSON)

// MaxMediaPerPost 单条动态的媒体文件数上限（PRODUCT「发布动态」：单条最多 9 个）。
// 用 var 而非 const：值来自运行时解析的共享 JSON（改上限只改 media.json，不动代码）。
var MaxMediaPerPost = media.MaxMediaPerPost

// MaxMediaPerChatMessage 单条聊天消息的媒体引用上限（与发动态上限对齐）。
var MaxMediaPerChatMessage = media.MaxMediaPerChatMessage

// mustParseMedia 解析内嵌上限。文件随二进制编译进来，只有被改坏才可能失败，
// 故直接 panic：宁可启动即报错，也不要静默按 0 把上限变成「一个都不许」。
func mustParseMedia(b []byte) mediaLimits {
	var m mediaLimits
	if err := json.Unmarshal(b, &m); err != nil {
		panic("limits: 解析 media.json 失败: " + err.Error())
	}
	if m.MaxMediaPerPost <= 0 || m.MaxMediaPerChatMessage <= 0 {
		panic("limits: media.json 的上限必须为正整数")
	}
	return m
}
