package appupdate

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// ---- release 解析 ----

const sampleRelease = `{
  "tag_name": "v0.1.4",
  "body": "### 更新内容\n- 修了某个问题\n\nmin-version-code: 1003\n",
  "draft": false,
  "prerelease": false,
  "assets": [
    {"name": "love-life-0.1.4-app-universal-release.apk", "size": 48234496, "browser_download_url": "https://github.com/x/y/releases/download/v0.1.4/app.apk"},
    {"name": "love-life-0.1.4-app-universal-release.aab", "size": 40000000, "browser_download_url": "https://github.com/x/y/releases/download/v0.1.4/app.aab"}
  ]
}`

func TestParseRelease(t *testing.T) {
	rel, err := parseRelease([]byte(sampleRelease))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if rel.VersionName != "0.1.4" {
		t.Fatalf("版本号不符: %s", rel.VersionName)
	}
	if rel.VersionCode != 1004 {
		t.Fatalf("versionCode 不符: %d", rel.VersionCode)
	}
	if rel.AssetName != "love-life-0.1.4-app-universal-release.apk" {
		t.Fatalf("选中了非 APK 资产: %s", rel.AssetName)
	}
	if rel.MinVersion != 1003 {
		t.Fatalf("最低可用版本不符: %d", rel.MinVersion)
	}
}

func TestParseReleaseRejectsDraft(t *testing.T) {
	body := `{"tag_name":"v0.1.4","draft":true,"assets":[{"name":"a.apk","size":1,"browser_download_url":"u"}]}`
	if _, err := parseRelease([]byte(body)); err == nil {
		t.Fatal("草稿 release 应当报错")
	}
}

func TestParseReleaseRejectsNoAPK(t *testing.T) {
	body := `{"tag_name":"v0.1.4","draft":false,"assets":[{"name":"a.aab","size":1,"browser_download_url":"u"}]}`
	if _, err := parseRelease([]byte(body)); err == nil {
		t.Fatal("无 APK 资产应当报错")
	}
}

// TestVersionCode 钉死量纲：与 APK 内 versionCode 同公式（x*1000000 + y*1000 + z），
// 改这里等于改客户端的更新判定，别朝代数值靠拢。
func TestVersionCode(t *testing.T) {
	cases := map[string]int{
		"0.1.3":    1003,
		"1.2.3":    1002003,
		"0.0.1":    1,
		"10.20.30": 10020030,
		"bad":      0,
		"1.2":      0,
	}
	for in, want := range cases {
		if got := versionCode(in); got != want {
			t.Fatalf("versionCode(%q) = %d，期望 %d", in, got, want)
		}
	}
}

func TestParseMinVersion(t *testing.T) {
	if got := parseMinVersion("正文\nmin-version-code: 1003000\n更多"); got != 1003000 {
		t.Fatalf("未解析到最低版本: %d", got)
	}
	if got := parseMinVersion("MIN-VERSION-CODE: 200"); got != 200 {
		t.Fatalf("大小写不敏感匹配失败: %d", got)
	}
	if got := parseMinVersion("没有标注"); got != 0 {
		t.Fatalf("无标注应返回 0: %d", got)
	}
	if got := parseMinVersion("min-version-code: abc"); got != 0 {
		t.Fatalf("非法值应返回 0: %d", got)
	}
}

// ---- 预存包模型的不变量 ----

// TestReadyReleaseNeedsLocalAPK 守住「预存包」模型的核心不变量：
// 元数据存在但本地没有 APK 时，ReadyRelease 必须返回 nil——
// 否则 APP 会看到「可更新」却下载 404，用户以为有更新却装不上。
func TestReadyReleaseNeedsLocalAPK(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := New(db, crypto.KeyFrom([]byte("test-secret")), t.TempDir())
	ctx := context.Background()

	// 没有元数据：无就绪版本
	if got := svc.ReadyRelease(ctx); got != nil {
		t.Fatal("无元数据时应返回 nil")
	}

	// 写入元数据但**不落盘 APK**：仍应无就绪版本
	rel := &Release{
		VersionName: "0.1.4",
		VersionCode: 104,
		AssetName:   "app.apk",
		AssetSize:   1234,
		AssetURL:    "https://example.invalid/app.apk",
	}
	if err := svc.saveRelease(ctx, rel); err != nil {
		t.Fatal(err)
	}
	if got := svc.ReadyRelease(ctx); got != nil {
		t.Fatal("元数据存在但本地无包时应返回 nil（否则 APP 会下载 404）")
	}

	// 落盘后才算就绪
	path := svc.apkPath(rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fake-apk"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := svc.ReadyRelease(ctx)
	if got == nil || got.VersionName != "0.1.4" {
		t.Fatalf("落盘后应就绪，实际 %+v", got)
	}
}

// ---- 超时估算（慢链路必须给够时间）----

// TestEstimateTimeout 守住「慢链路不能半途而废」：46MB 在 250KB/s 下约 3 分钟，
// 估算值必须显著大于该值（含重试与抖动余量），且不设无限等待。
func TestEstimateTimeout(t *testing.T) {
	// 46MB：至少 5 分钟（下限），且不超过 60 分钟（上限）
	got := estimateTimeout(46 * 1024 * 1024)
	if got < 5*time.Minute {
		t.Fatalf("46MB 的超时不应低于 5 分钟，实际 %v", got)
	}
	if got > 60*time.Minute {
		t.Fatalf("不应超过 60 分钟，实际 %v", got)
	}
	// 极大体积被上限截住
	if big := estimateTimeout(10 * 1024 * 1024 * 1024); big != 60*time.Minute {
		t.Fatalf("超大文件应截到 60 分钟，实际 %v", big)
	}
	// 体积未知时给一个合理默认值
	if unknown := estimateTimeout(0); unknown != 15*time.Minute {
		t.Fatalf("体积未知时应 15 分钟，实际 %v", unknown)
	}
}

// ---- 文件名防御 ----

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"app.apk":          "app.apk",
		"../../etc/passwd": "passwd",
		"a/b/c.apk":        "c.apk",
		"":                 "app.apk",
		".":                "app.apk",
		"..":               "app.apk",
	}
	for in, want := range cases {
		if got := sanitizeName(in); got != want {
			t.Fatalf("sanitizeName(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// ---- 清理只保留最新版本 ----

func TestCleanupExcept(t *testing.T) {
	dir := t.TempDir()
	svc := &Service{updDir: filepath.Join(dir, "updates")}
	for _, v := range []string{"0.1.3", "0.1.4", "0.1.5"} {
		d := filepath.Join(svc.updDir, v)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "a.apk"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.cleanupExcept("0.1.5"); err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	entries, err := os.ReadDir(svc.updDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "0.1.5" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("清理后应只剩 0.1.5，实际 %v", names)
	}
}

// ---- 代理配置 ----

func TestParseProxyURLWithAuth(t *testing.T) {
	// 校验 url.UserPassword 注入后 ProxyURL 能正确解析（服务端构造走同一路径）
	u, err := parseProxy("http://host:7890", "user", "pa:ss")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if u.Host != "host:7890" {
		t.Fatalf("host 不符: %s", u.Host)
	}
	pw, _ := u.User.Password()
	if u.User.Username() != "user" || pw != "pa:ss" {
		t.Fatalf("凭据注入失败: %s / %s", u.User.Username(), pw)
	}
}

func TestProxyEnabled(t *testing.T) {
	// 旧配置没有 use_proxy 字段（nil）：默认启用，行为不变
	if !(Config{ProxyURL: "http://host:7890"}).UseProxyEnabled() {
		t.Fatal("nil use_proxy 应默认启用")
	}
	// 显式关闭：即使配了代理也不走
	if (Config{ProxyURL: "http://host:7890", UseProxy: boolPtr(false)}).UseProxyEnabled() {
		t.Fatal("use_proxy=false 应禁用代理")
	}
	if !(Config{ProxyURL: "http://host:7890", UseProxy: boolPtr(true)}).UseProxyEnabled() {
		t.Fatal("use_proxy=true 应启用代理")
	}
}

func TestClientHonorsUseProxy(t *testing.T) {
	// 开关关闭时，client 不得挂代理（直连）
	ctx := context.Background()
	svc := &Service{}
	cfg := Config{ProxyURL: "http://127.0.0.1:1", UseProxy: boolPtr(false)}
	client, err := svc.clientForTest(ctx, cfg)
	if err != nil {
		t.Fatalf("构造 client 失败: %v", err)
	}
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport 类型不符")
	}
	if tr.Proxy != nil {
		t.Fatal("use_proxy=false 仍挂了代理")
	}

	cfgOn := Config{ProxyURL: "http://127.0.0.1:1", UseProxy: boolPtr(true)}
	clientOn, err := svc.clientForTest(ctx, cfgOn)
	if err != nil {
		t.Fatalf("构造 client 失败: %v", err)
	}
	trOn := clientOn.Transport.(*http.Transport)
	if trOn.Proxy == nil {
		t.Fatal("use_proxy=true 未挂代理")
	}
}

func boolPtr(b bool) *bool { return &b }

// git tag 允许 `/`（如 v0.1.4.5/evil）：版本号既拼进更新目录路径、又作为目录名参与清理比对，
// 进路径前必须收成单段，否则下载目录被拆成多级、清理会误删刚建的目录。
func TestParseReleaseSanitizesVersionName(t *testing.T) {
	raw := `{"tag_name":"v0.1.4.5/evil","draft":false,"assets":[{"name":"a.apk","size":1,"browser_download_url":"u"}]}`
	rel, err := parseRelease([]byte(raw))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if strings.ContainsAny(rel.VersionName, `/\`) {
		t.Fatalf("VersionName 含路径分隔符: %q", rel.VersionName)
	}
	if rel.VersionName != "0.1.4.5_evil" {
		t.Fatalf("VersionName = %q，want 0.1.4.5_evil", rel.VersionName)
	}
	// versionCode 用净化前的 tag 版本计算（只取前三段），不受尾段影响
	if rel.VersionCode != 1004 {
		t.Fatalf("VersionCode = %d，want 1004", rel.VersionCode)
	}

	svc := New(nil, nil, t.TempDir())
	relPath, err := filepath.Rel(svc.updDir, svc.apkPath(rel))
	if err != nil {
		t.Fatalf("apkPath 不是 updDir 下的相对路径: %v", err)
	}
	if relPath == "." || strings.HasPrefix(relPath, "..") || filepath.IsAbs(relPath) {
		t.Fatalf("apkPath 越出 updDir: %q", relPath)
	}
	if want := filepath.Join("0.1.4.5_evil", "a.apk"); relPath != want {
		t.Fatalf("apkPath = %q，want %q", relPath, want)
	}
}

func TestSafeVersionName(t *testing.T) {
	cases := map[string]string{
		"0.1.4":        "0.1.4",
		"0.1.4.5/evil": "0.1.4.5_evil",
		`0.1.4\evil`:   "0.1.4_evil",
		"":             "unknown",
		".":            "unknown",
		"..":           "unknown",
	}
	for in, want := range cases {
		if got := safeVersionName(in); got != want {
			t.Fatalf("safeVersionName(%q) = %q，want %q", in, got, want)
		}
	}
}
