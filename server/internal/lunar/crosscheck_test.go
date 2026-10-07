package lunar

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// 农历表交叉一致性（ARCHITECTURE「数据模型」Anniversary 护栏）：Go 与 APP(TS) 两份必须逐项一致。
//
// 两处实现是「同源同表」的双份拷贝（server/internal/lunar/lunar.go 与 app/src/utils/lunar.ts），
// 任一侧单独改表都会造成 APP 与服务端算出的纪念日不一致——这类漂移不会 panic，
// 只会安静地给出不同日期，故用测试钉死。
func TestLunarTableMatchesApp(t *testing.T) {
	tsPath := filepath.Join("..", "..", "..", "app", "src", "utils", "lunar.ts")
	raw, err := os.ReadFile(tsPath)
	if err != nil {
		t.Skipf("APP 侧农历表不可读（%v）——通常是在不含 app/ 的构建环境中运行", err)
	}
	body := raw
	if i := strings.Index(string(raw), "const LUNAR_INFO = ["); i >= 0 {
		body = raw[i:]
	}
	if i := strings.Index(string(body), "]"); i >= 0 {
		body = body[:i]
	}
	hexRe := regexp.MustCompile("0x[0-9a-fA-F]+")
	tsVals := hexRe.FindAllString(string(body), -1)

	if len(tsVals) != len(info) {
		t.Fatalf("表长度不一致：go=%d ts=%d", len(info), len(tsVals))
	}
	for i, v := range tsVals {
		n, err := strconv.ParseInt(strings.TrimPrefix(v, "0x"), 16, 64)
		if err != nil {
			t.Fatalf("第 %d 项解析失败 %q: %v", i, v, err)
		}
		if int(n) != info[i] {
			t.Fatalf("第 %d 项（农历年 %d）不一致：go=0x%x ts=0x%x", i, baseYear+i, info[i], n)
		}
	}
}
