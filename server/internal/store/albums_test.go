package store

import (
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
	if _, postGone, err := db.SoftDeleteMedia(ctx, mediaID, ActorTypeUser, 1); err != nil {
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
