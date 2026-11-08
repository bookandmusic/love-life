package chat

import (
	"testing"

	"github.com/bookandmusic/love-life/server/internal/tools"
)

// uiFence 拼一条卡片提案围栏（独占一行）。
func uiFence(spec string) string { return "```ui " + spec + "\n```" }

// 正文围栏解析：只认 ```ui 围栏，坏 JSON 给 bad_json（回给模型重写，不静默丢弃）。
func TestParseCardFences(t *testing.T) {
	if specs, cerr := parseCardFences("就这些，没有卡片"); len(specs) != 0 || cerr != nil {
		t.Fatalf("没有围栏应返回空：%+v %v", specs, cerr)
	}

	text := "海边这两条：\n" + uiFence(`{"ref":"r_1","view":"list"}`)
	specs, cerr := parseCardFences(text)
	if cerr != nil || len(specs) != 1 {
		t.Fatalf("应解析出 1 条提案：%+v %v", specs, cerr)
	}
	if specs[0].Ref != "r_1" || specs[0].View != tools.ViewList {
		t.Fatalf("解析结果不对：%+v", specs[0])
	}

	// 多条围栏按出现顺序
	two := uiFence(`{"ref":"r_1","view":"list"}`) + "\n正文\n" + uiFence(`{"ref":"r_2","view":"grid","note":"6 张"}`)
	if specs, cerr := parseCardFences(two); cerr != nil || len(specs) != 2 || specs[1].Note != "6 张" {
		t.Fatalf("应解析出 2 条提案：%+v %v", specs, cerr)
	}

	// 坏 JSON：带错误码，不静默丢
	if _, cerr := parseCardFences(uiFence(`{"ref":`)); cerr == nil || cerr.Code != tools.CardErrBadJSON {
		t.Fatalf("坏 JSON 应返回 bad_json，实得 %v", cerr)
	}
}

// 同一份数据只画一次：第二条提案判 duplicate_ref，且不产出卡片。
func TestValidateCardsDuplicate(t *testing.T) {
	data := map[string]*tools.ResultData{
		"r_1": {Ref: "r_1", Kind: tools.KindRecords, Records: []tools.Record{{ID: 1, Kind: "post"}}},
	}
	cards, errs := validateCards([]tools.CardSpec{
		{Ref: "r_1", View: tools.ViewList},
		{Ref: "r_1", View: tools.ViewList},
	}, data)
	if len(errs) != 1 || errs[0].Code != tools.CardErrDuplicateRef {
		t.Fatalf("第二条应判 duplicate_ref：%+v", errs)
	}
	if len(cards) != 1 {
		t.Fatalf("只有第一条通过：%+v", cards)
	}
}
