package media

import (
	"encoding/binary"
	"os"
)

// EXIFDateTimeOriginal 最小 JPEG APP1/TIFF 解析（A1c，DESIGN 4.8）：
// 仅取 Exif SubIFD 的 DateTimeOriginal(0x9003)，GPS 忽略；II/MM 两种字节序；
// 归一化为 "YYYY-MM-DD HH:MM:SS"（与 SQLite created_at 同形，时间轴排序口径一致）；
// 非 JPEG / 无 EXIF / 解析失败一律返回空串（上传方回退 created_at）。
func EXIFDateTimeOriginal(path string) string {
	bs, err := os.ReadFile(path)
	if err != nil || len(bs) < 4 || bs[0] != 0xFF || bs[1] != 0xD8 {
		return ""
	}
	// 扫描 JPEG 段：FFxx + 段长（2B 大端）；APP1 且头为 Exif\x00\x00 即 TIFF 块
	off := 2
	var tiff []byte
	for off+4 <= len(bs) {
		if bs[off] != 0xFF {
			break
		}
		marker := bs[off+1]
		if marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) { // 独立段无长度
			off += 2
			continue
		}
		segLen := int(binary.BigEndian.Uint16(bs[off+2 : off+4]))
		if marker == 0xE1 && off+10 <= len(bs) && string(bs[off+4:off+10]) == "Exif\x00\x00" {
			if off+4+segLen <= len(bs) {
				tiff = bs[off+10 : off+2+segLen]
			}
			break
		}
		off += 2 + segLen
	}
	if len(tiff) < 8 {
		return ""
	}
	bo := binary.ByteOrder(binary.LittleEndian)
	if tiff[0] == 'M' && tiff[1] == 'M' {
		bo = binary.BigEndian
	} else if tiff[0] != 'I' || tiff[1] != 'I' {
		return ""
	}
	if bo.Uint16(tiff[2:4]) != 42 {
		return ""
	}
	ifd0 := int(bo.Uint32(tiff[4:8]))
	if entry, ok := findIFDEntry(tiff, ifd0, 0x8769, bo); ok { // ExifIFDPointer (LONG, count=1, 内联)
		exifIFD := int(bo.Uint32(entry[8:12]))
		if e2, ok2 := findIFDEntry(tiff, exifIFD, 0x9003, bo); ok2 { // DateTimeOriginal (ASCII, count=20)
			valueOff := int(bo.Uint32(e2[8:12]))
			if valueOff >= 0 && valueOff+20 <= len(tiff) {
				s := string(tiff[valueOff : valueOff+20])
				if len(s) >= 19 && s[4] == ':' && s[7] == ':' && s[10] == ' ' {
					return s[0:4] + "-" + s[5:7] + "-" + s[8:10] + s[10:19]
				}
			}
		}
	}
	return ""
}

// findIFDEntry 在指定 IFD 内线性查找 tag，返回 12 字节 IFD Entry。
func findIFDEntry(bs []byte, ifdOff int, tag uint16, bo binary.ByteOrder) ([12]byte, bool) {
	var zero [12]byte
	if ifdOff < 0 || ifdOff+2 > len(bs) {
		return zero, false
	}
	n := int(bo.Uint16(bs[ifdOff : ifdOff+2]))
	for i := 0; i < n; i++ {
		e := ifdOff + 2 + i*12
		if e+12 > len(bs) {
			return zero, false
		}
		if bo.Uint16(bs[e:e+2]) == tag {
			var entry [12]byte
			copy(entry[:], bs[e:e+12])
			return entry, true
		}
	}
	return zero, false
}
