package tools

import (
	"database/sql"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// find_posts 的浏览 / 关键词路径共用 postHitColumns；该列清单里的作者署名
// 在昵称留空时同样要回落用户名——空串会让下游拿到无主句。
// 这里刻意让「没昵称的人」当作者：newToolDB 默认那位是有用户名的 girl，
// 拿它测不出空串问题。
func TestFindPostsHitColumnsAuthorFallsBackToUsername(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: testNotify(t, db)}
	hash, _ := auth.HashPassword("pw")
	authorID, err := db.CreateUser(t.Context(), &store.User{
		Username: "plainuser", PasswordHash: hash, Role: "boy", Nickname: "",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	postID, err := db.CreatePostWithMedia(t.Context(), &store.Post{AuthorID: authorID, Content: "没有昵称的作者"}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	// 内容时间固定，避免两条用例受写入时刻影响
	if _, err := db.Exec("UPDATE posts SET created_at = ? WHERE id = ?", "2026-11-01 10:00:00", postID); err != nil {
		t.Fatalf("set created_at: %v", err)
	}

	browse, _, err := d.browsePosts(t.Context(), postsFilter{}, authorID, 10, 0)
	if err != nil {
		t.Fatalf("browsePosts: %v", err)
	}
	keyword, _, err := d.keywordPosts(t.Context(), "没有昵称", postsFilter{}, authorID, 10, 0)
	if err != nil {
		t.Fatalf("keywordPosts: %v", err)
	}
	for name, hits := range map[string][]PostHit{"浏览": browse, "关键词": keyword} {
		var hit *PostHit
		for i := range hits {
			if hits[i].ID == postID {
				hit = &hits[i]
			}
		}
		if hit == nil {
			t.Fatalf("%s路径未命中目标动态：%+v", name, hits)
		}
		if hit.Author != "plainuser" {
			t.Errorf("%s路径作者署名 = %q，want 用户名 plainuser（空昵称时不能是空串）", name, hit.Author)
		}
	}
}

// 同一份列清单在 SQL 侧的类型也不能漂：空昵称回落的是 TEXT 而非 NULL，
// 否则扫描进 string 会直接报错。
func TestPostHitColumnsAuthorIsNeverNull(t *testing.T) {
	db := newToolDB(t)
	hash, _ := auth.HashPassword("pw")
	uid, err := db.CreateUser(t.Context(), &store.User{Username: "nonick", PasswordHash: hash, Role: "boy"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	postID, err := db.CreatePostWithMedia(t.Context(), &store.Post{AuthorID: uid, Content: "x"}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	var author sql.NullString
	row := db.QueryRow(`SELECT COALESCE(NULLIF(u.nickname, ''), u.username) FROM posts p JOIN users u ON u.id = p.author_id WHERE p.id = ?`, postID)
	if err := row.Scan(&author); err != nil {
		t.Fatalf("scan author: %v", err)
	}
	if !author.Valid || author.String != "nonick" {
		t.Fatalf("作者署名 = %+v，want 非空 nonick", author)
	}
}
