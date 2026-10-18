// Package media 媒体上传管线：两段式上传（临时区→发布转正）、
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
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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

// ErrTooLarge 单文件超过大小上限（可被 errors.Is 识别）。
var ErrTooLarge = errors.New("文件超过大小上限")

// ErrEmptyFile 上传的文件是 0 字节。必须在收尾时统一拒绝：照片分支会因缩略图解码失败
// 偶然挡住空文件，而视频 / 音频只做容器头时长解析、解析失败按「不阻塞上传」静默放过，
// 空文件于是落库成 0:00 的假媒体条目。三类媒体同一处守卫（ARCHITECTURE「媒体上传管线」格式与大小）。
var ErrEmptyFile = errors.New("文件是空的，请重新选择")

// ErrUploadNotFound 分片上传的目标不存在：token 非法、已被 janitor 清理、或归属不符。
// 归属不符也归到这里——不向调用方区分「不存在」与「不是你的」，免得成为探测他人上传的手段。
var ErrUploadNotFound = errors.New("上传不存在或已过期")

// OffsetMismatchError 分片 offset 与服务端已收字节不符。
// 带实际长度：409 响应要把长度作为数字回给客户端做重同步，字符串里带数字没法可靠解析。
type OffsetMismatchError struct{ Received int64 }

func (e *OffsetMismatchError) Error() string {
	return fmt.Sprintf("分片位置不符：服务端已收 %d 字节", e.Received)
}

// tokenRe 临时区 token 格式（32 位 hex）；分片上传的 upload_id 与它同一命名空间。
// 校验它是为了防路径穿越（token 会拼进文件路径）。
var tokenRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// errTooLarge 超限错误：文案在此唯一拼装，HTTP 层的 413 响应经 TooLargeMessage 复用同一句。
func errTooLarge(maxBytes int64) error {
	return fmt.Errorf("%w（%d MB）", ErrTooLarge, maxBytes/(1<<20))
}

// TooLargeMessage 超限响应文案；与 errTooLarge 同一句话，
// 使「HTTP 层截断」与「SaveTemp 判限」两个来源对外说法一致。
func TooLargeMessage(maxBytes int64) string {
	return errTooLarge(maxBytes).Error()
}

type Pipeline struct {
	dataDir  string
	secret   []byte // 与 JWT 共用密钥材料的 HMAC（仅用于签名 URL，用途域分隔）
	maxBytes int64
}

func New(dataDir string, secret []byte, maxBytes int64) *Pipeline {
	return &Pipeline{dataDir: dataDir, secret: secret, maxBytes: maxBytes}
}

// MaxBytes 单文件大小上限（HTTP 层据此设置请求体上限）。
func (p *Pipeline) MaxBytes() int64 { return p.maxBytes }

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
	Kind     string `json:"kind"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Size     int64  `json:"size"`
	Duration float64 `json:"duration,omitempty"`
	TakenAt  string `json:"taken_at,omitempty"`
	OwnerID  int64  `json:"owner_id,omitempty"` // 上传者：discard 归属校验用（ARCHITECTURE「媒体上传管线」）
	// Finalized 已收尾：分片上传的末片响应丢失后，客户端重发末片时据它直接取回结果
	// （否则 offset 已落后、只会拿到 409，客户端卡在「传完了却拿不到 token」）。
	Finalized bool `json:"finalized,omitempty"`
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
// ownerID 记录上传者，discard 撤销时校验归属（防跨用户误删）。
// 整文件一次到齐的路径（无分片头的老客户端）走这里；分片上传走 Begin/Append/Finalize 三段。
func (p *Pipeline) SaveTemp(r io.Reader, filename string, ownerID int64) (*Uploaded, error) {
	up, err := p.BeginUpload(filename, ownerID)
	if err != nil {
		return nil, err
	}
	if err := p.AppendChunk(up.Token, 0, r, ownerID); err != nil {
		return nil, err
	}
	return p.FinalizeUpload(up.Token)
}

// BeginUpload 开启一次上传：定 kind/ext、建空文件与 sidecar（记 owner），返回带 token 的 Uploaded
// （此时 Size 为 0，元数据待收尾时补全）。token 即临时区文件名，也是分片协议里的 upload_id。
func (p *Pipeline) BeginUpload(filename string, ownerID int64) (*Uploaded, error) {
	kind, ext, err := kindOf(filename)
	if err != nil {
		return nil, err
	}
	token := randHex(16)
	tmpDir := filepath.Join(p.dataDir, "media", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(p.dataDir, "thumbs", "tmp"), 0o755); err != nil {
		return nil, err
	}

	rawPath := filepath.Join(tmpDir, token+ext)
	if err := os.WriteFile(rawPath, nil, 0o600); err != nil {
		return nil, err
	}
	// sidecar 先落 owner：分片期间就要能被 Discard 认领（归属校验依赖它）。
	if err := p.writeTempMeta(token, tempMeta{Kind: kind, OwnerID: ownerID}); err != nil {
		_ = os.Remove(rawPath) // 此刻只有主文件，无 sidecar/缩略图可清
		return nil, err
	}
	return &Uploaded{Token: token, Kind: kind}, nil
}

// AppendChunk 向已开启的上传追加一片。offset 必须等于当前已收字节数（顺序传片），
// 否则返回 *OffsetMismatchError（带实际长度，供客户端重同步）。
// 累计超过上限即删除半成品并返回 ErrTooLarge——该次上传已不可能成功，留着只会占盘。
func (p *Pipeline) AppendChunk(token string, offset int64, r io.Reader, ownerID int64) error {
	if !tokenRe.MatchString(token) {
		return ErrUploadNotFound
	}
	meta, err := p.readTempMeta(token)
	if err != nil {
		return err // 含 ErrUploadNotFound（半成品已被 janitor 清理）
	}
	if meta.OwnerID != ownerID {
		return ErrUploadNotFound // 归属不符按「不存在」处理，不泄露他人上传的存在性
	}
	rawPath, _, ok := p.tmpPaths(token)
	if !ok {
		return ErrUploadNotFound
	}
	info, err := os.Stat(rawPath)
	if err != nil {
		return ErrUploadNotFound
	}
	if info.Size() != offset {
		return &OffsetMismatchError{Received: info.Size()}
	}

	f, err := os.OpenFile(rawPath, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	// 上限 +1：写满即停。必须用 LimitReader 而非 CopyN——CopyN 在源比 n 短时
	// 返回 io.EOF，会命中下面的 err != nil 分支把正常片判成失败。
	written, err := io.Copy(f, io.LimitReader(r, p.maxBytes-offset+1))
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		// 传输中断：清理半成品。整文件路径（SaveTemp）历来如此清理，
		// 分片路径若只标记而不清，每片失败都会留一份垃圾到 janitor 的 24h 兜底。
		p.discardTemp(token)
		return err
	}
	if offset+written > p.maxBytes {
		p.discardTemp(token)
		return errTooLarge(p.maxBytes)
	}
	return nil
}

// discardTemp 清理一次上传的全部临时产物（主文件 + sidecar + 缩略图）。
// 绕过 Discard 的归属校验：调用点已完成归属确认（这里只处理「自己的」半成品）。
func (p *Pipeline) discardTemp(token string) {
	if raw, thumb, ok := p.tmpPaths(token); ok {
		_ = os.Remove(raw)
		if thumb != "" {
			_ = os.Remove(thumb)
		}
	}
	_ = os.Remove(p.tempMetaPath(token))
}

// FinalizeUpload 收尾：生成缩略图、解析时长/EXIF、补全 sidecar 并置 finalized。
// 文件留在临时区不动——移入正式区仍由 Promote 在建动态时完成（既有分工不变）。
// 已收尾的上传可重复调用（幂等），供「末片响应丢失后重发」直接取回结果。
func (p *Pipeline) FinalizeUpload(token string) (*Uploaded, error) {
	if !tokenRe.MatchString(token) {
		return nil, ErrUploadNotFound
	}
	meta, err := p.readTempMeta(token)
	if err != nil {
		return nil, err
	}
	rawPath, _, ok := p.tmpPaths(token)
	if !ok {
		return nil, ErrUploadNotFound
	}
	if meta.Finalized {
		// 幂等：上次已收尾（sidecar 已带完整元数据），直接回同样结果
		return &Uploaded{Token: token, Kind: meta.Kind, Size: meta.Size,
			Width: meta.Width, Height: meta.Height, Duration: meta.Duration, TakenAt: meta.TakenAt}, nil
	}

	up := &Uploaded{Token: token, Kind: meta.Kind}
	if info, serr := os.Stat(rawPath); serr == nil {
		up.Size = info.Size()
	}

	// 空文件三类一律拒收：放在 photo 解码之前，让「空的 jpg」也得到面向用户的那句文案，
	// 而不是「生成缩略图失败: decode: image: unknown format」。放在 finalized 幂等分支之后，
	// 使已收尾的上传重发末片仍能取回结果（协议幂等不被历史空文件卡死）。
	if up.Size == 0 {
		p.discardTemp(token)
		return nil, ErrEmptyFile
	}

	if meta.Kind == "photo" {
		w, h, thumbErr := makeThumb(rawPath, filepath.Join(p.dataDir, "thumbs", "tmp", token+".jpg"))
		if thumbErr != nil {
			p.discardTemp(token)
			return nil, fmt.Errorf("生成缩略图失败: %w", thumbErr)
		}
		up.Width, up.Height = w, h
		// EXIF 拍摄时间（缺失回退上传时间，落库时处理），GPS 忽略
		up.TakenAt = EXIFDateTimeOriginal(rawPath)
	}
	if meta.Kind == "audio" {
		// 音频时长（秒）；解析失败不阻塞上传（duration 留空）
		if d, derr := audioDuration(rawPath); derr == nil && d > 0 {
			up.Duration = d
		}
	}
	if meta.Kind == "video" {
		// 视频时长（秒）：容器头自解析（mp4/mov 的 mvhd、webm 的 Info.Duration）；
		// 失败同样不阻塞上传（前端时长角标留空，播放占位图照常）
		if d, derr := videoDuration(rawPath, filepath.Ext(rawPath)); derr == nil && d > 0 {
			up.Duration = d
		}
	}

	meta.Width, meta.Height, meta.Size = up.Width, up.Height, up.Size
	meta.Duration, meta.TakenAt = up.Duration, up.TakenAt
	meta.Finalized = true
	if err := p.writeTempMeta(token, meta); err != nil {
		p.discardTemp(token)
		return nil, err
	}
	return up, nil
}

// UploadState 只读查询一次上传的状态：已收字节数与是否已收尾（校验归属）。
// 供 api 层回 {received} 回执、以及末片重发时判定「是否已收尾」——**不能拿
// FinalizeUpload 当查询用**：那会把一个还没收完的上传提前收尾（缩略图/元数据都基于半截文件）。
func (p *Pipeline) UploadState(token string, ownerID int64) (received int64, finalized bool, err error) {
	if !tokenRe.MatchString(token) {
		return 0, false, ErrUploadNotFound
	}
	meta, err := p.readTempMeta(token)
	if err != nil {
		return 0, false, err
	}
	if meta.OwnerID != ownerID {
		return 0, false, ErrUploadNotFound
	}
	rawPath, _, ok := p.tmpPaths(token)
	if !ok {
		return 0, false, ErrUploadNotFound
	}
	info, err := os.Stat(rawPath)
	if err != nil {
		return 0, false, ErrUploadNotFound
	}
	return info.Size(), meta.Finalized, nil
}

// tempMetaPath / writeTempMeta / readTempMeta sidecar 读写（owner 与 finalized 的载体）。
func (p *Pipeline) tempMetaPath(token string) string {
	return filepath.Join(p.dataDir, "media", "tmp", token+".json")
}

func (p *Pipeline) writeTempMeta(token string, m tempMeta) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(p.tempMetaPath(token), b, 0o600)
}

func (p *Pipeline) readTempMeta(token string) (tempMeta, error) {
	var m tempMeta
	b, err := os.ReadFile(p.tempMetaPath(token))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return m, ErrUploadNotFound
		}
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("临时媒体元数据损坏: %w", err)
	}
	return m, nil
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

// Discard 主动撤销临时文件：放弃 / 取消 / 卡片过期 / 把关作废时由客户端调用。
// 逐个读 sidecar 校验归属后删 tmp 文件与缩略图（含 sidecar）；幂等——token 不存在或
// 归属不符跳过不报错；返回实际清理数。ARCHITECTURE「媒体上传管线」主动撤销。
func (p *Pipeline) Discard(tokens []string, userID int64) int {
	removed := 0
	for _, token := range tokens {
		if !tokenRe.MatchString(token) {
			continue // 非法 token 一律跳过（防路径穿越与误删）
		}
		raw, thumb, ok := p.tmpPaths(token)
		if !ok {
			continue
		}
		sidecar := filepath.Join(p.dataDir, "media", "tmp", token+".json")
		// sidecar 的「缺失」与「损坏」要分开处理（ARCHITECTURE「媒体上传管线」的临时区元数据行）：
		//  - 文件不存在：可能已被 janitor 认领/清理，属正常收敛 → 仍放行删除，
		//    否则过期的放弃/重试会留下删不掉的临时文件（用户看到失败，还得等 24h 兜底）；
		//  - 文件存在但读不出/解析失败，或 OwnerID 缺失：被篡改或写坏的信号 → 拒绝删除并告警。
		switch b, err := os.ReadFile(sidecar); {
		case err == nil:
			var meta tempMeta
			if jerr := json.Unmarshal(b, &meta); jerr != nil {
				slog.Warn("临时文件 sidecar 解析失败，拒绝删除", "token", token, "err", jerr)
				continue
			}
			if meta.OwnerID == 0 {
				slog.Warn("临时文件 sidecar 缺归属，拒绝删除", "token", token)
				continue
			}
			if meta.OwnerID != userID {
				continue // 归属不符：不是你的临时文件，跳过
			}
		case !errors.Is(err, fs.ErrNotExist):
			slog.Warn("临时文件 sidecar 读取失败，拒绝删除", "token", token, "err", err)
			continue
		}
		if os.Remove(raw) == nil {
			removed++
		}
		if thumb != "" {
			_ = os.Remove(thumb)
		}
		_ = os.Remove(sidecar)
	}
	return removed
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

// ---- 签名 URL（短期签名）----

// signedURLTTL 媒体签名 URL 有效期（默认 15 分钟）。
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

// signAvatarPayload 头像签名载荷（独立命名空间 avatar:<userID>，与 media 签名隔离）。
func (p *Pipeline) signAvatarPayload(userID int64, exp time.Time) string {
	mac := hmac.New(sha256.New, p.secret)
	fmt.Fprintf(mac, "avatar:%d:%d", userID, exp.Unix())
	return hex.EncodeToString(mac.Sum(nil))
}

// SignedAvatarURL 头像签名 URL（独立通道，不归属 media 表）。
func (p *Pipeline) SignedAvatarURL(userID int64) string {
	exp := time.Now().Add(signedURLTTL)
	return fmt.Sprintf("/api/v1/avatar/%d?exp=%d&sig=%s", userID, exp.Unix(), p.signAvatarPayload(userID, exp))
}

// VerifyAvatarSig 头像签名校验。
func (p *Pipeline) VerifyAvatarSig(userID int64, expStr, sig string) bool {
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(p.signAvatarPayload(userID, time.Unix(exp, 0))), []byte(sig))
}

// SaveAvatar 保存头像图片到正式区（avatars/<YYYY>/<MM>/<rand>.<ext>），返回相对数据目录的路径。
// 仅接受照片格式；视频/音频一律拒绝。独立通道，不走媒体表与临时区两段式流程。
func (p *Pipeline) SaveAvatar(r io.Reader, filename string) (string, error) {
	kind, ext, err := kindOf(filename)
	if err != nil {
		return "", err
	}
	if kind != "photo" {
		return "", ErrUnsupportedFormat
	}
	now := time.Now()
	finalDir := filepath.Join(p.dataDir, "avatars", now.Format("2006"), now.Format("01"))
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		return "", err
	}
	name := randHex(16) + ext
	dst := filepath.Join(finalDir, name)
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(dst)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(dst)
		return "", err
	}
	return filepath.Rel(p.dataDir, dst)
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

// PlaceholderThumb 视频缩略图占位（零 ffmpeg 依赖）。
// AudioPlaceholder 音频缩略图占位（mock 契约同款：蓝底 + 「音频 N」）。
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
