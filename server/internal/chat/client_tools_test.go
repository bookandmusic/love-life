package chat

import (
	"strings"
	"testing"
)

// 客户端展示能力拼装（APP「AI 对话渲染」S5）：模板是服务端资产，客户端只声明它能画哪些 view。
func TestClientToolPrompt(t *testing.T) {
	// 声明 list + stats → 提示词含 view 选择规则、围栏写法与错误码含义
	got := clientToolPrompt([]clientToolDef{
		{Name: "list", Desc: "记录行"},
		{Name: "stats", Desc: "空间概览"},
	})
	for _, want := range []string{
		"【展示卡片】", "list：记录清单", "stats：数字网格", "```ui", "结果集 r_N",
		"≤3 张", "画成哪一种由你定",
		// 6 个错误码含义（模型据此自纠）
		"unknown_ref", "grid_needs_media", "stats_needs_numbers",
		"detail_needs_entity", "list_needs_records", "duplicate_ref",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("提示词缺 %q\ngot: %s", want, got)
		}
	}
	// 未声明的 view 不进规则表（客户端画不了的形态不提示模型去选）
	for _, unwanted := range []string{"grid：图片网格", "detail：单条详情"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("未声明 %q 不该出现在提示词里", unwanted)
		}
	}

	// 不声明 → 不注入
	if got := clientToolPrompt(nil); got != "" {
		t.Errorf("未声明应返回空串，got: %s", got)
	}
	// 只声明了服务端不认识的形态 → 同样不注入（没有可校验的 view）
	if got := clientToolPrompt([]clientToolDef{{Name: "heatmap", Desc: "热力图"}}); got != "" {
		t.Errorf("未知 view 不应注入提示词，got: %s", got)
	}
	// 声明了 timeline / map → 各自的规则与错误码含义都进提示词
	full := clientToolPrompt([]clientToolDef{
		{Name: "timeline", Desc: "时间线"}, {Name: "map", Desc: "地图"},
	})
	for _, want := range []string{"timeline：按日期排", "map：地图点位", "timeline_needs_dates", "map_needs_coords"} {
		if !strings.Contains(full, want) {
			t.Errorf("声明 timeline/map 后提示词缺 %q", want)
		}
	}

	// 非法项丢弃、合法项保留；超 8 个截断；不 400（校验函数不返回错误）
	mixed := []clientToolDef{
		{Name: "Bad-Name", Desc: "x", Params: ""},  // 大写非法
		{Name: "list", Desc: "fine", Params: ""},   // 合法
		{Name: "list", Desc: "dup", Params: ""},    // 重复名丢弃
		{Name: strings.Repeat("a", 30), Desc: "x"}, // 超长名丢弃
	}
	out := sanitizeClientTools(mixed)
	if len(out) != 1 || out[0].Name != "list" {
		t.Errorf("非法项应被丢弃只留 list，got %+v", out)
	}

	many := make([]clientToolDef, 12)
	for i := range many {
		many[i] = clientToolDef{Name: "t" + string(rune('a'+i))}
	}
	if got := sanitizeClientTools(many); len(got) != 8 {
		t.Errorf("应截断到 8 个，got %d", len(got))
	}

	// 控制字符被去、超长被截
	dirty := sanitizeClientTools([]clientToolDef{{Name: "clean", Desc: "a\nb\x00c" + strings.Repeat("长", 300)}})
	if strings.ContainsAny(dirty[0].Desc, "\x00") {
		t.Error("控制字符应被移除")
	}
	if len([]rune(dirty[0].Desc)) > 200 {
		t.Errorf("desc 应截断到 200 rune，got %d", len([]rune(dirty[0].Desc)))
	}
}
