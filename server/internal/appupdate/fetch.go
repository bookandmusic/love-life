package appupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// progressReader 统计已读字节并回调（用于慢速回源的进度日志）。
type progressReader struct {
	r          io.Reader
	total      int64
	received   int64
	onProgress func(int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.received += int64(n)
		if p.onProgress != nil {
			p.onProgress(p.received)
		}
	}
	return n, err
}

// apkPath 本地缓存路径（按版本号分目录，版本变了自然换目录）。
func (s *Service) apkPath(rel *Release) string {
	return filepath.Join(s.updDir, rel.VersionName, sanitizeName(rel.AssetName))
}

// sanitizeName 防路径穿越：GitHub 的资源名不该含分隔符，仍做一次防御。
func sanitizeName(name string) string {
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, string(filepath.Separator), "_")
	if name == "" || name == "." || name == ".." {
		return "app.apk"
	}
	return name
}

// Download 在后台把指定版本拉到本地（**只由后台手动触发**，不在 APP 请求路径里调用）。
//
// 为什么不让 APP 请求时现拉：慢速链路下 46MB 要十几分钟，任何客户端都会超时。
// 因此 APP 只能下载「已经躺在本地」的包；没有包就当作没有更新。
//
// 回源不绑调用方请求的生命周期（独立 context + 10 分钟上限），
// 且用 .part 累积 + Range 续传，慢速代理中途 EOF 后重试不从头开始。
func (s *Service) Download(_ context.Context, rel *Release) (string, error) {
	// 总超时按「体积 / 保守速度」估算，再留一倍余量：
	// 慢速代理实测约 250KB/s，46MB 约 3 分钟；给 2 倍余量覆盖抖动与重试。
	// 固定 10 分钟在更慢的链路上会半途而废（且已下载的 .part 会被白白清掉）。
	timeout := estimateTimeout(rel.AssetSize)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	slog.Info("appupdate: 开始回源", "version", rel.VersionName, "size", rel.AssetSize, "timeout", timeout)

	dst := s.apkPath(rel)
	if fi, err := os.Stat(dst); err == nil && fi.Size() > 0 {
		return dst, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	// 清理旧版本目录（同一个版本并发下载时不清理自己）
	if err := s.cleanupExcept(rel.VersionName); err != nil {
		return "", err
	}

	// 慢速代理下大文件传输常在中途断开（EOF）：重试若干次，每次都从已收到的位置续传。
	// 这也是为什么必须用 .part 累积而不是每次覆盖——否则重试永远从头开始。
	tmp := dst + ".part"
	var lastErr error
	for attempt := 0; attempt < fetchRetries; attempt++ {
		if attempt > 0 {
			slog.Warn("appupdate: 回源重试", "version", rel.VersionName, "attempt", attempt+1, "err", lastErr)
			select {
			case <-ctx.Done():
				// 超时也保留 .part：下次触发从断点继续
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		done, err := s.fetchOnce(ctx, rel, tmp)
		if err == nil && done {
			if renameErr := os.Rename(tmp, dst); renameErr != nil {
				_ = os.Remove(tmp)
				return "", renameErr
			}
			return dst, nil
		}
		lastErr = err
	}
	// 保留 .part：下次触发下载会从断点续传，不清零已下载的部分。
	// 只有文件大小已达标却仍失败（内容异常）才丢弃，那种情况续传无意义。
	if fi, statErr := os.Stat(tmp); statErr == nil && rel.AssetSize > 0 && fi.Size() >= rel.AssetSize {
		_ = os.Remove(tmp)
	}
	if lastErr == nil {
		lastErr = errors.New("多次尝试仍未完成下载")
	}
	return "", fmt.Errorf("下载安装包失败：%w", lastErr)
}

// fetchOnce 从 tmp 当前长度处续传一次；返回是否已完成（大小达标）。
func (s *Service) fetchOnce(ctx context.Context, rel *Release, tmp string) (bool, error) {
	client, err := s.client(ctx)
	if err != nil {
		return false, err
	}
	// 已有多少字节：从断点续传
	var offset int64
	if fi, statErr := os.Stat(tmp); statErr == nil {
		offset = fi.Size()
	}
	if rel.AssetSize > 0 && offset >= rel.AssetSize {
		return true, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.AssetURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", userAgent)
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	res, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	// 206 = 续传成功；200 = 服务端不支持 Range，只能从头写
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusPartialContent {
		return false, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if res.StatusCode == http.StatusOK {
		offset = 0
	}

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	if offset == 0 {
		if truncErr := f.Truncate(0); truncErr != nil {
			f.Close()
			return false, truncErr
		}
	}
	if _, seekErr := f.Seek(offset, io.SeekStart); seekErr != nil {
		f.Close()
		return false, seekErr
	}
	// 回源可能很慢（经代理约 70KB/s，46MB 要 10 分钟以上）：
	// 每 5MB 记一次进度，避免运维看到「卡住」却无从判断。
	var lastLogged int64
	written, copyErr := io.Copy(f, io.LimitReader(&progressReader{
		r:     res.Body,
		total: rel.AssetSize,
		onProgress: func(n int64) {
			if n-lastLogged >= 5*1024*1024 {
				lastLogged = n
				slog.Info("appupdate: 安装包回源进度", "version", rel.VersionName, "received", offset+n, "total", rel.AssetSize)
			}
		},
	}, maxFetchBytes-offset))
	closeErr := f.Close()
	if copyErr != nil {
		return false, copyErr // 保留 .part，下次续传
	}
	if closeErr != nil {
		return false, closeErr
	}
	total := offset + written
	// 大小校验：GitHub 给出了 size，对不上说明还没传完（留给下次续传）
	if rel.AssetSize > 0 && total != rel.AssetSize {
		return false, fmt.Errorf("传输未完成（%d/%d）", total, rel.AssetSize)
	}
	return true, nil
}

// LocalAPK 返回本地已就绪的安装包路径；没有则空串。
// 这是 APP 侧唯一的判定依据：本地有包才算「可更新」。
func (s *Service) LocalAPK(rel *Release) string {
	if rel == nil {
		return ""
	}
	dst := s.apkPath(rel)
	if fi, err := os.Stat(dst); err == nil && fi.Size() > 0 {
		return dst
	}
	return ""
}

// ReadyRelease 返回「已下载到本地」的最新版本；没有则 nil。
// 注意它不看 GitHub 上有没有更新版本——APP 只关心服务端能不能给出包。
func (s *Service) ReadyRelease(ctx context.Context) *Release {
	rel, _ := s.cachedRelease(ctx)
	if rel == nil {
		return nil
	}
	if s.LocalAPK(rel) == "" {
		return nil
	}
	return rel
}

// cleanupExcept 删除 updates 下除 keepVersion 之外的所有内容。
func (s *Service) cleanupExcept(keepVersion string) error {
	entries, err := os.ReadDir(s.updDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.Name() == keepVersion {
			continue
		}
		if err := os.RemoveAll(filepath.Join(s.updDir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// Cleanup 清空所有缓存的安装包。
func (s *Service) Cleanup(ctx context.Context) error {
	if err := os.RemoveAll(s.updDir); err != nil {
		return err
	}
	for _, k := range []string{keyRelease, keyETag} {
		if err := s.db.SetSetting(ctx, k, ""); err != nil {
			return err
		}
	}
	return nil
}

// Usage 返回缓存占用与文件（后台展示用）。
type Usage struct {
	Version   string `json:"version"`
	SizeBytes int64  `json:"size_bytes"`
	FetchTime string `json:"fetch_time"`
	FileName  string `json:"file_name"`
}

// Usage 统计当前缓存（无缓存时 Version 为空）。
func (s *Service) Usage(ctx context.Context) Usage {
	rel, _ := s.cachedRelease(ctx)
	if rel == nil {
		return Usage{}
	}
	u := Usage{Version: rel.VersionName, FetchTime: rel.FetchedAt, FileName: rel.AssetName}
	if fi, err := os.Stat(s.apkPath(rel)); err == nil {
		u.SizeBytes = fi.Size()
	}
	return u
}

// fetchRetries 回源重试次数。慢速代理下 46MB 传输中途断开是常态（EOF），
// 每次重试从 .part 已收到的位置续传，因此重试代价很低。
const fetchRetries = 5

// minFetchSpeed 保守估计的下行速度（慢速代理实测约 250KB/s，取一半留余量）。
const minFetchSpeed = 128 * 1024

// estimateTimeout 按体积与保守速度估算超时，下限 5 分钟、上限 60 分钟。
func estimateTimeout(size int64) time.Duration {
	if size <= 0 {
		return 15 * time.Minute
	}
	d := time.Duration(float64(size)/float64(minFetchSpeed)) * time.Second * 2
	if d < 5*time.Minute {
		return 5 * time.Minute
	}
	if d > 60*time.Minute {
		return 60 * time.Minute
	}
	return d
}
