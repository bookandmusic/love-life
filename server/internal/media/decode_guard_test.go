package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// pngDeclaring 造一个只有文件头的 PNG：几 KB 的文件声称 w×h 像素。
// 这正是解压炸弹的形态——整图解码按声明尺寸分配内存，与文件大小无关。
func pngDeclaring(t *testing.T, w, h uint32) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 2 // 8 位深、真彩色
	for _, chunk := range []struct {
		typ  string
		data []byte
	}{{"IHDR", ihdr}, {"IEND", nil}} {
		_ = binary.Write(&buf, binary.BigEndian, uint32(len(chunk.data)))
		buf.WriteString(chunk.typ)
		buf.Write(chunk.data)
		crc := crc32.NewIEEE()
		crc.Write([]byte(chunk.typ))
		crc.Write(chunk.data)
		_ = binary.Write(&buf, binary.BigEndian, crc.Sum32())
	}
	return buf.Bytes()
}

// 像素超限的图必须在解码前被拒：解码器一旦开跑，内存就按声明的宽高分配了。
func TestMakeThumbRejectsPixelBomb(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "bomb.png")
	if err := os.WriteFile(src, pngDeclaring(t, 20000, 20000), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	dst := filepath.Join(dir, "thumb.jpg")
	if _, _, err := makeThumb(src, dst); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("err=%v want ErrImageTooLarge", err)
	}
	if _, err := os.Stat(dst); err == nil {
		t.Fatal("像素超限不应生成缩略图")
	}
}

// 尺寸预检不能误伤正常照片：预检通过后仍要回到文件头重新解码。
func TestMakeThumbAcceptsNormalImage(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "ok.png")
	f, err := os.Create(src)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	w, h, err := makeThumb(src, filepath.Join(dir, "thumb.jpg"))
	if err != nil {
		t.Fatalf("makeThumb: %v", err)
	}
	if w != 4 || h != 3 {
		t.Fatalf("尺寸 %dx%d，want 4x3", w, h)
	}
}
