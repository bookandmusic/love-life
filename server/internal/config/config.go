package config

import (
	"os"
	"strconv"
)

// Config 全部来自环境变量——部署口径是"单二进制 + 一个数据目录"，不引入配置文件。
type Config struct {
	Listen        string // 监听地址
	DataDir       string // 自包含数据目录（SQLite/向量库/媒体）
	AdminUser     string // 部署时内置 admin 账号
	AdminPass     string
	MediaMaxBytes int64 // 单媒体文件软限（DESIGN 4.8 默认 500MB）
}

func Load() Config {
	maxMB := getenvInt("MEDIA_MAX_MB", 500)
	return Config{
		Listen:        getenv("LISTEN", ":8080"),
		DataDir:       getenv("DATA_DIR", "./data"),
		AdminUser:     os.Getenv("ADMIN_USER"),
		AdminPass:     os.Getenv("ADMIN_PASS"),
		MediaMaxBytes: maxMB << 20,
	}
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
