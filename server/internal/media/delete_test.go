package media

import (
	"os"
	"path/filepath"
	"testing"
)

// Delete 只允许删数据目录内的文件：脏数据/构造路径不得越界删外部文件。
func TestDeleteRejectsPathOutsideDataDir(t *testing.T) {
	dir := t.TempDir()
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "keep.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := New(dir, []byte("0123456789abcdef0123456789abcdef"), 1<<20)
	p.Delete(filepath.Join("..", filepath.Base(outsideDir), "keep.txt"))
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("越界路径不应被删除: %v", err)
	}
}
