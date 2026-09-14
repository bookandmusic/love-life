package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/bookandmusic/love-life/server/internal/llm"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// 嵌入模型配置（语义检索）：复用某个既有模型供应商的地址与 Key 调 OpenAI 兼容
// /embeddings。未配置 = 语义检索关闭（检索回落关键词），故 PUT 允许 provider_id=0 清空。
type embeddingAPI struct {
	db *store.DB
}

// embeddingConfig 请求与响应同形（provider_id + model）。
type embeddingConfig struct {
	ProviderID int64  `json:"provider_id"`
	Model      string `json:"model"`
}

func (a *embeddingAPI) get(w http.ResponseWriter, r *http.Request) {
	cfg, err := a.db.GetEmbeddingConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusOK, embeddingConfig{ProviderID: cfg.ProviderID, Model: cfg.Model})
}

func (a *embeddingAPI) put(w http.ResponseWriter, r *http.Request) {
	var req embeddingConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	req.Model = strings.TrimSpace(req.Model)
	if req.ProviderID == 0 {
		req.Model = "" // 清空 = 关闭语义检索
	} else {
		// 显式要求模型名：不复用供应商的默认聊天模型，避免拿聊天模型去调 /embeddings。
		if req.Model == "" {
			writeError(w, http.StatusBadRequest, "model 必填")
			return
		}
		p, err := a.db.GetProvider(r.Context(), req.ProviderID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusBadRequest, "供应商不存在")
				return
			}
			writeError(w, http.StatusInternalServerError, "服务器错误")
			return
		}
		// 只有 OpenAI 兼容端点才提供 /embeddings（其余协议地址形态不同）。
		if p.APIType != llm.APITypeOpenAIChat {
			writeError(w, http.StatusBadRequest, "嵌入模型仅支持 OpenAI Chat 协议的供应商")
			return
		}
	}
	if err := a.db.SaveEmbeddingConfig(r.Context(), req.ProviderID, req.Model); err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusOK, req)
}
