package media

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// ffmpeg 是本项目的**必备运行时依赖**（部署镜像必须安装）。
// 用途：视频首帧封面、音频波形图。缺失时降级为占位图，不阻塞上传。

const ffmpegTimeout = 30 * time.Second

// hasFFmpeg 探测 ffmpeg 是否可用（sync.Once 缓存：只 fork 一次 LookPath，且并发调用无竞争）。
var (
	ffmpegOnce sync.Once
	ffmpegOK   bool
)

func hasFFmpeg() bool {
	ffmpegOnce.Do(func() {
		_, err := exec.LookPath("ffmpeg")
		ffmpegOK = err == nil
	})
	return ffmpegOK
}

// ExtractVideoThumb 抽视频首帧存为 JPEG（微信式列表封面）。
// 失败返回 error，由调用方决定降级（当前策略：降级为占位图，不阻塞上传）。
func ExtractVideoThumb(src, dst string) error {
	if !hasFFmpeg() {
		return fmt.Errorf("ffmpeg 未安装")
	}
	ctx, cancel := context.WithTimeout(context.Background(), ffmpegTimeout)
	defer cancel()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	// -ss 0 取首帧；scale 限制最大边 1280，避免 4K 首帧过大
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-ss", "0",
		"-i", src,
		"-vframes", "1",
		"-vf", "scale='min(1280,iw)':-2",
		"-q:v", "3",
		"-y", dst,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg 抽帧失败: %w (%s)", err, truncate(string(out), 200))
	}
	if info, serr := os.Stat(dst); serr != nil || info.Size() == 0 {
		return fmt.Errorf("ffmpeg 未产出首帧")
	}
	return nil
}

// GenerateWaveform 生成音频波形图（微信式音频卡片封面）。
// 输出 PNG：透明底 + 主色波形，尺寸 400x400。
func GenerateWaveform(src, dst, color string) error {
	if !hasFFmpeg() {
		return fmt.Errorf("ffmpeg 未安装")
	}
	if color == "" {
		color = "#FF8FAB"
	}
	ctx, cancel := context.WithTimeout(context.Background(), ffmpegTimeout)
	defer cancel()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-i", src,
		"-filter_complex", "showwavespic=s=400x400:colors="+color,
		"-frames:v", "1",
		"-y", dst,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg 波形生成失败: %w (%s)", err, truncate(string(out), 200))
	}
	if info, serr := os.Stat(dst); serr != nil || info.Size() == 0 {
		return fmt.Errorf("ffmpeg 未产出波形图")
	}
	return nil
}

// truncate 截断命令输出，避免错误信息过长。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
