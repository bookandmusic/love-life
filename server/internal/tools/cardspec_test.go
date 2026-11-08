package tools

import (
	"strings"
	"testing"
)

// cardEnv 造一份校验环境：records / media / entity / stats 四种数据各一个句柄。
func cardEnv() CardEnv {
	return CardEnv{
		Data: map[string]*ResultData{
			"r_1": {Ref: "r_1", Kind: KindRecords, Records: []Record{{ID: 1, Kind: "post", Text: "海边"}}},
			"r_2": {Ref: "r_2", Kind: KindMedia, Records: []Record{{ID: 2, Kind: "media", ThumbURL: "/t.jpg"}}},
			"r_3": {Ref: "r_3", Kind: KindEntity, Entity: &Entity{Kind: "post", ID: 3}},
			"r_4": {Ref: "r_4", Kind: KindStats, Stats: []Stat{{Label: "动态", Value: "3 条"}}},
			// 多数字 + 分布：kpi 只能画一个数字，only 用来点名其中一项
			"r_5": {Ref: "r_5", Kind: KindStats, Stats: []Stat{
				{Label: "媒体", Value: "12 个"}, {Label: "动态", Value: "3 条"}, {Label: "足迹", Value: "6 点"},
			}, Series: []Series{{Title: "按年"}}, Summaries: []Summary{{Label: "城市", Text: "走过 2 城"}}},
			// 带日期的记录（时间轴）与带经纬度的点位（地图）
			"r_6": {Ref: "r_6", Kind: KindRecords, Records: []Record{
				{ID: 6, Kind: "post", Text: "第一天", Date: "2026-01-02"},
				{ID: 7, Kind: "post", Text: "没有日期"},
			}},
			"r_7": {Ref: "r_7", Kind: KindRecords, Records: []Record{
				{ID: 8, Kind: "place", Text: "海边", Lat: 30.2, Lng: 120.1},
				{ID: 9, Kind: "place", Text: "没坐标的候选"},
			}},
		},
		Used: map[string]bool{},
	}
}

// 校验规则表逐条（AI 卡片体系）：不合法必须给错误码，不静默降级。
func TestValidateSpecRules(t *testing.T) {
	cases := []struct {
		name string
		spec CardSpec
		want string // 期望错误码；空串 = 通过
	}{
		{"unknown_ref", CardSpec{Ref: "r_9", View: ViewList}, CardErrUnknownRef},
		{"empty_ref", CardSpec{View: ViewList}, CardErrUnknownRef},
		{"grid_needs_media", CardSpec{Ref: "r_1", View: ViewGrid}, CardErrGridNeedsMedia},
		{"stats_needs_numbers", CardSpec{Ref: "r_1", View: ViewStats}, CardErrStatsNeedsNum},
		{"detail_needs_entity", CardSpec{Ref: "r_1", View: ViewDetail}, CardErrDetailNeedsEnt},
		{"list_needs_records", CardSpec{Ref: "r_3", View: ViewList}, CardErrListNeedsRecs},
		{"unknown_view", CardSpec{Ref: "r_1", View: "table"}, CardErrUnknownView},
		// 合法组合
		{"list_ok", CardSpec{Ref: "r_1", View: ViewList}, ""},
		{"grid_ok", CardSpec{Ref: "r_2", View: ViewGrid}, ""},
		{"detail_ok", CardSpec{Ref: "r_3", View: ViewDetail}, ""},
		{"stats_ok", CardSpec{Ref: "r_4", View: ViewStats}, ""},
		// kpi：恰好一个数字才成立
		{"kpi_ok_single", CardSpec{Ref: "r_4", View: ViewKpi}, ""},
		{"kpi_needs_one", CardSpec{Ref: "r_5", View: ViewKpi}, CardErrKpiNeedsOne},
		{"kpi_ok_with_only", CardSpec{Ref: "r_5", View: ViewKpi, Only: []string{"媒体"}}, ""},
		// kpi 的 only 必须命中 Stats 的 Label：Series/Summaries 的标题不算数字项
		{"kpi_only_not_number", CardSpec{Ref: "r_5", View: ViewKpi, Only: []string{"按年"}}, CardErrKpiNeedsOne},
		{"kpi_only_unknown_field", CardSpec{Ref: "r_5", View: ViewKpi, Only: []string{"不存在的项"}}, CardErrUnknownField},
		// only：只配 stats / kpi，项名必须存在
		{"only_for_stats", CardSpec{Ref: "r_1", View: ViewList, Only: []string{"媒体"}}, CardErrOnlyForStats},
		{"only_unknown_field", CardSpec{Ref: "r_5", View: ViewStats, Only: []string{"不存在的项"}}, CardErrUnknownField},
		{"stats_only_ok", CardSpec{Ref: "r_5", View: ViewStats, Only: []string{"媒体", "城市"}}, ""},
		// limit：只配 list / grid / timeline，取值 1–50
		{"limit_on_stats", CardSpec{Ref: "r_4", View: ViewStats, Limit: 2}, CardErrBadLimit},
		{"limit_zero", CardSpec{Ref: "r_1", View: ViewList, Limit: -1}, CardErrBadLimit},
		{"limit_too_big", CardSpec{Ref: "r_1", View: ViewList, Limit: 51}, CardErrBadLimit},
		{"limit_on_map", CardSpec{Ref: "r_7", View: ViewMap, Limit: 3}, CardErrBadLimit},
		{"list_limit_ok", CardSpec{Ref: "r_1", View: ViewList, Limit: 3}, ""},
		// timeline：记录带日期才排得出时间轴
		{"timeline_ok", CardSpec{Ref: "r_6", View: ViewTimeline}, ""},
		{"timeline_limit_ok", CardSpec{Ref: "r_6", View: ViewTimeline, Limit: 5}, ""},
		{"timeline_needs_dates", CardSpec{Ref: "r_1", View: ViewTimeline}, CardErrTimelineNoDate},
		{"timeline_only_rejected", CardSpec{Ref: "r_6", View: ViewTimeline, Only: []string{"按年"}}, CardErrOnlyForStats},
		// map：记录带经纬度才落得上图
		{"map_ok", CardSpec{Ref: "r_7", View: ViewMap}, ""},
		{"map_needs_coords", CardSpec{Ref: "r_1", View: ViewMap}, CardErrMapNoCoord},
		{"map_on_stats", CardSpec{Ref: "r_5", View: ViewMap}, CardErrMapNoCoord},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSpec(tc.spec, cardEnv())
			if tc.want == "" {
				if err != nil {
					t.Fatalf("应通过，实得 %s", err.Error())
				}
				return
			}
			if err == nil || err.Code != tc.want {
				t.Fatalf("错误码 = %v，want %s", err, tc.want)
			}
			if err.Message == "" {
				t.Fatal("错误必须带给人话（模型据此重写）")
			}
		})
	}
}

// duplicate_ref：同一 ref 画第二次被拒；句柄清单按升序回给模型。
func TestValidateSpecDuplicateAndRefs(t *testing.T) {
	env := cardEnv()
	env.Used["r_1"] = true
	err := ValidateSpec(CardSpec{Ref: "r_1", View: ViewList}, env)
	if err == nil || err.Code != CardErrDuplicateRef {
		t.Fatalf("应判 duplicate_ref，实得 %v", err)
	}

	err = ValidateSpec(CardSpec{Ref: "r_9", View: ViewList}, env)
	if err == nil || err.Code != CardErrUnknownRef {
		t.Fatalf("应判 unknown_ref，实得 %v", err)
	}
	if got := strings.Join(err.AvailableRefs, ","); got != "r_1,r_2,r_3,r_4,r_5,r_6,r_7" {
		t.Fatalf("可用句柄应升序列出，实得 %q", got)
	}
	if !strings.Contains(err.Message, "r_1") {
		t.Fatalf("错误文案应带可用句柄：%s", err.Message)
	}

	// 空环境：可用句柄文案不能是空串
	err = ValidateSpec(CardSpec{Ref: "r_1", View: ViewList}, CardEnv{})
	if err == nil || !strings.Contains(err.Message, "无") {
		t.Fatalf("无可用句柄时文案应说明，实得 %v", err)
	}
}

// listData 空列表不出数据（空结果在源头就没有 Data，不做「先造再筛」）。
func TestListDataDropsEmpty(t *testing.T) {
	if got := listData(&ResultData{Ref: "r_1", Kind: KindRecords}); got != nil {
		t.Fatalf("空列表应返回 nil，实得 %+v", got)
	}
	if got := listData(nil); got != nil {
		t.Fatalf("nil 应原样返回 nil，实得 %+v", got)
	}
	if got := listData(&ResultData{Ref: "r_1", Records: []Record{{ID: 1}}}); got == nil {
		t.Fatal("有条目应原样返回")
	}
}
