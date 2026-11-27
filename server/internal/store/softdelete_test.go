package store

import (
	"context"
	"testing"
)

func seedSoftDelete(t *testing.T, db *DB, query string, args ...any) int64 {
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

func seedLivePost(t *testing.T, db *DB) int64 {
	t.Helper()
	return seedSoftDelete(t, db, `INSERT INTO posts (author_id, content) VALUES (1, '待删动态')`)
}

func seedPhoto(t *testing.T, db *DB, postID int64, path string) int64 {
	t.Helper()
	return seedSoftDelete(t, db,
		`INSERT INTO media (owner_type, owner_id, kind, file_path, thumb_path) VALUES ('post', ?, 'photo', ?, ?)`, postID, path, path+".thumb")
}

// P2-13：相册软删的「更新 albums」与「释放 album_items」同事务——释放失败时软删也不落。
// 用删表构造确定性失败（真实库的 DELETE 不会失败，无法用数据构造）。
func TestSoftDeleteAlbumRollsBackWhenReleaseFails(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)
	ctx := t.Context()

	albumID := seedSoftDelete(t, db, `INSERT INTO albums (name, created_by) VALUES ('旅行', 1)`)
	if _, err := db.ExecContext(ctx, `DROP TABLE album_items`); err != nil {
		t.Fatalf("drop album_items: %v", err)
	}
	if err := db.SoftDeleteAlbum(ctx, albumID, ActorTypeUser, 1); err == nil {
		t.Fatal("album_items 缺失时软删相册应报错")
	}
	if n := purgeCount(t, db, `SELECT COUNT(*) FROM albums WHERE id = ? AND deleted_at IS NOT NULL`, albumID); n != 0 {
		t.Fatal("释放失败时软删必须回滚（两处改动须在同一事务）")
	}
}

// P2-13：软删相册即释放媒体归属（语义不变），且该媒体随后能加入另一相册并真的落 1 行
// ——若 items 残留，UNIQUE(media_id) 会让插入静默变成 0 行。
func TestSoftDeleteAlbumReleasesMediaOwnership(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)
	ctx := t.Context()

	postID := seedLivePost(t, db)
	mediaID := seedPhoto(t, db, postID, "media/a.jpg")
	albumA := seedSoftDelete(t, db, `INSERT INTO albums (name, created_by) VALUES ('A', 1)`)
	seedSoftDelete(t, db, `INSERT INTO album_items (album_id, media_id, added_by) VALUES (?, ?, 1)`, albumA, mediaID)

	if err := db.SoftDeleteAlbum(ctx, albumA, ActorTypeUser, 1); err != nil {
		t.Fatalf("soft delete album: %v", err)
	}
	if n := purgeCount(t, db, `SELECT COUNT(*) FROM album_items WHERE album_id = ?`, albumA); n != 0 {
		t.Fatalf("软删后相册 items = %d，want 0（软删即释放归属）", n)
	}

	albumB := seedSoftDelete(t, db, `INSERT INTO albums (name, created_by) VALUES ('B', 1)`)
	if err := db.AddAlbumItems(ctx, albumB, 1, []int64{mediaID}); err != nil {
		t.Fatalf("add to another album: %v", err)
	}
	if n := purgeCount(t, db, `SELECT COUNT(*) FROM album_items WHERE album_id = ?`, albumB); n != 1 {
		t.Fatalf("加入另一相册后 items = %d，want 1", n)
	}
}

// P2-4：整批软删在同一事务内——第 2 条失败（不存在）时第 1 条也不落。
// 工具层在 Execute 前先做存在性校验，失败无法从工具入口构造，故守卫落在 store。
func TestSoftDeleteBatchRollsBackOnFailure(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)
	ctx := t.Context()

	p1 := seedLivePost(t, db)
	media1 := seedPhoto(t, db, p1, "media/a.jpg")
	p2 := seedLivePost(t, db)

	if err := db.SoftDeleteBatch(ctx, "post", []int64{p1, 999999}, ActorTypeAI, 1); err == nil {
		t.Fatal("含不存在记录的整批删除应报错")
	}
	if n := purgeCount(t, db, `SELECT COUNT(*) FROM posts WHERE id = ? AND deleted_at IS NOT NULL`, p1); n != 0 {
		t.Fatal("第 2 条失败时第 1 条必须回滚")
	}
	if n := purgeCount(t, db, `SELECT COUNT(*) FROM media WHERE id = ? AND deleted_at IS NOT NULL`, media1); n != 0 {
		t.Fatal("回滚须覆盖级联媒体")
	}

	if err := db.SoftDeleteBatch(ctx, "post", []int64{p1, p2}, ActorTypeAI, 1); err != nil {
		t.Fatalf("整批删除: %v", err)
	}
	if n := purgeCount(t, db, `SELECT COUNT(*) FROM posts WHERE deleted_at IS NOT NULL`); n != 2 {
		t.Fatalf("成功整批后已删动态 = %d，want 2", n)
	}
}

// P2-4：建册与首批媒体同事务——媒体不存在时相册也不落，不留「已建册但没有媒体」的半成品。
func TestCreateAlbumWithItemsRollsBack(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)
	ctx := t.Context()

	if _, err := db.CreateAlbumWithItems(ctx, "旅行", "", 1, []int64{999999}); err == nil {
		t.Fatal("含不存在媒体的建册应报错")
	}
	if n := purgeCount(t, db, `SELECT COUNT(*) FROM albums`); n != 0 {
		t.Fatalf("加媒体失败后相册 = %d，want 0", n)
	}

	postID := seedLivePost(t, db)
	mediaID := seedPhoto(t, db, postID, "media/a.jpg")
	albumID, err := db.CreateAlbumWithItems(ctx, "旅行", "", 1, []int64{mediaID})
	if err != nil {
		t.Fatalf("建册: %v", err)
	}
	if n := purgeCount(t, db, `SELECT COUNT(*) FROM album_items WHERE album_id = ?`, albumID); n != 1 {
		t.Fatalf("建册后 items = %d，want 1", n)
	}
}

// P2-4：相册增删同事务——移出失败时加入也回滚（一次确认 = 一个原子步）。
// 用 BEFORE DELETE 触发器构造确定性失败（移出是纯 DELETE，无数据能令其失败）。
func TestUpdateAlbumMembersRollsBack(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)
	ctx := t.Context()

	postID := seedLivePost(t, db)
	m2 := seedPhoto(t, db, postID, "media/a.jpg")
	m3 := seedPhoto(t, db, postID, "media/b.jpg")
	albumA := seedSoftDelete(t, db, `INSERT INTO albums (name, created_by) VALUES ('A', 1)`)
	seedSoftDelete(t, db, `INSERT INTO album_items (album_id, media_id, added_by) VALUES (?, ?, 1)`, albumA, m2)
	if _, err := db.ExecContext(ctx,
		`CREATE TRIGGER block_album_item_delete BEFORE DELETE ON album_items BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	if err := db.UpdateAlbumMembers(ctx, albumA, 1, []int64{m3}, []int64{m2}); err == nil {
		t.Fatal("移出失败时整理相册应报错")
	}
	if n := purgeCount(t, db, `SELECT COUNT(*) FROM album_items WHERE album_id = ? AND media_id = ?`, albumA, m2); n != 1 {
		t.Fatal("移出失败时既有的加入也必须回滚")
	}
	if n := purgeCount(t, db, `SELECT COUNT(*) FROM album_items WHERE album_id = ? AND media_id = ?`, albumA, m3); n != 0 {
		t.Fatal("移出失败时加入必须回滚")
	}
}

// 评论软删级联其图片；恢复评论时图片一并恢复（图随评论走，不能留半空评论）。
func TestSoftDeleteCommentCascadesMedia(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	seedSoftDelete(t, db, `INSERT INTO users (username, password_hash, role) VALUES ('u', 'x', 'girl')`)

	postID := seedLivePost(t, db)
	commentID, err := db.CreateCommentWithMedia(ctx, postID, 1, "带图的评论", []MediaRow{
		{Kind: "photo", FilePath: "media/c1.jpg", ThumbPath: NullStr("media/c1.thumb")},
		{Kind: "photo", FilePath: "media/c2.jpg", ThumbPath: NullStr("media/c2.thumb")},
	})
	if err != nil {
		t.Fatalf("create comment with media: %v", err)
	}

	// 评论图归属 comment，且带得出媒体列表
	c, err := db.GetComment(ctx, commentID)
	if err != nil {
		t.Fatalf("get comment: %v", err)
	}
	if len(c.Media) != 2 {
		t.Fatalf("评论应有 2 张图，实得 %d", len(c.Media))
	}
	for _, m := range c.Media {
		if m.OwnerType != MediaOwnerComment || m.OwnerID != commentID {
			t.Fatalf("归属应为 (%s,%d)，实得 (%s,%d)", MediaOwnerComment, commentID, m.OwnerType, m.OwnerID)
		}
	}

	// 软删评论 → 图一起软删
	if err := db.SoftDeleteComment(ctx, commentID, ActorTypeUser, 1); err != nil {
		t.Fatalf("soft delete comment: %v", err)
	}
	var alive int
	if err := db.QueryRow(`SELECT COUNT(*) FROM media WHERE owner_type = 'comment' AND owner_id = ? AND deleted_at IS NULL`, commentID).Scan(&alive); err != nil {
		t.Fatal(err)
	}
	if alive != 0 {
		t.Fatalf("评论软删后其图应一并软删，仍有 %d 张未删", alive)
	}

	// 恢复评论 → 图一起回来
	// 评论在 recycleSources 里排第 2（索引 1）：恢复要按合成 id 提交
	if _, err := db.RestoreRecycle(ctx, []int64{encodeRecycleID(1, commentID)}); err != nil {
		t.Fatalf("restore comment: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM media WHERE owner_type = 'comment' AND owner_id = ? AND deleted_at IS NULL`, commentID).Scan(&alive); err != nil {
		t.Fatal(err)
	}
	if alive != 2 {
		t.Fatalf("恢复评论后应有 2 张图可见，实得 %d", alive)
	}
}

// 评论里的图不能加进相册（随评论走，进相册会在删评论后留下孤儿归属）。
func TestCommentMediaCannotJoinAlbum(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	seedSoftDelete(t, db, `INSERT INTO users (username, password_hash, role) VALUES ('u', 'x', 'girl')`)

	postID := seedLivePost(t, db)
	commentID, err := db.CreateCommentWithMedia(ctx, postID, 1, "图", []MediaRow{
		{Kind: "photo", FilePath: "media/cm.jpg"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var mediaID int64
	if err := db.QueryRow(`SELECT id FROM media WHERE owner_type = 'comment' AND owner_id = ?`, commentID).Scan(&mediaID); err != nil {
		t.Fatal(err)
	}
	albumID, err := db.CreateAlbum(ctx, "旅行", "", 1)
	if err != nil {
		t.Fatalf("create album: %v", err)
	}
	if err := db.AddAlbumItems(ctx, albumID, 1, []int64{mediaID}); err == nil {
		t.Fatal("评论图不应能加入相册")
	}
}

// 单独删掉的评论图必须能在回收站里找到并恢复。
// 照片源若只认 owner_type='post'，评论图既不在照片里、也没有评论条目
// （评论本身没删），就会彻底消失，用户无从恢复。
func TestCommentMediaAppearsInRecycle(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	seedSoftDelete(t, db, `INSERT INTO users (username, password_hash, role) VALUES ('u', 'x', 'girl')`)

	postID := seedLivePost(t, db)
	commentID, err := db.CreateCommentWithMedia(ctx, postID, 1, "有正文", []MediaRow{
		{Kind: "photo", FilePath: "media/c1.jpg"}, {Kind: "photo", FilePath: "media/c2.jpg"},
	})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	byC, err := db.ListMediaByCommentIDs(ctx, []int64{commentID})
	if err != nil {
		t.Fatal(err)
	}
	firstID := byC[commentID][0].ID

	// 只删第一张（评论保留）
	if _, _, _, err := db.SoftDeleteMedia(ctx, firstID, ActorTypeUser, 1); err != nil {
		t.Fatalf("delete one comment media: %v", err)
	}

	items, total, err := db.ListRecycle(ctx, 50, 0)
	if err != nil {
		t.Fatalf("list recycle: %v", err)
	}
	// 只应有被删的那一张，另一张未删不能混进来
	if total != 1 || len(items) != 1 {
		t.Fatalf("回收站应有 1 条（只删了一张），实得 total=%d len=%d", total, len(items))
	}
	if items[0].Type != "照片" {
		t.Fatalf("评论图应归入「照片」类，实得 %q", items[0].Type)
	}

	// 能恢复：恢复后该图重新可见
	if _, err := db.RestoreRecycle(ctx, []int64{items[0].ID}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	after, err := db.ListMediaByCommentIDs(ctx, []int64{commentID})
	if err != nil {
		t.Fatal(err)
	}
	if len(after[commentID]) != 2 {
		t.Fatalf("恢复后评论应重新有 2 张图，实得 %d", len(after[commentID]))
	}
}

// 评论整条被删时，它的图随评论条目整体恢复，不单独列出（避免同一张图出现两次）。
func TestCommentMediaNotListedWhenCommentDeleted(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	seedSoftDelete(t, db, `INSERT INTO users (username, password_hash, role) VALUES ('u', 'x', 'girl')`)
	postID := seedLivePost(t, db)

	commentID, err := db.CreateCommentWithMedia(ctx, postID, 1, "带图评论", []MediaRow{
		{Kind: "photo", FilePath: "media/cm.jpg"},
	})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if err := db.SoftDeleteComment(ctx, commentID, ActorTypeUser, 1); err != nil {
		t.Fatalf("soft delete comment: %v", err)
	}

	items, _, err := db.ListRecycle(ctx, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Type == "照片" {
			t.Fatalf("评论整体删除时其图不应单列为照片（随评论条目恢复），实得 %+v", it)
		}
	}
}

// 恢复父实体不能复活用户先前单独删掉的图。
// 「删了一张图」与「删整条动态」是两个独立意图，恢复动态只该捞回因删动态而走的那些。
func TestRestoreParentDoesNotReviveSeparatelyDeletedMedia(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	seedSoftDelete(t, db, `INSERT INTO users (username, password_hash, role) VALUES ('u', 'x', 'girl')`)

	// —— 动态场景 ——
	postID, err := db.CreatePostWithMedia(ctx, &Post{AuthorID: 1, Content: "有正文的动态"}, []MediaRow{
		{Kind: "photo", FilePath: "media/p1.jpg"}, {Kind: "photo", FilePath: "media/p2.jpg"},
	})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	byP, _ := db.GetMediaByPostIDs(ctx, []int64{postID})
	removedFirst := byP[postID][0].ID
	// 先单删一张
	if _, _, _, err := db.SoftDeleteMedia(ctx, removedFirst, ActorTypeUser, 1); err != nil {
		t.Fatalf("delete one: %v", err)
	}
	// 再删整条动态，然后恢复
	if err := db.SoftDeletePost(ctx, postID, ActorTypeUser, 1); err != nil {
		t.Fatalf("delete post: %v", err)
	}
	if _, err := db.RestoreRecycle(ctx, []int64{encodeRecycleID(0, postID)}); err != nil {
		t.Fatalf("restore post: %v", err)
	}
	after, _ := db.GetMediaByPostIDs(ctx, []int64{postID})
	if len(after[postID]) != 1 {
		t.Fatalf("恢复动态后应只看到 1 张图（单删的那张不该复活），实得 %d", len(after[postID]))
	}
	if after[postID][0].ID == removedFirst {
		t.Fatal("复活的是那张被单独删掉的图")
	}

	// —— 评论场景（同一口径）——
	commentID, err := db.CreateCommentWithMedia(ctx, postID, 1, "有正文", []MediaRow{
		{Kind: "photo", FilePath: "media/c1.jpg"}, {Kind: "photo", FilePath: "media/c2.jpg"},
	})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	byC, _ := db.ListMediaByCommentIDs(ctx, []int64{commentID})
	removedC := byC[commentID][0].ID
	if _, _, _, err := db.SoftDeleteMedia(ctx, removedC, ActorTypeUser, 1); err != nil {
		t.Fatalf("delete one comment media: %v", err)
	}
	if err := db.SoftDeleteComment(ctx, commentID, ActorTypeUser, 1); err != nil {
		t.Fatalf("delete comment: %v", err)
	}
	if _, err := db.RestoreRecycle(ctx, []int64{encodeRecycleID(1, commentID)}); err != nil {
		t.Fatalf("restore comment: %v", err)
	}
	afterC, _ := db.ListMediaByCommentIDs(ctx, []int64{commentID})
	if len(afterC[commentID]) != 1 {
		t.Fatalf("恢复评论后应只看到 1 张图，实得 %d", len(afterC[commentID]))
	}
}
