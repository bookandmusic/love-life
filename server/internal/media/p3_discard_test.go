package media

import (
	"os"
	"path/filepath"
	"testing"
)

// 主动撤销的 sidecar 归属校验：**「缺失」与「损坏」分开处理**——
//   - sidecar 不存在（可能已被 janitor 清理）→ 仍放行删除，属正常收敛；
//   - sidecar 存在但解析失败 / 缺归属 → 拒绝删除（被篡改或写坏的信号）。
//
// 若把「缺失」也一并拒绝，过期的放弃/重试路径会留下删不掉的临时文件。
func TestDiscardDistinguishesMissingAndCorruptSidecar(t *testing.T) {
	token := "0123456789abcdef0123456789abcdef"

	setup := func(t *testing.T) (*Pipeline, string, string) {
		t.Helper()
		dir := t.TempDir()
		p := New(dir, []byte("0123456789abcdef0123456789abcdef"), 1<<20)
		tmpDir := filepath.Join(dir, "media", "tmp")
		if err := os.MkdirAll(tmpDir, 0o755); err != nil {
			t.Fatal(err)
		}
		raw := filepath.Join(tmpDir, token+".jpg")
		if err := os.WriteFile(raw, []byte("junk"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p, raw, filepath.Join(tmpDir, token+".json")
	}

	// ① sidecar 不存在 → 放行删除（sidecar 变量供后续用例复用）
	p, raw, sidecar := setup(t)
	_ = sidecar
	if n := p.Discard([]string{token}, 1); n != 1 {
		t.Fatalf("sidecar 缺失时应放行删除，removed=%d", n)
	}
	if _, err := os.Stat(raw); !os.IsNotExist(err) {
		t.Fatalf("临时文件应已删除：%v", err)
	}

	// ② sidecar 存在但解析失败 → 拒绝删除
	p, raw, sidecar = setup(t)
	if err := os.WriteFile(sidecar, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if n := p.Discard([]string{token}, 1); n != 0 {
		t.Fatalf("sidecar 损坏时应拒绝删除，removed=%d", n)
	}
	if _, err := os.Stat(raw); err != nil {
		t.Fatalf("损坏 sidecar 对应的临时文件应保留：%v", err)
	}

	// ③ sidecar 存在但缺归属（OwnerID=0）→ 拒绝删除
	p, raw, sidecar = setup(t)
	if err := os.WriteFile(sidecar, []byte(`{"kind":"photo"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if n := p.Discard([]string{token}, 1); n != 0 {
		t.Fatalf("sidecar 缺归属时应拒绝删除，removed=%d", n)
	}
	if _, err := os.Stat(raw); err != nil {
		t.Fatalf("缺归属 sidecar 对应的临时文件应保留：%v", err)
	}

	// ④ 正常 sidecar 且归属他人 → 跳过（不删、不报错）
	p, raw, sidecar = setup(t)
	if err := os.WriteFile(sidecar, []byte(`{"kind":"photo","owner_id":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if n := p.Discard([]string{token}, 1); n != 0 {
		t.Fatalf("归属他人应跳过，removed=%d", n)
	}
	if _, err := os.Stat(raw); err != nil {
		t.Fatalf("他人临时文件应保留：%v", err)
	}

	// ⑤ 正常 sidecar 且归属本人 → 删除（含 sidecar）
	if n := p.Discard([]string{token}, 2); n != 1 {
		t.Fatalf("归属本人应删除，removed=%d", n)
	}
	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Fatalf("sidecar 应随临时文件一并清理：%v", err)
	}
}

// sidecar 存在但**读不出来**（不是「不存在」）：同样属损坏信号，必须拒绝删除。
// 这一条是本次改动真正的行为增量——原实现只在「读成功」时校验归属，
// 任何读取错误（除不存在外）都会被静默放行，把文件删掉。
func TestDiscardRefusesOnUnreadableSidecar(t *testing.T) {
	dir := t.TempDir()
	p := New(dir, []byte("0123456789abcdef0123456789abcdef"), 1<<20)
	token := "0123456789abcdef0123456789abcdef"
	tmpDir := filepath.Join(dir, "media", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := filepath.Join(tmpDir, token+".jpg")
	if err := os.WriteFile(raw, []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 把 sidecar 路径做成目录 → os.ReadFile 返回 EISDIR（非 ErrNotExist）
	if err := os.Mkdir(filepath.Join(tmpDir, token+".json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if n := p.Discard([]string{token}, 1); n != 0 {
		t.Fatalf("sidecar 读不出时应拒绝删除，removed=%d", n)
	}
	if _, err := os.Stat(raw); err != nil {
		t.Fatalf("读不出 sidecar 对应的临时文件应保留：%v", err)
	}
}
