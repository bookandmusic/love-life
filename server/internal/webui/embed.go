// Package webui 内嵌后台管理页面构建产物。
// 构建流程：admin/ 目录 npm run build → 产物复制到本包 dist/（占位文件仅保证首次编译通过）。
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS 返回以 dist 为根的文件系统（可直接交给 http.FileServerFS）。
func FS() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
