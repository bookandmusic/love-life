package media

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sync"
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
	return buildISOBMFF(t, "M4A ", timescale, duration)
}

// buildISOBMFF 构造最小 ISO BMFF 容器（ftyp + moov{mvhd}，v0），品牌可换（m4a / mp4 / mov）。
// 每个 box 都带 4 字节长度头——真实容器即如此，解析按长度步进（不靠全文件搜四字节）。
func buildISOBMFF(t *testing.T, brand string, timescale, duration uint32) []byte {
	t.Helper()
	mvhdPayload := []byte{0}                   // version 0
	mvhdPayload = append(mvhdPayload, 0, 0, 0) // flags
	mvhdPayload = binary.BigEndian.AppendUint32(mvhdPayload, 0)
	mvhdPayload = binary.BigEndian.AppendUint32(mvhdPayload, 0)
	mvhdPayload = binary.BigEndian.AppendUint32(mvhdPayload, timescale)
	mvhdPayload = binary.BigEndian.AppendUint32(mvhdPayload, duration)
	mvhd := box("mvhd", mvhdPayload)
	moov := box("moov", mvhd)
	ftyp := box("ftyp", []byte(brand))
	return append(ftyp, moov...)
}

// box 拼一个 ISO BMFF box：4 字节长度 + 4 字节类型 + 负载。
func box(typ string, payload []byte) []byte {
	out := make([]byte, 0, 8+len(payload))
	out = binary.BigEndian.AppendUint32(out, uint32(8+len(payload)))
	out = append(out, []byte(typ)...)
	return append(out, payload...)
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

// 探针只读容器头/头部一段：文件远大于读取窗口时仍要拿到时长与 EXIF。
// mp4 的 mdat 在 moov 之前（非 faststart 排布），wav 的 data 有数 MB，mp3 整文件超探针窗口。
func TestMediaProbesHandleLargeFiles(t *testing.T) {
	dir := t.TempDir()

	// mp4：4 MB mdat 在前、moov 在后；按 box 长度 seek 跳过 mdat 才能读到 mvhd
	real := buildISOBMFF(t, "isom", 1000, 12500)
	mp4 := append(box("mdat", make([]byte, 4<<20)), real...)
	mp4Path := filepath.Join(dir, "big.mp4")
	if err := os.WriteFile(mp4Path, mp4, 0o600); err != nil {
		t.Fatal(err)
	}
	if d, err := videoDuration(mp4Path, "big.mp4"); err != nil || math.Abs(d-12.5) > 0.01 {
		t.Fatalf("大 mp4 duration = %v,%v want 12.5,nil", d, err)
	}

	// wav：2 MB data 负载，只读 chunk 头
	wav := buildWAV(t, 44100, 2<<20)
	wavPath := filepath.Join(dir, "big.wav")
	if err := os.WriteFile(wavPath, wav, 0o600); err != nil {
		t.Fatal(err)
	}
	wantWAV := float64(2<<20) / 44100
	if d, err := audioDuration(wavPath); err != nil || math.Abs(d-wantWAV) > 0.01 {
		t.Fatalf("大 wav duration = %v,%v want %v,nil", d, err, wantWAV)
	}

	// mp3：5000 帧 ≈ 2 MB（超 256 KB 探针窗口），总长按文件大小算
	frames := 5000
	frameLen := 128 * 1000 * 144 / 44100
	mp3Path := filepath.Join(dir, "big.mp3")
	if err := os.WriteFile(mp3Path, buildMP3(t, frames), 0o600); err != nil {
		t.Fatal(err)
	}
	wantMP3 := float64(frames*frameLen) * 8 / (128 * 1000)
	if d, err := audioDuration(mp3Path); err != nil || math.Abs(d-wantMP3) > 0.01 {
		t.Fatalf("大 mp3 duration = %v,%v want %v,nil", d, err, wantMP3)
	}

	// jpeg：EXIF 在头部，文件尾部追加 300 KB（超 128 KB 窗口）仍能取到拍摄时间
	jpeg := append(buildExifJPEG(t, "2026:08:20 14:30:05"), make([]byte, 300<<10)...)
	jpgPath := filepath.Join(dir, "big.jpg")
	if err := os.WriteFile(jpgPath, jpeg, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := EXIFDateTimeOriginal(jpgPath); got != "2026-08-20 14:30:05" {
		t.Fatalf("大 jpeg EXIF = %q", got)
	}
}

// hasFFmpeg 的探测结果被并发读取：用 sync.Once 缓存，任何并发先后都返回同一结果。
func TestHasFFmpegConcurrent(t *testing.T) {
	const n = 16
	results := make(chan bool, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- hasFFmpeg()
		}()
	}
	wg.Wait()
	close(results)
	first := <-results
	for got := range results {
		if got != first {
			t.Fatalf("并发探测结果不一致：%v vs %v", got, first)
		}
	}
}
