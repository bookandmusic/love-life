// upstream-check.mjs 前置：向指定 DATA_DIR 的库直插 mock 上游供应商（幂等）。
// 用法：cd server && go run ../.verify/seedproviders/main.go -data /tmp/up-data [-mock http://localhost:8787/upstream] [-only openai-chat]
// Key 加密读 server 同款 jwt_secret 派生密钥，保证运行时解密成功；
// -only 只保留指定协议（便于逐协议验证链头选择），缺省插全四条（验证链序）。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/store"
)

func main() {
	data := flag.String("data", "", "DATA_DIR 路径（必填）")
	mock := flag.String("mock", "http://localhost:8787/upstream", "mock 上游 base")
	only := flag.String("only", "", "仅保留该协议（openai-chat/openai-responses/anthropic/gemini）")
	flag.Parse()
	if *data == "" {
		fmt.Println("-data 必填")
		os.Exit(1)
	}

	secret, err := os.ReadFile(filepath.Join(*data, "jwt_secret"))
	if err != nil {
		fmt.Printf("读取 jwt_secret 失败（server 应已启动过一次）: %v\n", err)
		os.Exit(1)
	}
	enc, err := crypto.Encrypt(crypto.KeyFrom(secret), "mock-key")
	if err != nil {
		fmt.Printf("加密失败: %v\n", err)
		os.Exit(1)
	}

	db, err := store.Open(*data)
	if err != nil {
		fmt.Printf("打开库失败: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	if _, err := db.Exec(`DELETE FROM model_providers WHERE name = 'mock-upstream'`); err != nil {
		fmt.Printf("清理旧记录失败: %v\n", err)
		os.Exit(1)
	}

	all := []struct{ apiType, model string }{
		{"openai-chat", "gpt-4o"},
		{"openai-responses", "gpt-5"},
		{"anthropic", "claude-sonnet-4-20250514"},
		{"gemini", "gemini-2.5-flash"},
	}
	for i, p := range all {
		if *only != "" && p.apiType != *only {
			continue
		}
		if _, err := db.Exec(
			`INSERT INTO model_providers (api_type, name, base_url, api_key_enc, default_model, sort_order) VALUES (?, 'mock-upstream', ?, ?, ?, ?)`,
			p.apiType, *mock+"/"+p.apiType, enc, p.model, i); err != nil {
			fmt.Printf("插入 %s 失败: %v\n", p.apiType, err)
			os.Exit(1)
		}
		fmt.Printf("seeded %s → %s/%s (default_model=%s)\n", p.apiType, *mock, p.apiType, p.model)
	}
}
