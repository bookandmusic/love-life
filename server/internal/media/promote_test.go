package media

import (
	"bytes"
	"errors"
	"testing"
)

// Promote 转正前校验：token 形状、sidecar 归属、收尾状态三者缺一不可。
func TestPromoteRejectsInvalidTokenOwnerAndUnfinalized(t *testing.T) {
	p := New(t.TempDir(), []byte("0123456789abcdef0123456789abcdef"), 1<<20)

	// 非 32 位 hex：直接拒（token 会拼进文件路径，防穿越）。
	if _, err := p.Promote("../etc/passwd", 1); !errors.Is(err, ErrUploadNotFound) {
		t.Fatalf("非法 token 应返回 ErrUploadNotFound，实得 %v", err)
	}

	// 已开启但未收尾：半成品不得转正。
	up, err := p.BeginUpload("a.wav", 1)
	if err != nil {
		t.Fatalf("BeginUpload: %v", err)
	}
	if _, err := p.Promote(up.Token, 1); err == nil {
		t.Fatal("未收尾的上传不应转正")
	}

	// 收尾后：他人调用按「不存在」拒绝，本人可转正。
	if err := p.AppendChunk(up.Token, 0, bytes.NewReader(buildWAV(t, 44100, 44100)), 1); err != nil {
		t.Fatalf("AppendChunk: %v", err)
	}
	if _, err := p.FinalizeUpload(up.Token); err != nil {
		t.Fatalf("FinalizeUpload: %v", err)
	}
	if _, err := p.Promote(up.Token, 2); !errors.Is(err, ErrUploadNotFound) {
		t.Fatalf("他人 token 应返回 ErrUploadNotFound，实得 %v", err)
	}
	got, err := p.Promote(up.Token, 1)
	if err != nil {
		t.Fatalf("本人已收尾 token 应可转正: %v", err)
	}
	if got.Kind != "audio" || got.TakenAt != "" {
		t.Fatalf("转正结果元数据不符: %+v", got)
	}
}
