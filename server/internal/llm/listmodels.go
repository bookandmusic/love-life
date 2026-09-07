package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bookandmusic/love-life/server/internal/textutil"
)

// ListModels 按协议调用供应商的模型列表接口（AD1 fetch-models 的上游轻调用）。
// chat 侧的协议适配归 L2；此处仅做「列模型」，超时 10s。
func ListModels(ctx context.Context, apiType, baseURL, apiKey string) ([]string, error) {
	base := strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	var url string
	switch apiType {
	case "openai-chat", "openai-responses":
		url = base + "/models"
	case "anthropic":
		url = base + "/v1/models"
	case "gemini":
		// Key 经请求头传递，避免落入 URL/访问日志（其它协议同理走 header）
		url = base + "/v1beta/models"
	default:
		return nil, ErrUnsupportedAPIType
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("base_url 无效: %w", err)
	}
	switch apiType {
	case "openai-chat", "openai-responses":
		req.Header.Set("Authorization", "Bearer "+apiKey)
	case "anthropic":
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	case "gemini":
		req.Header.Set("x-goog-api-key", apiKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("上游返回 HTTP %d: %s", res.StatusCode, textutil.Trunc(string(body), 200))
	}

	var ids []string
	switch apiType {
	case "openai-chat", "openai-responses", "anthropic":
		var out struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, fmt.Errorf("解析模型列表失败: %w", err)
		}
		for _, m := range out.Data {
			if m.ID != "" {
				ids = append(ids, m.ID)
			}
		}
	case "gemini":
		var out struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, fmt.Errorf("解析模型列表失败: %w", err)
		}
		for _, m := range out.Models {
			ids = append(ids, strings.TrimPrefix(m.Name, "models/"))
		}
	}
	return ids, nil
}
