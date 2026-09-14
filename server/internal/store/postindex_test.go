package store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"
)

// fakePostIndex 记录 store 转发的索引调用：验证时机正确，且失败不会反向阻断落库。
type fakePostIndex struct {
	indexed   []string
	unindexed []int64
	err       error
}

func (f *fakePostIndex) IndexPost(_ context.Context, id int64, title, content, location string) error {
	f.indexed = append(f.indexed, fmt.Sprintf("%d|%s|%s|%s", id, title, content, location))
	return f.err
}

func (f *fakePostIndex) UnindexPost(_ context.Context, id int64) error {
	f.unindexed = append(f.unindexed, id)
	return f.err
}

func seedPostAuthor(t *testing.T, db *DB) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO users (username, password_hash, role) VALUES ('a', 'x', 'girl')`); err != nil {
		t.Fatalf("seed author: %v", err)
	}
}

// 索引钩子覆盖动态的三条写路径：新建、软删除、回收站恢复。
func TestPostIndexHookOnCreateDeleteRestore(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)
	idx := &fakePostIndex{}
	db.SetPostIndex(idx)
	ctx := context.Background()

	id, err := db.CreatePostWithMedia(ctx, &Post{
		AuthorID:     1,
		Title:        NullStr("今天"),
		Content:      "去看了海",
		LocationName: NullStr("青岛"),
	}, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	want := fmt.Sprintf("%d|今天|去看了海|青岛", id)
	if len(idx.indexed) != 1 || idx.indexed[0] != want {
		t.Fatalf("indexed = %v, want [%s]", idx.indexed, want)
	}

	if err := db.SoftDeletePost(ctx, id, ActorTypeUser, 1); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if len(idx.unindexed) != 1 || idx.unindexed[0] != id {
		t.Fatalf("unindexed = %v, want [%d]", idx.unindexed, id)
	}

	if _, err := db.RestoreRecycle(ctx, []int64{encodeRecycleID(0, id)}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(idx.indexed) != 2 || idx.indexed[1] != want {
		t.Fatalf("indexed after restore = %v, want second entry %s", idx.indexed, want)
	}
}

// 索引是派生数据：上游嵌入失败只告警，动态本身必须照常落库与删除。
func TestPostIndexFailureDoesNotBlockWrite(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)
	db.SetPostIndex(&fakePostIndex{err: errors.New("embedding upstream down")})
	ctx := context.Background()

	id, err := db.CreatePostWithMedia(ctx, &Post{AuthorID: 1, Content: "仍要落库"}, nil)
	if err != nil {
		t.Fatalf("create must succeed despite index failure: %v", err)
	}
	if _, err := db.GetPost(ctx, id); err != nil {
		t.Fatalf("post must be persisted: %v", err)
	}
	if err := db.SoftDeletePost(ctx, id, ActorTypeUser, 1); err != nil {
		t.Fatalf("soft delete must succeed despite unindex failure: %v", err)
	}
}

// PostsByIDs 保序返回未删动态，并跳过已删与不存在的 id（语义检索据此还原相似度序）。
func TestPostsByIDsKeepsOrderAndSkipsDeleted(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)
	ctx := context.Background()

	var ids []int64
	for _, content := range []string{"第一篇", "第二篇", "第三篇"} {
		id, err := db.CreatePostWithMedia(ctx, &Post{AuthorID: 1, Content: content}, nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		ids = append(ids, id)
	}
	if err := db.SoftDeletePost(ctx, ids[1], ActorTypeUser, 1); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	posts, err := db.PostsByIDs(ctx, []int64{ids[2], ids[1], ids[0], 9999})
	if err != nil {
		t.Fatalf("PostsByIDs: %v", err)
	}
	if len(posts) != 2 || posts[0].ID != ids[2] || posts[1].ID != ids[0] {
		t.Fatalf("posts = %+v, want [%d %d] in input order", posts, ids[2], ids[0])
	}
	if posts, err := db.PostsByIDs(ctx, nil); err != nil || posts != nil {
		t.Fatalf("PostsByIDs(nil) = %v,%v want nil,nil", posts, err)
	}
}
