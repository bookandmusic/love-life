package tools

import (
	"strings"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/notify"
)

// get_record 的动态媒体清单截断后必须注明「已列前 N 条」与完整清单的取法
// （AI模式设计「统计与计数口径」详情清单截断）。动态没有 album_id 可传，故提示按时间范围查。
func TestGetRecordPostMediaTruncationHint(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: notify.New(db)}

	postID := seedPostAt(t, db, "带很多媒体的动态", "2026-08-10 10:00:00")
	const total = 55 // 超过 detailLimitMedia(50)
	for i := 0; i < total; i++ {
		seedMedia(t, db, postID, "photo", "")
	}

	out := exec(t, "get_record", d, 1, map[string]any{"kind": "post", "id": float64(postID)})
	if !strings.Contains(out.ModelText, "已列前 50 条") {
		t.Fatalf("截断时应注明「已列前 50 条」：%s", out.ModelText)
	}
	// 提示必须带 unfiled=false：find_media 缺省只看未归档，而已归档媒体也在本动态清单里
	if !strings.Contains(out.ModelText, "find_media(unfiled=false)") {
		t.Fatalf("截断提示应给出可用的取数参数（unfiled=false）：%s", out.ModelText)
	}
	if strings.Contains(out.ModelText, "album_id") {
		t.Fatalf("动态的提示不得照抄相册那句（动态没有 album_id 可传）：%s", out.ModelText)
	}
	// UI 记录 = 动态本体 1 条 + 截断后的 50 条媒体
	if n := len(out.UI.Records); n != 51 {
		t.Fatalf("UI 记录应为 1（动态本体）+ 50（截断后媒体），实得 %d", n)
	}

	// 未超限时不应出现截断提示
	small := seedPostAt(t, db, "只有两条媒体", "2026-08-11 10:00:00")
	seedMedia(t, db, small, "photo", "")
	seedMedia(t, db, small, "photo", "")
	out2 := exec(t, "get_record", d, 1, map[string]any{"kind": "post", "id": float64(small)})
	if strings.Contains(out2.ModelText, "已列前") {
		t.Fatalf("未超限不应提截断：%s", out2.ModelText)
	}
}
