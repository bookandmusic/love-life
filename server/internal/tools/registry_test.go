package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/media"
	"github.com/bookandmusic/love-life/server/internal/notify"
	"github.com/bookandmusic/love-life/server/internal/store"
)

func findTool(t *testing.T, name string) *Tool {
	t.Helper()
	for _, tool := range Registry() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not registered", name)
	return nil
}

// withSink 注入结果集端口（chat 层实际职责：绑会话；工具单测自备一个会话 id=0 的）。
func withSink(ctx context.Context, db *store.DB) context.Context {
	ctx = WithSessionID(ctx, 0)
	return WithResultSink(ctx, func(kind string, ids []int64, groups []store.ResultGroup) (string, error) {
		return db.SaveResultSet(ctx, 0, kind, ids, groups)
	})
}

func newToolDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	hash, _ := auth.HashPassword("pw")
	if _, err := db.CreateUser(t.Context(), &store.User{Username: "girl", PasswordHash: hash, Role: "girl"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return db
}

// testNotify 构造推送器，并在测试清理时等待在途推送排空。
// Dispatch 起的是后台 goroutine（读渠道、写库）：不等它结束就关库/删临时目录，
// -race -count=N 下清理会撞上仍在写 SQLite 的推送，报 "directory not empty"。
func testNotify(t *testing.T, db *store.DB) *notify.Dispatcher {
	t.Helper()
	dp := notify.New(db)
	t.Cleanup(dp.Wait)
	return dp
}

func exec(t *testing.T, name string, d *Deps, userID int64, args map[string]any) *ToolResult {
	t.Helper()
	out, err := findTool(t, name).Invoke(withSink(t.Context(), d.DB), d, userID, args, ModeExecute)
	if err != nil {
		t.Fatalf("%s exec: %v", name, err)
	}
	return out
}

func propose(t *testing.T, name string, d *Deps, userID int64, args map[string]any) *ToolResult {
	t.Helper()
	out, err := findTool(t, name).Invoke(withSink(t.Context(), d.DB), d, userID, args, ModePropose)
	if err != nil {
		t.Fatalf("%s propose: %v", name, err)
	}
	return out
}

// 注册表形状：15 个工具（读 7 / 提案 7 / 删除 1），ProgressLabel 齐备，
// 删除类仅 propose_delete 且 AppOnly；提案类 IsWrite。
func TestRegistryShape(t *testing.T) {
	reg := Registry()
	if len(reg) != 15 {
		names := make([]string, 0, len(reg))
		for _, tl := range reg {
			names = append(names, tl.Name)
		}
		t.Fatalf("tool count = %d, want 15: %v", len(reg), names)
	}
	var reads, writes, appOnly int
	for _, tl := range reg {
		if tl.ProgressLabel == "" {
			t.Fatalf("%s: ProgressLabel 缺失（tool.start 的人话标签）", tl.Name)
		}
		switch {
		case tl.IsWrite:
			writes++
			if !strings.HasPrefix(tl.Name, "propose_") {
				t.Fatalf("%s: 提案类必须以 propose_ 前缀命名", tl.Name)
			}
		default:
			reads++
		}
		if tl.AppOnly {
			appOnly++
			if tl.Name != "propose_delete" {
				t.Fatalf("%s: 仅删除类可 AppOnly", tl.Name)
			}
		}
	}
	if reads != 7 || writes != 8 {
		t.Fatalf("reads=%d writes=%d, want 7/8", reads, writes)
	}
	if appOnly != 1 {
		t.Fatalf("appOnly=%d, want 1", appOnly)
	}
	for _, want := range []string{"find_posts", "find_albums", "find_media", "find_anniversaries", "find_places", "get_record"} {
		if findTool(t, want) == nil {
			t.Fatalf("missing read tool %s", want)
		}
	}
	// RegistryForPAT：AppOnly 不出现在 PAT 工具表（铁律：删除类不开放）
	for _, tl := range RegistryForPAT() {
		if tl.AppOnly {
			t.Fatalf("RegistryForPAT leaked AppOnly tool %q", tl.Name)
		}
	}
	if len(RegistryForPAT()) != 14 {
		t.Fatalf("PAT tools = %d, want 14", len(RegistryForPAT()))
	}
}

// ModelText 四条硬规则：条件回显、句柄与条数、≤10 逐条列 id、超长截断。
func TestModelTextRules(t *testing.T) {
	recs := []Record{{ID: 12, Kind: "post", Text: "海边的日落", Date: "2026-08-12", Badge: "有图"}}
	got := modelTextForList("按 query=海边 查询动态，", 1, recs, "r_17")
	for _, want := range []string{"query=海边", "共 1 条", "r_17", "#12", "海边的日落", "2026-08-12"} {
		if !strings.Contains(got, want) {
			t.Fatalf("ModelText 缺 %q：%s", want, got)
		}
	}
	// >10 条：只列前 5 + 句柄
	many := make([]Record, 0, 80)
	for i := 1; i <= 80; i++ {
		many = append(many, Record{ID: int64(i), Kind: "media", Text: "照片"})
	}
	got = modelTextForList("按 from=2026-08 查询，", 80, many, "r_3")
	if !strings.Contains(got, "共 80 条") || !strings.Contains(got, "另有 75 条未列出") {
		t.Fatalf("大结果集应只列前 5 + 计数：%s", got)
	}
	if strings.Contains(got, "#6") {
		t.Fatalf(">10 条不得逐条列 id：%s", got)
	}
	// 超长截断到 2000 rune
	long := make([]Record, 0, 10)
	for i := 0; i < 10; i++ {
		long = append(long, Record{ID: int64(i), Kind: "post", Text: strings.Repeat("字", 300)})
	}
	if n := len([]rune(modelTextForList("x", 10, long, "r_1"))); n > modelTextCap+1 {
		t.Fatalf("ModelText 超上限未截断: %d", n)
	}
}

// find_posts：浏览/关键词/过滤全下推；产出三分离（Text + UserText + Data，Data 带句柄）。
func TestFindPosts(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: testNotify(t, db)}
	ctx := t.Context()
	in1 := seedPostAt(t, db, "海边散步", "2026-01-15 10:00:00")
	in2 := seedPostAt(t, db, "海边看日出", "2026-02-15 10:00:00")
	out := seedPostAt(t, db, "山里的清晨", "2026-03-15 10:00:00")
	seedPostMediaKind(t, db, in1, "photo")

	// 关键词 + 日期过滤（SQL 下推）
	res := exec(t, "find_posts", d, 1, map[string]any{"query": "海边", "from": "2026-01-01", "to": "2026-01-31"})
	if res.Data == nil || res.Data.Kind != KindRecords || len(res.Data.Records) != 1 {
		t.Fatalf("Data = %+v", res.Data)
	}
	if res.Data.Records[0].ID != in1 {
		t.Fatalf("hit = %+v, want %d", res.Data.Records[0], in1)
	}
	if res.Data.Ref == "" {
		t.Fatal("结果集句柄缺失（D10）：Data.Ref 为空")
	}
	firstResultID := res.Data.Ref
	if !strings.Contains(res.Text, "from=2026-01-01") {
		t.Fatalf("ModelText 必须回显查询条件：%s", res.Text)
	}
	if res.UserText == "" {
		t.Fatal("UserText 缺失")
	}
	// 日期过滤真的下推：区间只含 3 月的动态 → 只出 out
	res = exec(t, "find_posts", d, 1, map[string]any{"from": "2026-03-01", "to": "2026-03-31"})
	if len(res.Data.Records) != 1 || res.Data.Records[0].ID != out {
		t.Fatalf("date pushdown: %+v", res.Data.Records)
	}
	// media_type 过滤
	res = exec(t, "find_posts", d, 1, map[string]any{"media_type": "photo"})
	if len(res.Data.Records) != 1 || res.Data.Records[0].ID != in1 {
		t.Fatalf("media_type: %+v", res.Data.Records)
	}
	_ = in2
	// 那年今日：独立路径（测试库里没有今天的记录 → 空结果不出卡，答案交给模型的话）
	res = exec(t, "find_posts", d, 1, map[string]any{"on_this_day": true})
	if res.Data != nil {
		t.Fatalf("空结果不该出卡: %+v", res.Data)
	}
	// 结果集确实落库（句柄可回取并校验归属）
	if _, err := db.GetResultSet(ctx, 0, firstResultID); err != nil {
		t.Fatalf("结果集未落库: %v", err)
	}
	// 非法参数报错
	if _, err := findTool(t, "find_posts").Invoke(ctx, d, 1, map[string]any{"from": "2026/01/01"}, ModeExecute); err == nil {
		t.Fatal("非法 from 应报错")
	}
	if _, err := findTool(t, "find_posts").Invoke(ctx, d, 1, map[string]any{"media_type": "doc"}, ModeExecute); err == nil {
		t.Fatal("非法 media_type 应报错")
	}
}

// find_media：过滤下推 + group_by 分桶（吸收 suggest_photo_grouping）。
func TestFindMediaGrouping(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: testNotify(t, db)}
	p1 := seedPostAt(t, db, "厦门海边", "2026-08-10 10:00:00")
	p2 := seedPostAt(t, db, "厦门鼓浪屿", "2026-08-11 10:00:00")
	m1 := seedMedia(t, db, p1, "photo", "2026-08-10 10:00:00")
	m2 := seedMedia(t, db, p2, "photo", "2026-08-11 10:00:00")
	seedMedia(t, db, p2, "video", "2026-08-11 11:00:00")

	res := exec(t, "find_media", d, 1, map[string]any{"media_type": "photo"})
	if res.Data == nil || res.Data.Kind != KindMedia || len(res.Data.Records) != 2 {
		t.Fatalf("grid = %+v", res.Data)
	}
	if !res.Data.Selectable {
		t.Fatal("媒体网格必须可勾选（D9 引用）")
	}
	res = exec(t, "find_media", d, 1, map[string]any{"group_by": "date"})
	if res.Data == nil || res.Data.Kind != KindMedia {
		t.Fatalf("group_by 应出 grid: %+v", res.Data)
	}
	if len(res.Data.Records) != 2 {
		t.Fatalf("按日期应两组（08-10 / 08-11）: %+v", res.Data.Records)
	}
	if res.Data.Records[0].GroupKey == "" {
		t.Fatal("分组条目必须带 GroupKey（勾选引用要凭它展开）")
	}
	if !strings.Contains(res.Text, "分组") || !strings.Contains(res.Text, res.Data.Ref) {
		t.Fatalf("分组 ModelText 应含分组说明与句柄：%s", res.Text)
	}
	_ = m1
	_ = m2
}

// find_albums / find_anniversaries / find_places / get_record 的基本形状。
func TestFindOthers(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: testNotify(t, db)}
	ctx := t.Context()
	albumID, err := db.CreateAlbum(ctx, "海边合集", "看海", 1)
	if err != nil {
		t.Fatal(err)
	}
	annID, err := db.CreateAnniversary(ctx, "相识", "2020-05-20", "solar", false, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.FindOrCreatePoint(ctx, store.PointInput{Name: "杭州·西湖", Lat: 30.25, Lng: 120.15, Source: "ai", UserID: 1}); err != nil {
		t.Fatal(err)
	}

	res := exec(t, "find_albums", d, 1, map[string]any{"query": "海边"})
	if len(res.Data.Records) != 1 || res.Data.Records[0].ID != albumID {
		t.Fatalf("find_albums: %+v", res.Data.Records)
	}
	res = exec(t, "find_anniversaries", d, 1, map[string]any{"upcoming_only": true})
	if len(res.Data.Records) != 1 || res.Data.Records[0].ID != annID {
		t.Fatalf("find_anniversaries: %+v", res.Data.Records)
	}
	if !strings.Contains(res.Data.Records[0].Text, "相识") {
		t.Fatalf("anniversary text = %q", res.Data.Records[0].Text)
	}
	// 提醒设置与提醒日必须进回执：模型凭它回答「这个月有哪些提醒」「提前几天提醒」
	if got := res.Data.Records[0].Remind; !strings.Contains(got, "提前 1 天提醒（") {
		t.Fatalf("纪念日回执应带提醒设置与提醒日，实得 %q", got)
	}
	if !strings.Contains(res.Text, "提前 1 天提醒（") {
		t.Fatalf("ModelText 应含提醒日（否则模型答不出日期）：%s", res.Text)
	}
	res = exec(t, "find_places", d, 1, map[string]any{"query": "西湖"})
	if res.Data == nil || res.Data.Kind != KindRecords || !res.Data.Selectable {
		t.Fatalf("find_places Data = %+v", res.Data)
	}
	var sawVisited, sawCandidate bool
	for _, r := range res.Data.Records {
		if r.ID > 0 {
			sawVisited = true
			// 已记录点位带经纬度：地图卡靠它落点（候选没有坐标，是「建议你去建」）
			if r.Lat != 30.25 || r.Lng != 120.15 {
				t.Fatalf("已记录点位应带经纬度，实得 %+v", r)
			}
		} else if r.Badge == "候选" {
			sawCandidate = true
		}
	}
	if !sawVisited {
		t.Fatalf("应含已记录点位: %+v", res.Data.Records)
	}
	_ = sawCandidate

	// get_record：post 详情含媒体与评论
	pID := seedPostAt(t, db, "带图动态", "2026-08-10 10:00:00")
	seedMedia(t, db, pID, "photo", "2026-08-10 10:00:00")
	exec(t, "get_record", d, 1, map[string]any{"kind": "post", "id": float64(pID)})
	res = exec(t, "get_record", d, 1, map[string]any{"kind": "album", "id": float64(albumID)})
	if res.Data == nil || res.Data.Entity == nil || res.Data.Entity.Kind != "album" {
		t.Fatalf("get_record album: %+v", res.Data)
	}
	if _, err := findTool(t, "get_record").Invoke(ctx, d, 1, map[string]any{"kind": "post", "id": float64(999999)}, ModeExecute); err == nil {
		t.Fatal("不存在的 post 应报错")
	}
}

// propose_post：Propose 出目标形态预览与展开参数；Execute 建动态（含媒体转正）；纯媒体校验。
func TestProposePostFlow(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: testNotify(t, db), MP: newTestPipeline(t)}
	ctx := t.Context()

	// 校验前置：全空 → 报错（不出卡）
	if _, err := findTool(t, "propose_post").Invoke(ctx, d, 1, map[string]any{}, ModePropose); err == nil {
		t.Fatal("全空动态应报错")
	}
	// Propose：预览含正文/心情/地点（含解析坐标）
	out := propose(t, "propose_post", d, 1, map[string]any{"content": "今天去看了电影", "mood": "开心", "place": "厦门·鼓浪屿"})
	if out.Proposal == nil {
		t.Fatal("Propose 模式必须产出提案")
	}
	if out.Proposal.Preview.Kind != "post" {
		t.Fatalf("preview kind = %q", out.Proposal.Preview.Kind)
	}
	b, _ := json.Marshal(out.Proposal.Preview.Body)
	if !strings.Contains(string(b), "今天去看了电影") || !strings.Contains(string(b), "开心") {
		t.Fatalf("预览缺参数：%s", b)
	}
	if out.Proposal.Params["latitude"] == nil {
		t.Fatalf("地名应解析出坐标：%+v", out.Proposal.Params)
	}
	// Execute（模拟 resolve：params + media_tokens）
	execArgs := out.Proposal.Params
	res := exec(t, "propose_post", d, 1, execArgs)
	if res.Record == nil || res.Record.Kind != "post" {
		t.Fatalf("record = %+v", res.Record)
	}
	var loc sql.NullString
	if err := db.QueryRow(`SELECT location_name FROM posts WHERE id = ?`, res.Record.ID).Scan(&loc); err != nil {
		t.Fatal(err)
	}
	if !loc.Valid || loc.String != "厦门·鼓浪屿" {
		t.Fatalf("location_name = %q", loc.String)
	}
	// 通知另一方
	var n int
	hash, _ := auth.HashPassword("pw")
	if _, err := db.CreateUser(ctx, &store.User{Username: "boy", PasswordHash: hash, Role: "boy"}); err != nil {
		t.Fatal(err)
	}
	out = propose(t, "propose_post", d, 1, map[string]any{"content": "再记一条"})
	exec(t, "propose_post", d, 1, out.Proposal.Params)
	_ = db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE event_type='post_created' AND recipient_id=2`).Scan(&n)
	if n != 1 {
		t.Fatalf("post_created 通知 = %d, want 1", n)
	}
}

// propose_reaction：幂等「设为」——状态无变化不出提案；有变化预览写明状态变化。
func TestProposeReactionIdempotent(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: testNotify(t, db)}
	pID := seedPostAt(t, db, "点赞对象", "2026-08-10 10:00:00")

	// 初始未点赞：like=true 出提案，预览 from=false to=true
	out := propose(t, "propose_reaction", d, 1, map[string]any{"post_id": float64(pID), "like": true})
	if out.Proposal == nil {
		t.Fatal("应有提案")
	}
	body, _ := json.Marshal(out.Proposal.Preview.Body)
	if !strings.Contains(string(body), `"like":{"from":false,"to":true}`) {
		t.Fatalf("reaction 预览应含状态迁移：%s", body)
	}
	exec(t, "propose_reaction", d, 1, map[string]any{"post_id": float64(pID), "like": true})
	// 已点赞：再要 like=true → 无变化，不出提案
	out = propose(t, "propose_reaction", d, 1, map[string]any{"post_id": float64(pID), "like": true})
	if out.Proposal != nil {
		t.Fatal("状态无变化不应出提案")
	}
	if !strings.Contains(out.Text, "无需变更") {
		t.Fatalf("无变化应给出说明：%s", out.Text)
	}
	// 取消点赞 → 出提案
	out = propose(t, "propose_reaction", d, 1, map[string]any{"post_id": float64(pID), "like": false})
	if out.Proposal == nil {
		t.Fatal("取消点赞应出提案")
	}
	// 参数缺失报错
	if _, err := findTool(t, "propose_reaction").Invoke(t.Context(), d, 1, map[string]any{"post_id": float64(pID)}, ModePropose); err == nil {
		t.Fatal("like/favorite 都没给应报错")
	}
}

// propose_album：source 句柄展开（模型不搬 id）+ 已归属警告 + 一次确认建册。
func TestProposeAlbumWithSource(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: testNotify(t, db)}
	ctx := t.Context()
	p1 := seedPostAt(t, db, "厦门海边", "2026-08-10 10:00:00")
	m1 := seedMedia(t, db, p1, "photo", "2026-08-10 10:00:00")
	m2 := seedMedia(t, db, p1, "photo", "2026-08-11 10:00:00")
	// 造结果集（模拟 find_media 落句柄），绑会话 7
	rid, err := db.SaveResultSet(ctx, 7, "media", []int64{m1, m2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// 归属校验：句柄不属于会话时展开失败
	if _, err := findTool(t, "propose_album").Invoke(ctx, d, 1,
		map[string]any{"name": "X", "source": map[string]any{"result_id": rid}}, ModePropose); err == nil {
		t.Fatal("句柄不属于当前会话应报错")
	}

	sctx := WithSessionID(ctx, 7)
	// kind 校验：post 集合传给相册类工具必须校验前置驳回（media/post 是两个自增空间，
	// 不校验时 media id 撞 posts 同号会被误当成动态——S1 修正）
	ridPost, err := db.SaveResultSet(ctx, 7, "post", []int64{p1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := findTool(t, "propose_album").Invoke(sctx, d, 1,
		map[string]any{"name": "X", "source": map[string]any{"result_id": ridPost}}, ModePropose); err == nil {
		t.Fatal("post 集合传给相册工具应被 kind 校验驳回")
	}
	out, err := findTool(t, "propose_album").Invoke(sctx, d, 1,
		map[string]any{"name": "厦门行", "source": map[string]any{"result_id": rid}}, ModePropose)
	if err != nil {
		t.Fatalf("propose with source: %v", err)
	}
	if out.Proposal == nil {
		t.Fatal("应出提案")
	}
	ids := out.Proposal.Params["media_ids"]
	if ids == nil || len(ids.([]any)) != 2 {
		t.Fatalf("source 应展开为具体 id 存 params：%+v", out.Proposal.Params)
	}
	body, _ := json.Marshal(out.Proposal.Preview.Body)
	if !strings.Contains(string(body), `"count":2`) {
		t.Fatalf("预览应含张数：%s", body)
	}
	// Execute：建册 + 加图一次完成
	res, err := findTool(t, "propose_album").Invoke(sctx, d, 1, out.Proposal.Params, ModeExecute)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Record == nil || res.Record.Kind != "album" {
		t.Fatalf("record = %+v", res.Record)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM album_items WHERE album_id = ?`, res.Record.ID).Scan(&n)
	if n != 2 {
		t.Fatalf("album items = %d, want 2", n)
	}
	// 已归属警告：把 m1 再入另一册 → 新提案应带 Warnings
	_ = db.AddAlbumItems(ctx, res.Record.ID, 1, []int64{m1})
	if err := db.AddAlbumItems(ctx, res.Record.ID, 1, []int64{m2}); err != nil {
		t.Fatal(err)
	}
	rid2, _ := db.SaveResultSet(ctx, 8, "media", []int64{m1}, nil)
	out, err = findTool(t, "propose_album").Invoke(WithSessionID(ctx, 8), d, 1,
		map[string]any{"name": "再来一册", "source": map[string]any{"result_id": rid2}}, ModePropose)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Proposal.Warnings) == 0 || !strings.Contains(out.Proposal.Warnings[0], "跳过") {
		t.Fatalf("已归属媒体应给 Warnings：%+v", out.Proposal.Warnings)
	}
}

// propose_delete：AppOnly；存在性前置校验；Execute 软删除 + 通知口径。
func TestProposeDelete(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: testNotify(t, db)}
	ctx := t.Context()
	hash, _ := auth.HashPassword("pw")
	if _, err := db.CreateUser(ctx, &store.User{Username: "boy", PasswordHash: hash, Role: "boy"}); err != nil {
		t.Fatal(err)
	}
	pID := seedPostAt(t, db, "要删的动态", "2026-08-10 10:00:00")

	out := propose(t, "propose_delete", d, 2, map[string]any{"kind": "post", "id": float64(pID)})
	if out.Proposal == nil || out.Proposal.Preview.Kind != "delete" {
		t.Fatalf("delete proposal = %+v", out.Proposal)
	}
	body, _ := json.Marshal(out.Proposal.Preview.Body)
	if !strings.Contains(string(body), "要删的动态") {
		t.Fatalf("删除预览应含对象摘要：%s", body)
	}
	exec(t, "propose_delete", d, 2, map[string]any{"kind": "post", "id": float64(pID)})
	var deleted sql.NullString
	_ = db.QueryRow(`SELECT deleted_at FROM posts WHERE id = ?`, pID).Scan(&deleted)
	if !deleted.Valid {
		t.Fatal("未软删除")
	}
	// AI 代删对方内容 → 通知创建者
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE event_type='post_deleted' AND recipient_id=1`).Scan(&n)
	if n != 1 {
		t.Fatalf("删除通知 = %d, want 1", n)
	}
	// 批量删除走整批事务：一次执行删多条，通知逐条发出（回滚守卫在 store 侧）
	q1 := seedPostAt(t, db, "批量一", "2026-08-11 10:00:00")
	q2 := seedPostAt(t, db, "批量二", "2026-08-12 10:00:00")
	exec(t, "propose_delete", d, 2, map[string]any{"kind": "post", "ids": []any{float64(q1), float64(q2)}})
	var batchN int
	_ = db.QueryRow(`SELECT COUNT(*) FROM posts WHERE id IN (?, ?) AND deleted_at IS NOT NULL`, q1, q2).Scan(&batchN)
	if batchN != 2 {
		t.Fatalf("批量删除 = %d 条，want 2", batchN)
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE event_type='post_deleted' AND recipient_id=1`).Scan(&n)
	if n != 3 {
		t.Fatalf("批量删除通知累计 = %d, want 3", n)
	}
	// 删除类通知由 AI 代操作产生 → 快照须带 via:"ai"（接收端据此判别来源）
	var delSnap string
	_ = db.QueryRow(`SELECT snapshot_json FROM notifications WHERE event_type='post_deleted' ORDER BY id DESC LIMIT 1`).Scan(&delSnap)
	if !strings.Contains(delSnap, `"via":"ai"`) {
		t.Fatalf("删除快照应带 via=ai：%s", delSnap)
	}
	// 不存在 → 前置报错（不出注定失败的卡）
	if _, err := findTool(t, "propose_delete").Invoke(ctx, d, 1, map[string]any{"kind": "post", "id": float64(999999)}, ModePropose); err == nil {
		t.Fatal("不存在的动态应报错")
	}
	if _, err := findTool(t, "propose_delete").Invoke(ctx, d, 1, map[string]any{"kind": "bogus", "id": float64(1)}, ModePropose); err == nil {
		t.Fatal("非法 kind 应报错")
	}
}

// P2-11：五类软删通知的快照都要带 via:"ai"（AI 代删），不只动态那一类。
func TestDeleteNoticesAllCarryAIMark(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: testNotify(t, db)}
	ctx := t.Context()
	hash, _ := auth.HashPassword("pw")
	if _, err := db.CreateUser(ctx, &store.User{Username: "boy", PasswordHash: hash, Role: "boy"}); err != nil {
		t.Fatal(err)
	}
	// 内容都由用户 1 创建，由 AI（发起人 user 2）删除 → 通知回创建者
	postID := seedPostAt(t, db, "要被 AI 删的动态", "2026-08-10 10:00:00")
	cmtID := seedRow(t, db, `INSERT INTO comments (post_id, author_id, content) VALUES (?, 1, '一条评论')`, postID)
	albumID := seedRow(t, db, `INSERT INTO albums (name, created_by) VALUES ('旅行', 1)`)
	annivID := seedRow(t, db, `INSERT INTO anniversaries (name, date, type, is_leap, remind_days_before, created_by)
		VALUES ('她的生日', '1996-05-15', 'solar', 0, 3, 1)`)
	pointID := seedRow(t, db, `INSERT INTO map_points (name, lat, lng, city, source, created_by)
		VALUES ('西湖', 30.2467, 120.4599, '杭州', 'manual', 1)`)

	for _, tc := range []struct {
		kind  string
		id    int64
		event string
	}{
		{"post", postID, "post_deleted"},
		{"comment", cmtID, "comment_deleted"},
		{"album", albumID, "album_deleted"},
		{"anniversary", annivID, "anniversary_deleted"},
		{"place", pointID, "map_point_deleted"},
	} {
		exec(t, "propose_delete", d, 2, map[string]any{"kind": tc.kind, "id": float64(tc.id)})
		var snap string
		if err := db.QueryRow(`SELECT snapshot_json FROM notifications WHERE event_type = ? ORDER BY id DESC LIMIT 1`, tc.event).Scan(&snap); err != nil {
			t.Fatalf("%s：未取到通知：%v", tc.kind, err)
		}
		if !strings.Contains(snap, `"via":"ai"`) {
			t.Fatalf("%s 删除快照应带 via=ai：%s", tc.kind, snap)
		}
	}
}

// propose_anniversary / propose_place 的预览与执行。
func TestProposeAnniversaryReminderPlace(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: testNotify(t, db)}
	ctx := t.Context()

	out := propose(t, "propose_anniversary", d, 1, map[string]any{
		"name": "她的生日", "date": "1996-05-15", "calendar": "lunar", "is_leap": true, "remind_days_before": float64(3)})
	body, _ := json.Marshal(out.Proposal.Preview.Body)
	for _, want := range []string{"她的生日", "1996-05-15", "lunar", `"is_leap":true`, `"remind_days_before":3`, "next_solar_date"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("纪念日预览缺 %s：%s", want, body)
		}
	}
	res := exec(t, "propose_anniversary", d, 1, out.Proposal.Params)
	if res.Record == nil || res.Record.Kind != "anniversary" {
		t.Fatalf("record = %+v", res.Record)
	}
	var isLeap int
	_ = db.QueryRow(`SELECT is_leap FROM anniversaries WHERE id = ?`, res.Record.ID).Scan(&isLeap)
	if isLeap != 1 {
		t.Fatalf("lunar is_leap 应落库 1，got %d", isLeap)
	}
	// 日期格式错误 → 前置报错
	if _, err := findTool(t, "propose_anniversary").Invoke(ctx, d, 1,
		map[string]any{"name": "x", "date": "1996/05/15"}, ModePropose); err == nil {
		t.Fatal("非法日期应报错")
	}
	// 越界提醒天数 → 前置报错（与 REST 同口径，不静默改写）
	if _, err := findTool(t, "propose_anniversary").Invoke(ctx, d, 1,
		map[string]any{"name": "x", "date": "1996-05-15", "remind_days_before": float64(91)}, ModePropose); err == nil {
		t.Fatal("越界提醒天数应报错")
	}
	// place：地名解析坐标
	out = propose(t, "propose_place", d, 1, map[string]any{"name": "西湖", "place_query": "杭州·西湖", "visit_date": "2026-08-17"})
	body, _ = json.Marshal(out.Proposal.Preview.Body)
	if !strings.Contains(string(body), `"resolved":true`) || !strings.Contains(string(body), "30.2") {
		t.Fatalf("地点预览应含解析坐标：%s", body)
	}
	res = exec(t, "propose_place", d, 1, out.Proposal.Params)
	if res.Record == nil || res.Record.Kind != "map_point" {
		t.Fatalf("record = %+v", res.Record)
	}
	// 既无坐标也无地名 → 报错
	if _, err := findTool(t, "propose_place").Invoke(ctx, d, 1, map[string]any{"name": "无名地"}, ModePropose); err == nil {
		t.Fatal("缺坐标与地名应报错")
	}
	// 地名库查不到 → 报错（不出注定失败的卡）
	if _, err := findTool(t, "propose_place").Invoke(ctx, d, 1, map[string]any{"name": "x", "place_query": "不存在的地名xyz"}, ModePropose); err == nil {
		t.Fatal("未收录地名应报错")
	}
}

// propose_comment：预览含被评论动态摘要；执行落评论并通知。
func TestProposeComment(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: testNotify(t, db)}
	ctx := t.Context()
	hash, _ := auth.HashPassword("pw")
	if _, err := db.CreateUser(ctx, &store.User{Username: "boy", PasswordHash: hash, Role: "boy"}); err != nil {
		t.Fatal(err)
	}
	pID := seedPostAt(t, db, "被评论的动态", "2026-08-10 10:00:00")

	out := propose(t, "propose_comment", d, 1, map[string]any{"post_id": float64(pID), "content": "真好呀"})
	body, _ := json.Marshal(out.Proposal.Preview.Body)
	if !strings.Contains(string(body), "被评论的动态") || !strings.Contains(string(body), "真好呀") {
		t.Fatalf("评论预览 = %s", body)
	}
	res := exec(t, "propose_comment", d, 1, out.Proposal.Params)
	if res.Record == nil || res.Record.Kind != "comment" {
		t.Fatalf("record = %+v", res.Record)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE event_type='post_commented' AND recipient_id=2`).Scan(&n)
	if n != 1 {
		t.Fatalf("评论通知 = %d, want 1", n)
	}
	// 超长评论 → 前置报错
	if _, err := findTool(t, "propose_comment").Invoke(ctx, d, 1,
		map[string]any{"post_id": float64(pID), "content": strings.Repeat("字", 501)}, ModePropose); err == nil {
		t.Fatal("超长评论应报错")
	}
}

// 审计口径：提案两个时点各记一条（出卡 pending / 裁决 success·failed·cancelled）由 chat 层记账，
// 工具 Exec 内部不得自记（防一次调用落两条）。
func TestToolsDoNotSelfAudit(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: testNotify(t, db)}
	pID := seedPostAt(t, db, "审计对象", "2026-08-10 10:00:00")
	exec(t, "propose_reaction", d, 1, map[string]any{"post_id": float64(pID), "like": true})
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_logs`).Scan(&n)
	if n != 0 {
		t.Fatalf("工具内部不应写审计，实得 %d 条", n)
	}
}

// ---- 测试辅助 ----

func seedPostAt(t *testing.T, db *store.DB, content, createdAt string) int64 {
	t.Helper()
	id, err := db.CreatePostWithMedia(t.Context(), &store.Post{AuthorID: 1, Content: content}, nil)
	if err != nil {
		t.Fatalf("seed post: %v", err)
	}
	if _, err := db.Exec("UPDATE posts SET created_at = ? WHERE id = ?", createdAt, id); err != nil {
		t.Fatalf("set created_at: %v", err)
	}
	return id
}

func seedMedia(t *testing.T, db *store.DB, postID int64, kind, takenAt string) int64 {
	t.Helper()
	res, err := db.Exec("INSERT INTO media (post_id, kind, file_path, thumb_path, taken_at) VALUES (?, ?, ?, ?, ?)",
		postID, kind, "media/x."+kind, "media/x.thumb", takenAt)
	if err != nil {
		t.Fatalf("seed media: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func seedPostMediaKind(t *testing.T, db *store.DB, postID int64, kind string) {
	t.Helper()
	seedMedia(t, db, postID, kind, "")
}

// newTestPipeline 最小媒体管线（promote 用）；数据目录独立。
func newTestPipeline(t *testing.T) *media.Pipeline {
	t.Helper()
	return media.New(t.TempDir(), []byte("0123456789abcdef0123456789abcdef"), 500<<20)
}

var _ = time.Now

// seedRow 插一行并返回自增 id（测试造数据用）。
func seedRow(t *testing.T, db *store.DB, query string, args ...any) int64 {
	t.Helper()
	res, err := db.Exec(query, args...)
	if err != nil {
		t.Fatalf("seed %q: %v", query, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return id
}

// 勾选后模型手里只有句柄、没有 post_id：评论与点赞必须能凭 source 出卡，
// 否则「勾选 → 让 AI 修改」这条路在工具层就断了（点选优先形同虚设）。
func TestProposeInteractionWithSource(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: testNotify(t, db)}
	ctx := WithSessionID(t.Context(), 7)
	p1 := seedPostAt(t, db, "海边日落", "2026-08-10 10:00:00")
	p2 := seedPostAt(t, db, "山里清晨", "2026-08-11 10:00:00")

	// 勾选产出的是「每条一个单条句柄」（r_116 / r_117 这种），照此造两个
	r1, err := db.SaveResultSet(ctx, 7, "post", []int64{p1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := db.SaveResultSet(ctx, 7, "post", []int64{p2}, nil)
	if err != nil {
		t.Fatal(err)
	}

	out, err := findTool(t, "propose_reaction").Invoke(ctx, d, 1,
		map[string]any{"like": true, "source": map[string]any{"result_id": r1}}, ModePropose)
	if err != nil {
		t.Fatalf("点赞凭句柄应出提案：%v", err)
	}
	if out.Proposal == nil {
		t.Fatal("应出点赞提案")
	}
	if got, _ := out.Proposal.Params["post_id"].(int64); got != p1 {
		t.Fatalf("source 应展开成具体 post_id，params=%+v", out.Proposal.Params)
	}

	out, err = findTool(t, "propose_comment").Invoke(ctx, d, 1,
		map[string]any{"content": "nice", "source": map[string]any{"result_id": r2}}, ModePropose)
	if err != nil {
		t.Fatalf("评论凭句柄应出提案：%v", err)
	}
	if out.Proposal == nil || out.Proposal.Intent != "propose_comment" {
		t.Fatalf("应出评论提案：%+v", out.Proposal)
	}

	// 一批塞进「一条」的卡里会变成假确认：多条的句柄要驳回并要求逐条调用
	multi, err := db.SaveResultSet(ctx, 7, "post", []int64{p1, p2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := findTool(t, "propose_reaction").Invoke(ctx, d, 1,
		map[string]any{"like": true, "source": map[string]any{"result_id": multi}}, ModePropose); err == nil {
		t.Fatal("多条的句柄应驳回")
	}
	// 既没 post_id 也没 source：错误要能照着改
	if _, err := findTool(t, "propose_comment").Invoke(ctx, d, 1,
		map[string]any{"content": "hi"}, ModePropose); err == nil {
		t.Fatal("缺目标应报错")
	}
}

// TestRegistryDocCommentMatchesRegistry 注册表注释里的数字必须与实际注册一致。
// 注释是后台工具表与文档引用的口径，数字对不上就是「读代码的人先被注释带偏」。
func TestRegistryDocCommentMatchesRegistry(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "registry.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse registry.go: %v", err)
	}

	var reads, proposals, deletes int
	for _, tl := range Registry() {
		switch {
		case tl.IsWrite && tl.AppOnly:
			deletes++
		case tl.IsWrite:
			proposals++
		default:
			reads++
		}
	}
	total := reads + proposals + deletes

	// 函数文档注释：Registry N 工具全量注册（读 A / 提案 B / 删除 C）
	var doc string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if ok && fn.Name.Name == "Registry" && fn.Doc != nil {
			doc = fn.Doc.Text()
		}
	}
	if doc == "" {
		t.Fatal("Registry 缺少文档注释")
	}
	want := fmt.Sprintf("%d 工具全量注册（读 %d / 提案 %d / 删除 %d）", total, reads, proposals, deletes)
	if !strings.Contains(doc, want) {
		t.Fatalf("Registry 注释与实际注册不符\n注释：%s\n应为：%s", strings.TrimSpace(doc), want)
	}

	// 包注释：MCP 工具注册表：N 个工具（读 A / 提案 B / 删除 C）
	if f.Doc == nil {
		t.Fatal("tools 包缺少包注释")
	}
	wantPkg := fmt.Sprintf("%d 个工具（读 %d / 提案 %d / 删除 %d）", total, reads, proposals, deletes)
	if !strings.Contains(f.Doc.Text(), wantPkg) {
		t.Fatalf("包注释与实际注册不符\n注释：%s\n应为：%s", strings.TrimSpace(f.Doc.Text()), wantPkg)
	}
}
