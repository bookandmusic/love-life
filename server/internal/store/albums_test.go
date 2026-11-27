package store

import (
	"context"
	"strings"
	"testing"
)

// 相册可收进已单删的媒体？不行——媒体与所属动态各自软删，媒体已删时即使动态还在也要拒。
func TestCreateAlbumRejectsSoftDeletedMedia(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)
	ctx := t.Context()

	postID := seedLivePost(t, db)
	mediaID := seedPhoto(t, db, postID, "media/a.jpg")
	// 动态仍在（有正文），只单删这一张媒体
	if _, postGone, _, err := db.SoftDeleteMedia(ctx, mediaID, ActorTypeUser, 1); err != nil {
		t.Fatalf("soft delete media: %v", err)
	} else if postGone {
		t.Fatal("有正文的动态不应随单张媒体一起删")
	}

	_, err = db.CreateAlbumWithItems(ctx, "旅行", "", 1, []int64{mediaID})
	if err == nil {
		t.Fatal("已单删的媒体不应能加入相册")
	}
	if !strings.Contains(err.Error(), "已删除") {
		t.Fatalf("错误文案应说明媒体已删除，实得 %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM albums`).Scan(&n); err != nil {
		t.Fatalf("count albums: %v", err)
	}
	if n != 0 {
		t.Fatal("加媒体失败时相册应随事务回滚")
	}
}

// 时间轴是「全部媒体」的全景视图：动态里发的图与评论里附的图都要出现。
// 只有「所属实体已删」或「已归入活跃相册」才排除。
func TestTimelineIncludesCommentMedia(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	if _, err := db.Exec(`INSERT INTO users (username, password_hash, role) VALUES ('u','x','girl')`); err != nil {
		t.Fatal(err)
	}
	postID, err := db.CreatePostWithMedia(ctx, &Post{AuthorID: 1, Content: "动态"}, []MediaRow{
		{Kind: "photo", FilePath: "media/post.jpg"},
	})
	if err != nil {
		t.Fatal(err)
	}
	commentID, err := db.CreateCommentWithMedia(ctx, postID, 1, "评论", []MediaRow{
		{Kind: "photo", FilePath: "media/comment.jpg"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 时间轴：两张都要在
	list, total, err := db.ListTimelineMediaPaged(ctx, 1, 50, false)
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("时间轴应含动态图与评论图共 2 张，实得 total=%d len=%d", total, len(list))
	}
	var sawPost, sawComment bool
	for _, m := range list {
		switch m.OwnerType {
		case MediaOwnerPost:
			sawPost = true
		case MediaOwnerComment:
			sawComment = true
			// 评论图的「所属动态」要能解出来（供按动态跳转）
			if m.PostID != postID {
				t.Errorf("评论图的 post_id 应为 %d，实得 %d", postID, m.PostID)
			}
		}
	}
	if !sawPost || !sawComment {
		t.Fatalf("两类归属都应出现：post=%v comment=%v", sawPost, sawComment)
	}

	// 评论软删后，它的图随评论一起从时间轴消失
	if err := db.SoftDeleteComment(ctx, commentID, ActorTypeUser, 1); err != nil {
		t.Fatalf("soft delete comment: %v", err)
	}
	_, total, err = db.ListTimelineMediaPaged(ctx, 1, 50, false)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("评论软删后时间轴应只剩动态图，实得 total=%d", total)
	}

	// 评论图不能进相册（随评论走）：即使评论还在也不行
	commentID2, err := db.CreateCommentWithMedia(ctx, postID, 1, "再评", []MediaRow{
		{Kind: "photo", FilePath: "media/c2.jpg"},
	})
	if err != nil {
		t.Fatal(err)
	}
	byComment, _ := db.ListMediaByCommentIDs(ctx, []int64{commentID2})
	albumID, err := db.CreateAlbum(ctx, "相册", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AddAlbumItems(ctx, albumID, 1, []int64{byComment[commentID2][0].ID}); err == nil {
		t.Fatal("评论图不应能加入相册")
	}
}

// 「评论图不进相册」有两半：加入时拒绝（已有测试）+ 候选池不列出。
// 只测前者的话，选图器照样会把评论图摆出来让用户勾选，提交才 400——
// 用户以为能选，点了才被告知不行。
func TestAlbumCandidatePoolsExcludeCommentMedia(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	if _, err := db.Exec(`INSERT INTO users (username, password_hash, role) VALUES ('u','x','girl')`); err != nil {
		t.Fatal(err)
	}
	postID, err := db.CreatePostWithMedia(ctx, &Post{AuthorID: 1, Content: "动态"}, []MediaRow{
		{Kind: "photo", FilePath: "media/post.jpg"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateCommentWithMedia(ctx, postID, 1, "评论", []MediaRow{
		{Kind: "photo", FilePath: "media/comment.jpg"},
	}); err != nil {
		t.Fatal(err)
	}

	// 三条候选池路径都必须只剩动态图
	check := func(name string, got int) {
		t.Helper()
		if got != 1 {
			t.Errorf("%s 应只含动态图（1 张），实得 %d——评论图被列进了相册候选池", name, got)
		}
	}

	// 1) 选图器口径（includeFiled=true，全部媒体）
	_, total, err := db.ListTimelineMediaPaged(ctx, 1, 50, true)
	if err != nil {
		t.Fatal(err)
	}
	check("ListTimelineMediaPaged(includeFiled=true)", total)

	// 2) 按日期/类型分组的候选
	cands, err := db.ListMediaGroupCandidates(ctx, "", "", "", "photo")
	if err != nil {
		t.Fatal(err)
	}
	check("ListMediaGroupCandidates", len(cands))

	// 3) find_media 的通用过滤（未归档口径）
	items, err := db.ListMediaFiltered(ctx, MediaFilter{Unfiled: true}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	check("ListMediaFiltered(Unfiled)", len(items))
	cnt, err := db.CountMediaFiltered(ctx, MediaFilter{Unfiled: true})
	if err != nil {
		t.Fatal(err)
	}
	check("CountMediaFiltered(Unfiled)", cnt)
}
