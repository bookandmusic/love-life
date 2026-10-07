package media

import (
	"encoding/binary"
	"errors"
	"os"
)

// audioDuration 音频时长（秒，REAL）——零依赖手写解析：
// wav 精确（data 大小/字节率）；m4a 读 mvhd；mp3 按首帧码率估算（VBR 有误差，产品可接受）。
func audioDuration(path string) (float64, error) {
	bs, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(bs) >= 44 && string(bs[0:4]) == "RIFF" && string(bs[8:12]) == "WAVE" {
		return wavDuration(bs)
	}
	if len(bs) > 32 && string(bs[4:8]) == "ftyp" {
		return m4aDuration(bs)
	}
	return mp3Duration(bs)
}

func wavDuration(bs []byte) (float64, error) {
	var byteRate, dataSize uint32
	for off := 12; off+8 <= len(bs); {
		id := string(bs[off : off+4])
		sz := binary.LittleEndian.Uint32(bs[off+4 : off+8])
		if id == "fmt " && off+20 <= len(bs) {
			byteRate = binary.LittleEndian.Uint32(bs[off+16 : off+20])
		}
		if id == "data" {
			dataSize = sz
			if int(dataSize) > len(bs)-off-8 {
				dataSize = uint32(len(bs) - off - 8)
			}
		}
		off += 8 + int(sz)
		if sz%2 == 1 {
			off++ // RIFF chunk 2 字节对齐
		}
		if byteRate > 0 && dataSize > 0 {
			return float64(dataSize) / float64(byteRate), nil
		}
	}
	return 0, errors.New("wav 缺 fmt/data chunk")
}

// m4aDuration m4a 时长：与视频同一个 mvhd box，共用按 box 步进的解析
// （不再全文件搜 "mvhd"——mdat 里的数据可能恰好含该四字节，会算出错误时长）。
func m4aDuration(bs []byte) (float64, error) {
	return mvhdDuration(bs)
}

// mp3Duration 首帧码率估算：跳过 ID3v2，找帧同步字 0xFFE。
func mp3Duration(bs []byte) (float64, error) {
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
		return float64(len(bs)-i) * 8 / float64(bitrate), nil
	}
	return 0, errors.New("mp3 帧同步未找到")
}

func indexOf(bs, sub []byte) int {
	for i := 0; i+len(sub) <= len(bs); i++ {
		match := true
		for j := range sub {
			if bs[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
