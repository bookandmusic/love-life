package media

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// buildWAV 构造最小合法 WAV（fmt + data），时长可精确推导。
func buildWAV(t *testing.T, byteRate, dataLen uint32) []byte {
	t.Helper()
	bs := []byte("RIFF\x00\x00\x00\x00WAVE")
	fmtChunk := []byte("fmt ")
	fmtChunk = binary.LittleEndian.AppendUint32(fmtChunk, 16)
	fmtChunk = binary.LittleEndian.AppendUint16(fmtChunk, 1)
	fmtChunk = binary.LittleEndian.AppendUint16(fmtChunk, 1)
	fmtChunk = binary.LittleEndian.AppendUint32(fmtChunk, 44100)
	fmtChunk = binary.LittleEndian.AppendUint32(fmtChunk, byteRate)
	fmtChunk = binary.LittleEndian.AppendUint16(fmtChunk, 2)
	fmtChunk = binary.LittleEndian.AppendUint16(fmtChunk, 16)
	dataChunk := []byte("data")
	dataChunk = binary.LittleEndian.AppendUint32(dataChunk, dataLen)
	dataChunk = append(dataChunk, make([]byte, dataLen)...)
	return append(append(bs, fmtChunk...), dataChunk...)
}

func TestWAVDuration(t *testing.T) {
	// 44100 字节率 × 10 秒数据 → 精确 10 秒
	p := filepath.Join(t.TempDir(), "a.wav")
	if err := os.WriteFile(p, buildWAV(t, 44100, 441000), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := audioDuration(p)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(d-10) > 0.01 {
		t.Fatalf("duration = %v, want 10", d)
	}
}

// buildM4A 构造最小 m4a（ftyp + moov/mvhd，v0）。
func buildM4A(t *testing.T, timescale, duration uint32) []byte {
	t.Helper()
	mvhdPayload := []byte{0}                   // version 0
	mvhdPayload = append(mvhdPayload, 0, 0, 0) // flags
	mvhdPayload = binary.BigEndian.AppendUint32(mvhdPayload, 0)
	mvhdPayload = binary.BigEndian.AppendUint32(mvhdPayload, 0)
	mvhdPayload = binary.BigEndian.AppendUint32(mvhdPayload, timescale)
	mvhdPayload = binary.BigEndian.AppendUint32(mvhdPayload, duration)
	mvhd := append([]byte("mvhd"), mvhdPayload...)
	moovBody := append([]byte("moov"), mvhd...)
	moov := append([]byte{0, 0, 0, byte(len(mvhd) + 8)}, moovBody...)
	ftyp := append([]byte{0, 0, 0, 20}, []byte("ftypM4A isom")...)
	return append(ftyp, moov...)
}

func TestM4ADuration(t *testing.T) {
	// timescale 44100，duration 441000 → 10 秒
	p := filepath.Join(t.TempDir(), "a.m4a")
	if err := os.WriteFile(p, buildM4A(t, 44100, 441000), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := audioDuration(p)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(d-10) > 0.01 {
		t.Fatalf("duration = %v, want 10", d)
	}
}

// buildMP3 构造 MPEG1 Layer3 CBR 帧：128kbps/44100，10 帧全同。
func buildMP3(t *testing.T, frames int) []byte {
	t.Helper()
	// 0xFF 0xFB (MPEG1 L3 no CRC) 0x90 0x00 → bitrateIdx 9 (128k), srIdx 0 (44100), 无 padding
	head := []byte{0xFF, 0xFB, 0x90, 0x00}
	frameLen := 128 * 1000 * 144 / 44100 // ≈ 417 字节
	frame := append(head, make([]byte, frameLen-4)...)
	out := []byte{}
	for i := 0; i < frames; i++ {
		out = append(out, frame...)
	}
	return out
}

func TestMP3DurationEstimate(t *testing.T) {
	// CBR 128kbps：按字节数估算，误差仅来自首帧前的字节（此处为 0）
	frames := 100
	want := float64(frames*(128*1000*144/44100)) * 8 / (128 * 1000)
	p := filepath.Join(t.TempDir(), "a.mp3")
	if err := os.WriteFile(p, buildMP3(t, frames), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := audioDuration(p)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(d-want) > 0.01 {
		t.Fatalf("duration = %v, want %v", d, want)
	}
}

func TestUnsupportedDurationFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.mp3")
	if err := os.WriteFile(p, []byte("not an audio file at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := audioDuration(p); err == nil {
		t.Fatal("want error for non-audio bytes")
	}
}

// buildExifJPEG 构造含 APP1 EXIF（TIFF II）+ DateTimeOriginal 的最小 JPEG。
func buildExifJPEG(t *testing.T, dateTime string) []byte {
	t.Helper()
	// TIFF: II header + IFD0（只含 ExifIFDPointer 0x8769）→ ExifIFD（只含 0x9003 ASCII 20）
	type entry struct {
		tag        uint16
		typ, count uint32
		value      []byte
	}
	toBytes := func(v uint32) []byte {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, v)
		return b
	}
	// 布局：header(8) IFD0(2+n*12+4) exifIFD(2+m*12+4) values
	ifd0Entries := 1
	exifEntries := 1
	ifd0Off := 8
	exifOff := ifd0Off + 2 + ifd0Entries*12 + 4
	valueOff := exifOff + 2 + exifEntries*12 + 4
	h := []byte{'I', 'I', 42, 0}
	h = append(h, toBytes(uint32(ifd0Off))...)
	h = append(h, byte(ifd0Entries), 0)
	// IFD0 entry: tag 0x8769 type LONG(4) count 1 → value = exifOff（内联）
	h = append(h, 0x69, 0x87)
	h = append(h, 4, 0)
	h = append(h, toBytes(1)...)
	h = append(h, toBytes(uint32(exifOff))...)
	h = append(h, toBytes(0)...) // next IFD
	// ExifIFD：count 字段 + entry（tag 0x9003 type ASCII(2) count 20 → value offset = valueOff）
	h = append(h, byte(exifEntries), 0)
	h = append(h, 0x03, 0x90)
	h = append(h, 2, 0)
	h = append(h, toBytes(20)...)
	h = append(h, toBytes(uint32(valueOff))...)
	h = append(h, toBytes(0)...)
	dt := append([]byte(dateTime), 0)
	for len(dt) < 20 {
		dt = append(dt, 0)
	}
	h = append(h, dt...)
	// JPEG: SOI + APP1(Exif\x00\x00 + TIFF) + EOI
	app1Len := len(h) + 8 // 段长含自身 2 字节
	app1 := append([]byte{0xFF, 0xE1}, byte(app1Len>>8), byte(app1Len&0xFF))
	app1 = append(app1, []byte("Exif\x00\x00")...)
	app1 = append(app1, h...)
	return append(append([]byte{0xFF, 0xD8}, app1...), 0xFF, 0xD9)
}

func TestEXIFDateTimeOriginal(t *testing.T) {
	// 有 EXIF：归一化为 YYYY-MM-DD HH:MM:SS
	p := filepath.Join(t.TempDir(), "e.jpg")
	if err := os.WriteFile(p, buildExifJPEG(t, "2026:08:20 14:30:05"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := EXIFDateTimeOriginal(p); got != "2026-08-20 14:30:05" {
		t.Fatalf("EXIF = %q", got)
	}

	// 无 EXIF JPEG：空串（上传方回退 created_at）
	p2 := filepath.Join(t.TempDir(), "plain.jpg")
	plain := []byte{0xFF, 0xD8, 0xFF, 0xD9} // 裸 SOI/EOI，无 APP1
	if err := os.WriteFile(p2, plain, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := EXIFDateTimeOriginal(p2); got != "" {
		t.Fatalf("plain jpeg EXIF = %q, want empty", got)
	}
}
