package media

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// buildMP4 最小 mp4/mov（ftyp + moov/mvhd）：与 m4a 同一个容器构造，仅品牌不同。
func buildMP4(t *testing.T, brand string, timescale, duration uint32) []byte {
	t.Helper()
	return buildISOBMFF(t, brand, timescale, duration)
}

// ebmlElem 拼一个 EBML 元素：ID 原样 + 1 字节长度 + 负载。
func ebmlElem(id []byte, payload []byte) []byte {
	out := append([]byte{}, id...)
	out = append(out, byte(0x80|len(payload)))
	return append(out, payload...)
}

// buildWebM 最小 webm：EBML 头（DocType=webm）+ Segment{Info{TimecodeScale, Duration}}。
// duration 为 0 时**不写** Duration 元素（模拟流式录制缺元素的情形）。
func buildWebM(t *testing.T, timecodeScale uint64, duration float32) []byte {
	t.Helper()
	header := ebmlElem([]byte{0x1A, 0x45, 0xDF, 0xA3}, ebmlElem([]byte{0x42, 0x82}, []byte("webm")))
	info := append([]byte{}, ebmlElem([]byte{0x2A, 0xD7, 0xB1}, uintBytes(timecodeScale))...)
	if duration > 0 {
		f := make([]byte, 4)
		binary.BigEndian.PutUint32(f, math.Float32bits(duration))
		info = append(info, ebmlElem([]byte{0x44, 0x89}, f)...)
	}
	infoElem := ebmlElem([]byte{0x15, 0x49, 0xA9, 0x66}, info)
	seg := append([]byte{0x18, 0x53, 0x80, 0x67}, byte(0x80|len(infoElem)))
	return append(append(header, seg...), infoElem...)
}

// uintBytes 大端最短字节表示（EBML 无符号整数元素的值）。
func uintBytes(v uint64) []byte {
	if v == 0 {
		return []byte{0}
	}
	var out []byte
	for v > 0 {
		out = append([]byte{byte(v & 0xFF)}, out...)
		v >>= 8
	}
	return out
}

// P2-14：视频时长自解析（零 ffmpeg）——mp4/mov 读 mvhd、webm 读 Info 的 Duration × TimecodeScale。
func TestVideoDuration(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		file string
		bs   []byte
		want float64
	}{
		{"mp4", "v.mp4", buildMP4(t, "isom", 1000, 12500), 12.5},
		{"mov", "v.mov", buildMP4(t, "qt  ", 600, 9000), 15},
		// webm：Duration 以 TimecodeScale 为单位；scale 缺省 1ms 也走同一条路径
		{"webm 1ms", "v.webm", buildWebM(t, 1_000_000, 4200), 4.2},
		{"webm 10ms", "v2.webm", buildWebM(t, 10_000_000, 420), 4.2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, tc.file)
			if err := os.WriteFile(p, tc.bs, 0o600); err != nil {
				t.Fatal(err)
			}
			d, err := videoDuration(p, tc.file)
			if err != nil {
				t.Fatalf("解析失败：%v", err)
			}
			if math.Abs(d-tc.want) > 0.01 {
				t.Fatalf("duration = %v, want %v", d, tc.want)
			}
		})
	}
}

// 解析不出时长时必须**报错而不猜**（前端角标留空），上传流程据此保持「失败不阻塞」。
func TestVideoDurationFailsClosed(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name string
		file string
		bs   []byte
	}{
		{"webm 缺 Duration 元素", "x.webm", buildWebM(t, 1_000_000, 0)},
		{"mp4 缺 mvhd", "x.mp4", append([]byte{0, 0, 0, 16}, []byte("ftypisom")...)},
		{"截断的 webm", "x.webm", buildWebM(t, 1_000_000, 4200)[:20]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, "x.bin")
			if err := os.WriteFile(p, tc.bs, 0o600); err != nil {
				t.Fatal(err)
			}
			if d, err := videoDuration(p, tc.file); err == nil {
				t.Fatalf("应报错，实得 duration=%v", d)
			}
		})
	}
}

// 上传管线：视频时长进 Uploaded 与 sidecar（发布转正后落 media.duration）。
func TestSaveTempVideoDuration(t *testing.T) {
	p := New(t.TempDir(), []byte("0123456789abcdef0123456789abcdef"), 10<<20)
	up, err := p.SaveTemp(bytes.NewReader(buildMP4(t, "isom", 1000, 9000)), "clip.mp4", 1)
	if err != nil {
		t.Fatalf("SaveTemp: %v", err)
	}
	if up.Kind != "video" {
		t.Fatalf("kind = %q", up.Kind)
	}
	if math.Abs(up.Duration-9) > 0.01 {
		t.Fatalf("duration = %v, want 9", up.Duration)
	}

	// 损坏容器：不阻塞上传，duration 留空
	up2, err := p.SaveTemp(bytes.NewReader([]byte("not a real video at all")), "broken.mp4", 1)
	if err != nil {
		t.Fatalf("损坏容器不应阻塞上传：%v", err)
	}
	if up2.Duration != 0 {
		t.Fatalf("解析失败应留空，实得 %v", up2.Duration)
	}
}

// 反向用例：容器**数据区**里出现与头元素同名的字节串时，不得把它当头部读出来。
// 全文件搜字节的旧实现会在这两例上给出「看起来合法」的错误时长（mp4 999.999s / webm 0.012s）。
func TestVideoDurationIgnoresHeaderBytesInPayload(t *testing.T) {
	dir := t.TempDir()

	// mp4：mdat 在前、moov 在后（非 faststart 的常见排布），mdat 里塞一份「假 mvhd」
	// ——全文件搜字节会先命中这一份，算出 999999 秒的假时长
	real := buildISOBMFF(t, "isom", 1000, 12500)
	fake := append([]byte("mvhd"), make([]byte, 20)...)
	binary.BigEndian.PutUint32(fake[13:17], 1)      // timescale = 1
	binary.BigEndian.PutUint32(fake[17:21], 999999) // duration = 999999
	mdat := box("mdat", append([]byte("junk"), fake...))
	mp4 := append(mdat, real...)

	// webm：Info 里的 Duration 正确（4.2s），Cluster 数据里塞一份假的 Duration 元素
	webm := buildWebM(t, 1_000_000, 4200)
	cluster := ebmlElem([]byte{0x1F, 0x43, 0xB6, 0x75}, ebmlElem([]byte{0x44, 0x89}, []byte{0x45, 0x83, 0x40, 0x00}))
	webm = append(webm, cluster...)

	for _, tc := range []struct {
		name string
		file string
		bs   []byte
		want float64
	}{
		{"mp4 数据区含假 mvhd", "x.mp4", mp4, 12.5},
		{"webm 簇内含假 Duration", "x.webm", webm, 4.2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, tc.file)
			if err := os.WriteFile(p, tc.bs, 0o600); err != nil {
				t.Fatal(err)
			}
			d, err := videoDuration(p, tc.file)
			if err != nil {
				t.Fatalf("应解析出真实时长，实得错误：%v", err)
			}
			if math.Abs(d-tc.want) > 0.01 {
				t.Fatalf("duration = %v，want %v（读到了数据区里的假头？）", d, tc.want)
			}
		})
	}
}
