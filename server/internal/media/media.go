// Package media 媒体上传管线（DESIGN 4.8）：两段式上传（临时区→发布转正）、
// 图片同步缩略图、视频占位图（零 ffmpeg 依赖）、短期签名 URL。
package media

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"golang.org/x/image/webp"
)

const thumbMaxEdge = 1080

var ErrUnsupportedFormat = errors.New("不支持的媒体格式（照片 jpg/png/webp，视频 mp4/mov/webm，音频 mp3/m4a/wav；HEIC 首版不支持）")

type Pipeline struct {
	dataDir  string
	secret   []byte // 与 JWT 共用密钥材料的 HMAC（仅用于签名 URL，用途域分隔）
	maxBytes int64
}

func New(dataDir string, secret []byte, maxBytes int64) *Pipeline {
	return &Pipeline{dataDir: dataDir, secret: secret, maxBytes: maxBytes}
}

type Uploaded struct {
	Token    string  `json:"media_token"`
	Kind     string  `json:"kind"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Size     int64   `json:"size"`
	Duration float64 `json:"duration,omitempty"`
	TakenAt  string  `json:"taken_at,omitempty"`
}

// tempMeta 临时区 sidecar 元数据（media/tmp/{token}.json），发布转正时读取；让元数据跨重启存活。
type tempMeta struct {
	Kind     string  `json:"kind"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Size     int64   `json:"size"`
	Duration float64 `json:"duration,omitempty"`
	TakenAt  string  `json:"taken_at,omitempty"`
}

// Promoted 转正结果：正式区相对路径 + 媒体元数据。
type Promoted struct {
	FilePath  string
	ThumbPath string
	Kind      string
	Width     int
	Height    int
	Size      int64
	Duration  float64
	TakenAt   string
}

var photoExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true}
var videoExts = map[string]bool{".mp4": true, ".mov": true, ".webm": true}
var audioExts = map[string]bool{".mp3": true, ".m4a": true, ".wav": true}

func kindOf(filename string) (kind, ext string, err error) {
	ext = strings.ToLower(filepath.Ext(filename))
	if photoExts[ext] {
		return "photo", ext, nil
	}
	if videoExts[ext] {
		return "video", ext, nil
	}
	if audioExts[ext] {
		return "audio", ext, nil
	}
	return "", "", ErrUnsupportedFormat
}

// SaveTemp 上传至临时区并同步生成缩略图；返回临时 token（发布时引用）。
func (p *Pipeline) SaveTemp(r io.Reader, filename string) (*Uploaded, error) {
	kind, ext, err := kindOf(filename)
	if err != nil {
		return nil, err
	}
	token := randHex(16)
	tmpDir := filepath.Join(p.dataDir, "media", "tmp")
	tmpThumbDir := filepath.Join(p.dataDir, "thumbs", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(tmpThumbDir, 0o755); err != nil {
		return nil, err
	}

	rawPath := filepath.Join(tmpDir, token+ext)
	f, err := os.Create(rawPath)
	if err != nil {
		return nil, err
	}
	size, err := io.Copy(f, r)
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(rawPath)
		return nil, err
	}
	if size > p.maxBytes {
		_ = os.Remove(rawPath)
		return nil, fmt.Errorf("文件超过大小上限（%d MB）", p.maxBytes/(1<<20))
	}

	up := &Uploaded{Token: token, Kind: kind, Size: size}

	if kind == "photo" {
		w, h, thumbErr := makeThumb(rawPath, filepath.Join(tmpThumbDir, token+".jpg"))
		if thumbErr != nil {
			_ = os.Remove(rawPath)
			return nil, fmt.Errorf("生成缩略图失败: %w", thumbErr)
		}
		up.Width, up.Height = w, h
		// A1c：EXIF 拍摄时间（缺失回退上传时间，落库时处理），GPS 忽略（DESIGN 4.8）
		up.TakenAt = EXIFDateTimeOriginal(rawPath)
	}
	if kind == "audio" {
		// D4：音频时长（秒）；解析失败不阻塞上传（duration 留空）
		if d, err := audioDuration(rawPath); err == nil && d > 0 {
			up.Duration = d
		}
	}

	metaBytes, _ := json.Marshal(tempMeta{Kind: kind, Width: up.Width, Height: up.Height, Size: size, Duration: up.Duration, TakenAt: up.TakenAt})
	if err := os.WriteFile(filepath.Join(tmpDir, token+".json"), metaBytes, 0o600); err != nil {
		_ = os.Remove(rawPath)
		return nil, err
	}
	return up, nil
}

// makeThumb 解码原图取尺寸并生成最长边 1080 的 JPEG 缩略图；损坏图片直接报错拒绝。
func makeThumb(srcPath, dstPath string) (int, int, error) {
	img, err := decodeImage(srcPath)
	if err != nil {
		return 0, 0, fmt.Errorf("decode: %w", err)
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	thumb := scaleToMaxEdge(img, thumbMaxEdge)
	out, err := os.Create(dstPath)
	if err != nil {
		return 0, 0, err
	}
	defer out.Close()
	if err := encodeJPEG(out, thumb); err != nil {
		return 0, 0, err
	}
	return w, h, nil
}

func decodeImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if strings.EqualFold(filepath.Ext(path), ".webp") {
		return webp.Decode(f)
	}
	img, _, err := image.Decode(f)
	return img, err
}

func (p *Pipeline) tmpPaths(token string) (raw string, thumb string, ok bool) {
	matches, _ := filepath.Glob(filepath.Join(p.dataDir, "media", "tmp", token+".*"))
	for _, m := range matches {
		if strings.HasSuffix(m, ".json") {
			continue
		}
		raw = m
	}
	if raw == "" {
		return "", "", false
	}
	tb := filepath.Join(p.dataDir, "thumbs", "tmp", token+".jpg")
	if _, err := os.Stat(tb); err == nil {
		thumb = tb
	}
	return raw, thumb, true
}

// Promote 发布转正：临时区文件移入 media/{年}/{月}/ 与 thumbs/{年}/{月}/，读取 sidecar 元数据。
func (p *Pipeline) Promote(token string) (*Promoted, error) {
	raw, thumb, ok := p.tmpPaths(token)
	if !ok {
		return nil, fmt.Errorf("临时媒体不存在或已过期: %s", token)
	}
	metaBytes, err := os.ReadFile(filepath.Join(p.dataDir, "media", "tmp", token+".json"))
	if err != nil {
		return nil, fmt.Errorf("临时媒体元数据缺失: %s", token)
	}
	var m tempMeta
	if err := json.Unmarshal(metaBytes, &m); err != nil {
		return nil, err
	}

	rel, err := p.moveToFinal(raw, "media")
	if err != nil {
		return nil, err
	}
	relThumb := ""
	if thumb != "" {
		if relThumb, err = p.moveToFinal(thumb, "thumbs"); err != nil {
			return nil, err
		}
	}
	_ = os.Remove(filepath.Join(p.dataDir, "media", "tmp", token+".json"))
	return &Promoted{FilePath: rel, ThumbPath: relThumb, Kind: m.Kind, Width: m.Width, Height: m.Height, Size: m.Size, Duration: m.Duration, TakenAt: m.TakenAt}, nil
}

func (p *Pipeline) moveToFinal(src, root string) (string, error) {
	now := time.Now()
	finalDir := filepath.Join(p.dataDir, root, now.Format("2006"), now.Format("01"))
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(finalDir, filepath.Base(src))
	if err := os.Rename(src, dst); err != nil {
		return "", err
	}
	return filepath.Rel(p.dataDir, dst)
}

// Delete 物理删除主文件与缩略图（回收站永久清除时调用）。
// Delete 删除数据目录内的相对路径；越界路径（脏数据或构造路径）直接跳过，避免误删数据目录外的文件。
// 主文件与缩略图由调用方一并传入。
func (p *Pipeline) Delete(relPaths ...string) {
	for _, rel := range relPaths {
		if rel == "" {
			continue
		}
		full := filepath.Join(p.dataDir, rel)
		relToRoot, err := filepath.Rel(p.dataDir, full)
		if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
			continue
		}
		_ = os.Remove(full)
	}
}

// CleanTmp 清理临时区孤儿（上传后 24h 未发布引用）。
func (p *Pipeline) CleanTmp(olderThan time.Duration) int {
	cutoff := time.Now().Add(-olderThan)
	removed := 0
	for _, root := range []string{filepath.Join(p.dataDir, "media", "tmp"), filepath.Join(p.dataDir, "thumbs", "tmp")} {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			info, err := e.Info()
			if err != nil || info.IsDir() {
				continue
			}
			if info.ModTime().Before(cutoff) {
				if err := os.Remove(filepath.Join(root, e.Name())); err == nil {
					removed++
				}
			}
		}
	}
	return removed
}

// ---- 签名 URL（DESIGN 4.5：短期签名）----

// signedURLTTL 媒体签名 URL 有效期（DESIGN 4.5 默认 15 分钟）。
const signedURLTTL = 15 * time.Minute

func (p *Pipeline) signPayload(mediaID int64, variant string, exp time.Time) string {
	mac := hmac.New(sha256.New, p.secret)
	fmt.Fprintf(mac, "media:%d:%s:%d", mediaID, variant, exp.Unix())
	return hex.EncodeToString(mac.Sum(nil))
}

func (p *Pipeline) SignedURL(mediaID int64, variant string) string {
	exp := time.Now().Add(signedURLTTL)
	return fmt.Sprintf("/api/v1/media/file/%d?v=%s&exp=%d&sig=%s",
		mediaID, variant, exp.Unix(), p.signPayload(mediaID, variant, exp))
}

func (p *Pipeline) VerifySig(mediaID int64, variant, expStr, sig string) bool {
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(p.signPayload(mediaID, variant, time.Unix(exp, 0))), []byte(sig))
}

// Open 安全打开数据目录内相对路径（防目录穿越）。
func (p *Pipeline) Open(rel string) (*os.File, error) {
	abs := filepath.Join(p.dataDir, rel)
	root := filepath.Clean(p.dataDir)
	if !strings.HasPrefix(filepath.Clean(abs), root+string(filepath.Separator)) {
		return nil, fs.ErrInvalid
	}
	return os.Open(abs)
}

// PlaceholderThumb 视频缩略图占位（零 ffmpeg 依赖，DESIGN 4.8）。
// AudioPlaceholder 音频缩略图占位（D4，mock 契约同款：蓝底 + 「音频 N」）。
func AudioPlaceholder(id int64) (string, []byte) {
	svg := "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"512\" height=\"512\"><rect width=\"100%\" height=\"100%\" fill=\"#a0c4ff\"/><text x=\"50%\" y=\"50%\" font-size=\"28\" fill=\"#fff\" text-anchor=\"middle\" dominant-baseline=\"middle\">音频 " + fmt.Sprintf("%d", id) + "</text></svg>"
	return "image/svg+xml;charset=utf-8", []byte(svg)
}

func PlaceholderThumb() (string, []byte) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="1080" height="1080"><rect width="1080" height="1080" fill="#F0EDE8"/><polygon points="440,420 660,540 440,660" fill="#8B6F47"/></svg>`
	return "image/svg+xml", []byte(svg)
}

// DetectContentType 透传标准库探测（供 file 响应头）。
func DetectContentType(bs []byte) string { return http.DetectContentType(bs) }

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
