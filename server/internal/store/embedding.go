package store

import (
	"context"
	"errors"
	"strconv"
)

// 嵌入模型配置（语义检索用，DESIGN §10.3）：复用某个既有模型供应商的地址与 Key 调
// OpenAI 兼容 /embeddings。未配置（ProviderID=0 或 Model 为空）→ 语义检索关闭，
// 检索回落关键词，故允许用零值清空。
const (
	settingEmbeddingProviderID = "embedding_provider_id"
	settingEmbeddingModel      = "embedding_model"
)

// EmbeddingConfig 嵌入模型配置。
type EmbeddingConfig struct {
	ProviderID int64
	Model      string
}

// GetEmbeddingConfig 读嵌入配置；未配置返回零值而非错误。
func (d *DB) GetEmbeddingConfig(ctx context.Context) (*EmbeddingConfig, error) {
	cfg := &EmbeddingConfig{}
	if v, err := d.GetSetting(ctx, settingEmbeddingProviderID); err == nil {
		if n, convErr := strconv.ParseInt(v, 10, 64); convErr == nil {
			cfg.ProviderID = n
		}
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if v, err := d.GetSetting(ctx, settingEmbeddingModel); err == nil {
		cfg.Model = v
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	return cfg, nil
}

// SaveEmbeddingConfig 原样写入嵌入配置两键；「providerID=0 视为清空」的判定由调用方负责。
func (d *DB) SaveEmbeddingConfig(ctx context.Context, providerID int64, model string) error {
	if err := d.SetSetting(ctx, settingEmbeddingProviderID, strconv.FormatInt(providerID, 10)); err != nil {
		return err
	}
	return d.SetSetting(ctx, settingEmbeddingModel, model)
}
