package store

import "testing"

// P2-19：role 条件必须在 store 层（不只靠 handler）——直接调 store 也不得改写 admin。
func TestStoreRejectsAdminUserUpdates(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()
	adminID, err := db.CreateUser(ctx, &User{Username: "admin", PasswordHash: "x", Role: "admin", Nickname: "管理员"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateUserNickname(ctx, adminID, "改名了"); err == nil {
		t.Fatal("store 层应拒绝改 admin 昵称")
	}
	if err := db.UpdateUserPassword(ctx, adminID, "y"); err == nil {
		t.Fatal("store 层应拒绝改 admin 密码")
	}
	if err := db.UpdateUserBirthday(ctx, adminID, "2000-01-01"); err == nil {
		t.Fatal("store 层应拒绝改 admin 生日")
	}
	if err := db.UpdateUserAvatar(ctx, adminID, "a.jpg"); err == nil {
		t.Fatal("store 层应拒绝改 admin 头像")
	}
	var nick string
	_ = db.QueryRow(`SELECT nickname FROM users WHERE id = ?`, adminID).Scan(&nick)
	if nick != "管理员" {
		t.Fatalf("admin 昵称被改写：%q", nick)
	}
}
