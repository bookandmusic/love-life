package store

import (
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
		`INSERT INTO media (post_id, kind, file_path, thumb_path) VALUES (?, 'photo', ?, ?)`, postID, path, path+".thumb")
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
