package store

import (
	"errors"
	"testing"
)

// 单条媒体删除：三种情形（还有媒体 / 无媒体有正文 / 无媒体无正文）+ 权限由 API 层负责。
func TestSoftDeleteMedia(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)
	ctx := t.Context()

	t.Run("还有别的媒体：只删这个文件，动态保留", func(t *testing.T) {
		postID := seedSoftDelete(t, db, `INSERT INTO posts (author_id, content) VALUES (1, '')`)
		keep := seedPhoto(t, db, postID, "media/keep.jpg")
		gone := seedPhoto(t, db, postID, "media/gone.jpg")

		gotPost, postGone, err := db.SoftDeleteMedia(ctx, gone, ActorTypeUser, 1)
		if err != nil {
			t.Fatalf("SoftDeleteMedia: %v", err)
		}
		if gotPost != postID || postGone {
			t.Fatalf("期望 post=%d 且动态保留，得到 post=%d gone=%v", postID, gotPost, postGone)
		}
		if n := purgeCount(t, db, `SELECT COUNT(*) FROM media WHERE id = ? AND deleted_at IS NULL`, gone); n != 0 {
			t.Fatal("被删媒体应已软删")
		}
		if n := purgeCount(t, db, `SELECT COUNT(*) FROM media WHERE id = ? AND deleted_at IS NULL`, keep); n != 1 {
			t.Fatal("兄弟媒体不应受影响")
		}
		if n := purgeCount(t, db, `SELECT COUNT(*) FROM posts WHERE id = ? AND deleted_at IS NULL`, postID); n != 1 {
			t.Fatal("还有媒体时动态必须保留")
		}
	})

	t.Run("无媒体但有正文：只删这个文件，动态保留", func(t *testing.T) {
		postID := seedSoftDelete(t, db, `INSERT INTO posts (author_id, content) VALUES (1, '只有一句话')`)
		only := seedPhoto(t, db, postID, "media/only.jpg")

		_, postGone, err := db.SoftDeleteMedia(ctx, only, ActorTypeUser, 1)
		if err != nil {
			t.Fatalf("SoftDeleteMedia: %v", err)
		}
		if postGone {
			t.Fatal("正文非空时动态必须保留")
		}
		if n := purgeCount(t, db, `SELECT COUNT(*) FROM posts WHERE id = ? AND deleted_at IS NULL`, postID); n != 1 {
			t.Fatal("正文非空时动态必须保留")
		}
	})

	t.Run("无媒体且正文为空：连带删动态", func(t *testing.T) {
		postID := seedSoftDelete(t, db, `INSERT INTO posts (author_id, content) VALUES (1, '   ')`)
		only := seedPhoto(t, db, postID, "media/last.jpg")

		_, postGone, err := db.SoftDeleteMedia(ctx, only, ActorTypeUser, 1)
		if err != nil {
			t.Fatalf("SoftDeleteMedia: %v", err)
		}
		if !postGone {
			t.Fatal("无媒体且正文为空时必须连带删动态（含纯空白正文）")
		}
		if n := purgeCount(t, db, `SELECT COUNT(*) FROM posts WHERE id = ? AND deleted_at IS NOT NULL`, postID); n != 1 {
			t.Fatal("动态应已软删")
		}
	})

	t.Run("重复删除返回 ErrNotFound", func(t *testing.T) {
		postID := seedSoftDelete(t, db, `INSERT INTO posts (author_id, content) VALUES (1, 'x')`)
		m := seedPhoto(t, db, postID, "media/twice.jpg")
		if _, _, err := db.SoftDeleteMedia(ctx, m, ActorTypeUser, 1); err != nil {
			t.Fatalf("首次删除: %v", err)
		}
		if _, _, err := db.SoftDeleteMedia(ctx, m, ActorTypeUser, 1); !errors.Is(err, ErrNotFound) {
			t.Fatalf("重复删除应 ErrNotFound，得到 %v", err)
		}
	})
}

// 回收站：单独删的媒体要能列出并可恢复；随动态删的媒体不重复列出。
func TestRecycleListsDetachedMediaOnly(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)
	ctx := t.Context()

	// 1) 动态 A：两张图，单独删一张 → 该图进回收站，动态不受影响
	postA := seedSoftDelete(t, db, `INSERT INTO posts (author_id, content) VALUES (1, '')`)
	keepA := seedPhoto(t, db, postA, "media/a1.jpg")
	goneA := seedPhoto(t, db, postA, "media/a2.jpg")
	_ = keepA
	if _, _, err := db.SoftDeleteMedia(ctx, goneA, ActorTypeUser, 1); err != nil {
		t.Fatalf("SoftDeleteMedia: %v", err)
	}

	// 2) 动态 B：整条软删（媒体级联）
	postB := seedSoftDelete(t, db, `INSERT INTO posts (author_id, content) VALUES (1, '整条删')`)
	mB := seedPhoto(t, db, postB, "media/b1.jpg")
	if err := db.SoftDeletePost(ctx, postB, ActorTypeUser, 1); err != nil {
		t.Fatalf("SoftDeletePost: %v", err)
	}
	_ = mB

	items, _, err := db.ListRecycle(ctx, 100, 0)
	if err != nil {
		t.Fatalf("ListRecycle: %v", err)
	}
	mediaCount, postCount := 0, 0
	var mediaItem *RecycleItem
	for _, it := range items {
		switch it.Type {
		case "照片":
			mediaCount++
			mediaItem = it
		case "动态":
			postCount++
		}
	}
	if mediaCount != 1 {
		t.Fatalf("回收站应只列 1 条单独删的照片（随动态删的不单列），实际 %d", mediaCount)
	}
	if postCount != 1 {
		t.Fatalf("整条删除的动态应列出 1 条，实际 %d", postCount)
	}

	// 恢复该照片：动态 A 重新有这张图
	res, err := db.RestoreRecycle(ctx, []int64{mediaItem.ID})
	if err != nil {
		t.Fatalf("RestoreRecycle media: %v", err)
	}
	if res.Restored != 1 {
		t.Fatalf("应恢复 1 条，实际 %d", res.Restored)
	}
	if n := purgeCount(t, db, `SELECT COUNT(*) FROM media WHERE id = ? AND deleted_at IS NULL`, goneA); n != 1 {
		t.Fatal("恢复后媒体应重新可见")
	}
}
