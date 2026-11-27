package store

import (
	"context"
	"testing"
)

// 昵称留空时作者署名必须回退用户名：`COALESCE(nickname, username)` 只在 NULL 时生效，
// 空串会把「小六 赞了对方的动态」这类残句和空作者名一路带到通知与时间轴。
func TestEmptyNicknameFallsBackToUsername(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	uid, err := db.CreateUser(ctx, &User{Username: "plainuser", PasswordHash: "x", Role: "girl"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	postID, err := db.CreatePostWithMedia(ctx, &Post{AuthorID: uid, Content: "hi"}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	commentID, err := db.CreateCommentWithMedia(ctx, postID, uid, "自己评论", nil)
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}

	p, err := db.GetPost(ctx, postID)
	if err != nil {
		t.Fatalf("get post: %v", err)
	}
	if p.AuthorNickname.String != "plainuser" {
		t.Errorf("动态作者署名 = %q，want 用户名 plainuser", p.AuthorNickname.String)
	}
	c, err := db.GetComment(ctx, commentID)
	if err != nil {
		t.Fatalf("get comment: %v", err)
	}
	if c.AuthorNickname.String != "plainuser" {
		t.Errorf("评论作者署名 = %q，want 用户名 plainuser", c.AuthorNickname.String)
	}
}

// 点赞者列表同口径：两人都没昵称时，缓存列表里不能出现空名字。
func TestEmptyNicknameFallsBackToUsernameInLikers(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	author, err := db.CreateUser(ctx, &User{Username: "author", PasswordHash: "x", Role: "girl"})
	if err != nil {
		t.Fatalf("create author: %v", err)
	}
	liker, err := db.CreateUser(ctx, &User{Username: "liker", PasswordHash: "x", Role: "boy"})
	if err != nil {
		t.Fatalf("create liker: %v", err)
	}
	postID, err := db.CreatePostWithMedia(ctx, &Post{AuthorID: author, Content: "hi"}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	if _, err := db.SetLike(ctx, postID, liker, true); err != nil {
		t.Fatalf("like: %v", err)
	}

	likers, err := db.LikersByPostIDs(ctx, []int64{postID})
	if err != nil {
		t.Fatalf("likers: %v", err)
	}
	if got := likers[postID]; len(got) != 1 || got[0].Nickname != "liker" {
		t.Fatalf("点赞者署名 = %+v，want 用户名 liker", got)
	}
}
