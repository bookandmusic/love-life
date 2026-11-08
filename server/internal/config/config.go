package config

import (
	"os"
	"strconv"
	"strings"
)

// defaultMediaMaxMB 单媒体文件软限默认值（MB）；可用 MEDIA_MAX_MB 覆盖。
const defaultMediaMaxMB = 500

// Config 全部来自环境变量——部署口径是"单二进制 + 一个数据目录"，不引入配置文件。
type Config struct {
	Listen         string // 监听地址
	DataDir        string // 自包含数据目录（SQLite/向量库/媒体）
	AdminUser      string // 部署时内置 admin 账号
	AdminPass      string
	MediaMaxBytes  int64    // 单媒体文件软限（默认 500MB）
	TrustedProxies []string // 受信反代的 IP/CIDR（TRUSTED_PROXIES，逗号分隔）；空 = 不信任任何转发头
}

func Load() Config {
	maxMB := getenvInt("MEDIA_MAX_MB", defaultMediaMaxMB)
	return Config{
		Listen:         getenv("LISTEN", ":8080"),
		DataDir:        getenv("DATA_DIR", "./data"),
		AdminUser:      os.Getenv("ADMIN_USER"),
		AdminPass:      os.Getenv("ADMIN_PASS"),
		MediaMaxBytes:  maxMB << 20,
		TrustedProxies: splitList(os.Getenv("TRUSTED_PROXIES")),
	}
}

// splitList 逗号分隔的环境变量值 → 去空白、丢空项；空串返回 nil（等于未配置）。
func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := make([]string, 0, 4)
	for _, item := range strings.Split(raw, ",") {
		if v := strings.TrimSpace(item); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func getenvInt(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
