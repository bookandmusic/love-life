package media

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
)

// audioDuration 音频时长（秒，REAL）——零依赖手写解析：
// wav 精确（data 大小/字节率）；m4a 读 mvhd；mp3 按首帧码率估算（VBR 有误差，产品可接受）。
// 只按需读：wav 读 chunk 头、m4a 按 box 长度 seek、mp3 只读文件头一段（见 mp3ProbeLimit）。
func audioDuration(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := info.Size()

	var head [12]byte
	n, err := f.ReadAt(head[:], 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, err
	}
	if n >= 12 && string(head[0:4]) == "RIFF" && string(head[8:12]) == "WAVE" {
		return wavDuration(f, size)
	}
	if n >= 8 && string(head[4:8]) == "ftyp" {
		return mp4Duration(f, size)
	}
	return mp3Duration(f, size)
}

// wavDuration 只读 RIFF chunk 头算时长：fmt 取 byteRate，data 取声明长度（按文件大小收敛），
// 不读 data 负载（可能几十 MB）。
func wavDuration(f *os.File, size int64) (float64, error) {
	var byteRate, dataSize int64
	for off := int64(12); off+8 <= size; {
		var hdr [8]byte
		if _, err := f.ReadAt(hdr[:], off); err != nil {
			break // 尾部截断：已读到的信息不足就按失败返回
		}
		id := string(hdr[:4])
		sz := int64(binary.LittleEndian.Uint32(hdr[4:8]))
		if id == "fmt " && off+20 <= size {
			var fmtBuf [12]byte
			if _, err := f.ReadAt(fmtBuf[:], off+8); err == nil {
				byteRate = int64(binary.LittleEndian.Uint32(fmtBuf[8:12]))
			}
		}
		if id == "data" {
			dataSize = sz
			if dataSize > size-off-8 {
				dataSize = size - off - 8
			}
		}
		if byteRate > 0 && dataSize > 0 {
			return float64(dataSize) / float64(byteRate), nil
		}
		next := off + 8 + sz
		if sz%2 == 1 {
			next++ // RIFF chunk 2 字节对齐
		}
		if next <= off {
			break
		}
		off = next
	}
	return 0, errors.New("wav 缺 fmt/data chunk")
}

// mp3ProbeLimit mp3 探针只读文件头 256 KB 找首帧（ID3v2 之后通常紧跟帧头）。
// ponytail: 超大 ID3 标签或损坏文件会在窗口内找不到帧同步，时长留空不阻塞上传；
// 真要精确解析 VBR 需遍历全部帧或读 Xing/VBRI 头，届时再升级。
const mp3ProbeLimit = 256 << 10

// mp3Duration 首帧码率估算：跳过 ID3v2，找帧同步字 0xFFE；总长按文件大小算（只读头一段）。
func mp3Duration(f *os.File, size int64) (float64, error) {
	headLen := size
	if headLen > mp3ProbeLimit {
		headLen = mp3ProbeLimit
	}
	bs := make([]byte, headLen)
	if _, err := f.ReadAt(bs, 0); err != nil && !errors.Is(err, io.EOF) {
		return 0, err
	}
	start := 0
	if len(bs) > 10 && string(bs[0:3]) == "ID3" {
		start = 10 + int(bs[6])<<21 + int(bs[7])<<14 + int(bs[8])<<7 + int(bs[9])
	}
	// MPEG1 L3 码率表 kbps（brIdx 1-14）
	br1 := map[int]int{1: 32, 2: 40, 3: 48, 4: 56, 5: 64, 6: 80, 7: 96, 8: 112, 9: 128, 10: 160, 11: 192, 12: 224, 13: 256, 14: 320}
	// MPEG2/2.5 L3 码率表 kbps
	br2 := map[int]int{1: 8, 2: 16, 3: 24, 4: 32, 5: 40, 6: 48, 7: 56, 8: 64, 9: 80, 10: 96, 11: 112, 12: 128, 13: 144, 14: 160}
	sampleRates := map[int]int{0: 44100, 1: 48000, 2: 32000}
	for i := start; i < len(bs)-4; i++ {
		if bs[i] != 0xFF || bs[i+1]&0xE0 != 0xE0 {
			continue
		}
		version := int(bs[i+1]>>3) & 0x03
		layer := int(bs[i+1]>>1) & 0x03
		brIdx := int(bs[i+2] >> 4)
		srIdx := int(bs[i+2]>>2) & 0x03
		if version == 1 || layer != 1 || brIdx == 0 || brIdx == 15 || srIdx == 3 {
			continue
		}
		var bitrate int
		if version == 3 {
			bitrate = br1[brIdx] * 1000
		} else {
			bitrate = br2[brIdx] * 1000
		}
		sr := sampleRates[srIdx]
		if bitrate == 0 || sr == 0 {
			continue
		}
		return float64(size-int64(i)) * 8 / float64(bitrate), nil
	}
	return 0, errors.New("mp3 帧同步未找到")
}
