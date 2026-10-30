// Package appupdate 让服务端替 APP 从 GitHub 取回更新信息与安装包。
//
// 为什么需要：APP 直连 GitHub 在国内常超时，下载 46MB 依赖公共加速代理、质量不可控。
// 改由服务端取回后，APP 只连自己的服务端（与业务请求同一地址，含绿联通道）。
//
// 代理：GitHub 出站可配标准 HTTP(S) 代理（含可选认证），仅作用于本包——
// 模型 API / 地图瓦片 / Webhook 等其它出站各有自己的可达性，混用会因代理不回源而失败。
package appupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/store"
)

const (
	// releaseAPI GitHub 最新 release 接口
	releaseAPI = "https://api.github.com/repos/bookandmusic/love-life/releases/latest"

	// 配置与缓存用的 settings 键
	keyConfig  = "appupdate_config"
	keyRelease = "appupdate_release"
	keyETag    = "appupdate_etag"

	// metaTimeout release 元数据请求超时
	metaTimeout = 15 * time.Second
	// maxMetaBytes release JSON 上限
	maxMetaBytes = 256 * 1024
	// maxFetchBytes 单次回源上限（防异常响应撑爆磁盘；正常 APK 约 46MB）
	maxFetchBytes = 200 * 1024 * 1024

	userAgent = "love-life-server"
)

// Config 后台可配项。
type Config struct {
	// ProxyURL 标准 HTTP(S) 代理地址（如 http://host:7890）；空 = 直连
	ProxyURL string `json:"proxy_url"`
	// ProxyUser / ProxyPassEnc 代理认证（可选）；密码加密存储、只写不读
	ProxyUser    string `json:"proxy_user"`
	ProxyPassEnc string `json:"proxy_pass_enc"`
}

// Release 一份 release 的关键信息（服务端解析后下发给 APP）。
type Release struct {
	VersionName string `json:"version_name"` // 如 0.1.3
	VersionCode int    `json:"version_code"` // 由 tag 推导，供 APP 比较
	Notes       string `json:"notes"`
	MinVersion  int    `json:"min_version_code"` // 低于它强制更新；0 表示未标注
	AssetName   string `json:"asset_name"`
	AssetSize   int64  `json:"asset_size"`
	AssetURL    string `json:"asset_url"` // GitHub 上的下载地址（服务端回源用，不下发给 APP）
	FetchedAt   string `json:"fetched_at"`
}

// Service 更新中转：元数据缓存 + APK 落盘。
type Service struct {
	db     *store.DB
	key    []byte
	updDir string
}

// New 构造；updDir 为 APK 落盘目录（<DATA_DIR>/updates）。
func New(db *store.DB, key []byte, dataDir string) *Service {
	return &Service{
		db:     db,
		key:    key,
		updDir: filepath.Join(dataDir, "updates"),
	}
}

// LoadConfig 读配置（返回明文密码；未配置时返回零值）。
func (s *Service) LoadConfig(ctx context.Context) (*Config, error) {
	raw, err := s.db.GetSetting(ctx, keyConfig)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return &Config{}, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LoadConfigForDisplay 读配置供后台展示（密码不回显，只给是否已设置）。
func (s *Service) LoadConfigForDisplay(ctx context.Context) (*Config, bool, error) {
	cfg, err := s.LoadConfig(ctx)
	if err != nil {
		return nil, false, err
	}
	hasPassword := cfg.ProxyPassEnc != ""
	cfg.ProxyPassEnc = ""
	return cfg, hasPassword, nil
}

// SaveConfig 写配置；password 为空表示沿用旧密码，clearPassword 为真则清除。
func (s *Service) SaveConfig(ctx context.Context, cfg Config, password string, clearPassword bool) error {
	old, err := s.LoadConfig(ctx)
	if err != nil {
		return err
	}
	switch {
	case clearPassword:
		cfg.ProxyPassEnc = ""
	case password != "":
		enc, encErr := crypto.Encrypt(s.key, password)
		if encErr != nil {
			return encErr
		}
		cfg.ProxyPassEnc = enc
	default:
		cfg.ProxyPassEnc = old.ProxyPassEnc // 沿用
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return s.db.SetSetting(ctx, keyConfig, string(raw))
}

// client 按配置构造出站客户端：配了代理就走代理，未配则直连。
// 只用于 GitHub 出站（release 元数据与 APK 回源）。
func (s *Service) client(ctx context.Context) (*http.Client, error) {
	cfg, err := s.LoadConfig(ctx)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{}
	if strings.TrimSpace(cfg.ProxyURL) != "" {
		password := ""
		if cfg.ProxyPassEnc != "" {
			if decoded, decErr := crypto.Decrypt(s.key, cfg.ProxyPassEnc); decErr == nil {
				password = decoded
			}
		}
		proxyURL, parseErr := parseProxy(cfg.ProxyURL, cfg.ProxyUser, password)
		if parseErr != nil {
			return nil, parseErr
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return &http.Client{Transport: transport}, nil
}

// parseProxy 解析代理地址并注入可选认证。
// 只支持标准 HTTP(S) 代理（Go 的 Transport.Proxy 语义）；SOCKS5 需引入额外依赖，不做。
func parseProxy(raw, user, password string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("代理地址不合法：%w", err)
	}
	if u.Host == "" {
		return nil, errors.New("代理地址缺少主机名")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("代理只支持 http/https，收到 %q", u.Scheme)
	}
	if user != "" {
		u.User = url.UserPassword(user, password)
	}
	return u, nil
}

// Release 取最新 release：带 ETag 条件请求，命中 304 直接复用缓存。
func (s *Service) Release(ctx context.Context, force bool) (*Release, error) {
	cached, cachedETag := s.cachedRelease(ctx)
	if cached != nil && !force {
		// 有缓存且未强制刷新：仍发条件请求，探测是否有新版
		rel, notModified, err := s.fetchRelease(ctx, cachedETag)
		if err != nil {
			// 网络不可用但有缓存：退回缓存（更新检查不该因网络抖动而失败）
			return cached, nil
		}
		if notModified {
			return cached, nil
		}
		if err := s.saveRelease(ctx, rel); err != nil {
			return rel, nil
		}
		return rel, nil
	}
	rel, notModified, err := s.fetchRelease(ctx, cachedETag)
	if err != nil {
		if cached != nil {
			return cached, nil
		}
		return nil, err
	}
	// 304 且没有缓存：ETag 在但元数据丢了（例如手工清过库），退回重新无条件取一次
	if notModified {
		if cached != nil {
			return cached, nil
		}
		rel, _, err = s.fetchRelease(ctx, "")
		if err != nil {
			return nil, err
		}
	}
	if rel == nil {
		return nil, errors.New("未能获取版本信息")
	}
	if err := s.saveRelease(ctx, rel); err != nil {
		return rel, nil
	}
	return rel, nil
}

// fetchRelease 发一次条件请求；返回 (release, 未修改?)。
func (s *Service) fetchRelease(ctx context.Context, etag string) (*Release, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, metaTimeout)
	defer cancel()

	client, err := s.client(ctx)
	if err != nil {
		return nil, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseAPI, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("无法连接 GitHub：%w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotModified {
		return nil, true, nil
	}
	if res.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("GitHub 返回 HTTP %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxMetaBytes))
	if err != nil {
		return nil, false, err
	}
	rel, err := parseRelease(raw)
	if err != nil {
		return nil, false, err
	}
	// ETag 与 release 一起存：下次条件请求用它
	if tag := res.Header.Get("ETag"); tag != "" {
		_ = s.db.SetSetting(context.Background(), keyETag, tag)
	}
	return rel, false, nil
}

func (s *Service) cachedRelease(ctx context.Context) (*Release, string) {
	rawETag, _ := s.db.GetSetting(ctx, keyETag)
	raw, err := s.db.GetSetting(ctx, keyRelease)
	if err != nil {
		return nil, rawETag
	}
	var rel Release
	if err := json.Unmarshal([]byte(raw), &rel); err != nil {
		return nil, rawETag
	}
	return &rel, rawETag
}

func (s *Service) saveRelease(ctx context.Context, rel *Release) error {
	rel.FetchedAt = time.Now().UTC().Format(time.RFC3339)
	raw, err := json.Marshal(rel)
	if err != nil {
		return err
	}
	return s.db.SetSetting(ctx, keyRelease, string(raw))
}

// ---- release JSON 解析 ----

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Body       string `json:"body"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name               string `json:"name"`
		Size               int64  `json:"size"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// parseRelease 从 GitHub 响应里取出发布信息。
// 版本号来自 tag（形如 v0.1.3）；强制更新的最低版本来自 release body 的 `min-version-code: N` 标注。
func parseRelease(raw []byte) (*Release, error) {
	var gr githubRelease
	if err := json.Unmarshal(raw, &gr); err != nil {
		return nil, fmt.Errorf("release 响应不是合法 JSON：%w", err)
	}
	if gr.Draft {
		return nil, errors.New("最新 release 是草稿")
	}
	version := strings.TrimPrefix(gr.TagName, "v")
	code := versionCode(version)
	if code == 0 {
		return nil, fmt.Errorf("无法从 tag %q 解析版本号", gr.TagName)
	}
	asset := pickAPK(gr.Assets)
	if asset == nil {
		return nil, errors.New("release 中没有 APK 资产")
	}
	return &Release{
		VersionName: version,
		VersionCode: code,
		Notes:       strings.TrimSpace(gr.Body),
		MinVersion:  parseMinVersion(gr.Body),
		AssetName:   asset.Name,
		AssetSize:   asset.Size,
		AssetURL:    asset.BrowserDownloadURL,
	}, nil
}

// versionCode 把 x.y.z 折算成可比较的整数（x*10000 + y*100 + z）。
func versionCode(v string) int {
	parts := strings.Split(v, ".")
	if len(parts) < 3 {
		return 0
	}
	nums := make([]int, 0, 3)
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil || n < 0 {
			return 0
		}
		nums = append(nums, n)
	}
	return nums[0]*10000 + nums[1]*100 + nums[2]
}

// parseMinVersion 从 release 说明里读 `min-version-code: N`（0 表示未标注）。
func parseMinVersion(body string) int {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if !strings.HasPrefix(lower, "min-version-code:") {
			continue
		}
		val := strings.TrimSpace(lower[len("min-version-code:"):])
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return 0
		}
		return n
	}
	return 0
}

func pickAPK(assets []struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}) *struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
} {
	for i := range assets {
		name := strings.ToLower(assets[i].Name)
		if strings.HasSuffix(name, ".apk") {
			return &assets[i]
		}
	}
	return nil
}
