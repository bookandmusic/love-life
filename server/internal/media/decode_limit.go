package media

import (
	"errors"
	"image"
	"io"
	"os"
)

// maxDecodePixels 单张图片解码的像素上限（约 5200 万，覆盖手机 48MP 原图）。
const maxDecodePixels = 50 << 20

// ErrImageTooLarge 图片像素超过解码上限：小文件可以声明几十亿像素，整图解码会把进程撑爆。
var ErrImageTooLarge = errors.New("图片分辨率过大，请压缩后再上传")

// guardDecodeSize 只读文件头取宽高，超限直接拒绝，并把读指针绕回开头交给解码器。
// 必须先预检再解码：解码器按声明的宽高一次性分配内存，与文件实际大小无关。
func guardDecodeSize(f *os.File) error {
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return err
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxDecodePixels {
		return ErrImageTooLarge
	}
	_, err = f.Seek(0, io.SeekStart)
	return err
}
