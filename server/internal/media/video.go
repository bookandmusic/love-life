package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// videoDuration 视频时长（秒）——零 ffmpeg 自实现容器头解析，与音频同思路：
// mp4/mov 读 moov/mvhd（与 m4a 同一个 box，共用 mvhdDuration）；webm 读 Segment/Info 的 Duration。
// ext 传扩展名或文件名均可（按最后一个点的后缀判分支）——按扩展名分流，不嗅探字节：
// 容器字节里出现什么都有可能，扩展名是上传时就已确定的分类。
// 解析失败不阻塞上传（duration 留空，前端只显示播放占位图、不显示时长）。
func videoDuration(path, ext string) (float64, error) {
	bs, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	// 传扩展名或文件名都认（filepath.Ext 取最后一个点起的后缀）
	if strings.EqualFold(filepath.Ext(ext), ".webm") {
		return webmDuration(bs)
	}
	return mvhdDuration(bs)
}

// ---- mp4 / mov / m4a（ISO BMFF box）----

// boxFind 在 box 序列里按类型找第一个 box，返回其负载（不含头）。
// 按 box 声明长度步进，**不跨边界搜字节**：mdat 里的数据可能恰好含 "mvhd" 四字节，
// 全文件搜会把随机数据当成时间基算出一个看似合法的错误时长。
func boxFind(bs []byte, typ string) ([]byte, bool) {
	for off := 0; off+8 <= len(bs); {
		size := int64(binary.BigEndian.Uint32(bs[off : off+4]))
		head := int64(8)
		switch size {
		case 1: // 64 位 largesize
			if off+16 > len(bs) {
				return nil, false
			}
			size = int64(binary.BigEndian.Uint64(bs[off+8 : off+16]))
			head = 16
		case 0: // 延伸到文件尾
			size = int64(len(bs) - off)
		}
		if size < head || off+int(size) > len(bs) {
			return nil, false
		}
		if string(bs[off+4:off+8]) == typ {
			return bs[off+int(head) : off+int(size)], true
		}
		off += int(size)
	}
	return nil, false
}

// mvhdDuration 读 moov/mvhd 的 timescale 与 duration；m4a 与视频共用（同一个 box）。
func mvhdDuration(bs []byte) (float64, error) {
	moov, ok := boxFind(bs, "moov")
	if !ok {
		return 0, errors.New("moov not found")
	}
	p, ok := boxFind(moov, "mvhd")
	if !ok {
		return 0, errors.New("mvhd not found")
	}
	if len(p) < 20 {
		return 0, errors.New("mvhd truncated")
	}
	var timescale, duration uint64
	if p[0] == 1 { // version 1：64 位 duration
		if len(p) < 32 {
			return 0, errors.New("mvhd truncated")
		}
		timescale = uint64(binary.BigEndian.Uint32(p[20:24]))
		duration = binary.BigEndian.Uint64(p[24:32])
	} else {
		timescale = uint64(binary.BigEndian.Uint32(p[12:16]))
		duration = uint64(binary.BigEndian.Uint32(p[16:20]))
	}
	if timescale == 0 {
		return 0, errors.New("bad timescale")
	}
	return float64(duration) / float64(timescale), nil
}

// ---- webm（EBML）----

// EBML 元素 ID（定长，直接按字节比对）。
var (
	ebmlSegment = []byte{0x18, 0x53, 0x80, 0x67}
	ebmlInfo    = []byte{0x15, 0x49, 0xA9, 0x66}
	ebmlScale   = []byte{0x2A, 0xD7, 0xB1} // TimecodeScale（纳秒/单位，缺省 1e6）
	ebmlDur     = []byte{0x44, 0x89}       // Duration（浮点，单位 = TimecodeScale）
)

// webmDuration 取 Segment/Info 里的 TimecodeScale 与 Duration。
// Duration 缺失（流式录制的 webm 常见）即解析失败——不猜时长。
func webmDuration(bs []byte) (float64, error) {
	seg, ok := ebmlFind(bs, ebmlSegment)
	if !ok {
		return 0, errors.New("webm 缺 Segment")
	}
	info, ok := ebmlFind(seg, ebmlInfo)
	if !ok {
		return 0, errors.New("webm 缺 Info")
	}
	scale := 1_000_000.0 // 缺省 1ms（纳秒）
	if raw, ok := ebmlFind(info, ebmlScale); ok {
		if v, err := ebmlUintValue(raw); err == nil && v > 0 {
			scale = float64(v)
		}
	}
	raw, ok := ebmlFind(info, ebmlDur)
	if !ok {
		return 0, errors.New("webm 缺 Duration")
	}
	dur, err := ebmlFloatValue(raw)
	if err != nil {
		return 0, err
	}
	// Duration 以 TimecodeScale 为单位（纳秒/单位）→ 秒
	return dur * scale / 1e9, nil
}

// ebmlFind 在**同一层**的 EBML 元素序列里找 ID 匹配的第一个元素，返回其负载。
// 按元素声明长度步进，不跨边界搜字节：帧数据里出现 0x44 0x89 这类短 ID 的概率不低，
// 全文件搜会把它读成浮点，造出一个看似合法的错误时长。
// 未定长元素（长度位全 1，流式文件常见）取到缓冲区末尾，其后没有兄弟元素可找。
func ebmlFind(bs, id []byte) ([]byte, bool) {
	for len(bs) > 0 {
		idLen := ebmlIDLen(bs[0])
		if idLen == 0 || len(bs) < idLen+1 {
			return nil, false
		}
		cur := bs[:idLen]
		size, n, unknown, ok := ebmlSize(bs[idLen:])
		if !ok {
			return nil, false
		}
		start := idLen + n
		end := len(bs)
		if !unknown {
			if size > uint64(len(bs)-start) {
				return nil, false
			}
			end = start + int(size)
		}
		if bytes.Equal(cur, id) {
			return bs[start:end], true
		}
		if unknown {
			return nil, false
		}
		bs = bs[end:]
	}
	return nil, false
}

// ebmlIDLen 元素 ID 的字节数（首字节前导 1 的位置决定，1~4 字节）。
func ebmlIDLen(b byte) int {
	for n := 1; n <= 4; n++ {
		if b&(0x80>>(n-1)) != 0 {
			return n
		}
	}
	return 0
}

// ebmlSize 读长度字段：返回值与占用字节数；unknown = 长度位全 1（未定长）。
func ebmlSize(bs []byte) (size uint64, n int, unknown bool, ok bool) {
	if len(bs) == 0 || bs[0] == 0 {
		return 0, 0, false, false
	}
	n = 1
	for mask := byte(0x80); mask != 0; mask >>= 1 {
		if bs[0]&mask != 0 {
			break
		}
		n++
	}
	if n > 8 || len(bs) < n {
		return 0, 0, false, false
	}
	size = uint64(bs[0] & (0xFF >> n))
	allOnes := uint64(0xFF >> n)
	for i := 1; i < n; i++ {
		size = size<<8 | uint64(bs[i])
		allOnes = allOnes<<8 | 0xFF
	}
	return size, n, size == allOnes, true
}

// ebmlUintValue 无符号整数元素的值（0~8 字节，大端）。
func ebmlUintValue(bs []byte) (uint64, error) {
	if len(bs) == 0 || len(bs) > 8 {
		return 0, errors.New("bad ebml uint")
	}
	var v uint64
	for _, b := range bs {
		v = v<<8 | uint64(b)
	}
	return v, nil
}

// ebmlFloatValue 浮点元素的值（4 或 8 字节，大端）。
func ebmlFloatValue(bs []byte) (float64, error) {
	switch len(bs) {
	case 4:
		return float64(math.Float32frombits(binary.BigEndian.Uint32(bs))), nil
	case 8:
		return math.Float64frombits(binary.BigEndian.Uint64(bs)), nil
	}
	return 0, errors.New("bad ebml float size")
}
