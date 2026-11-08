package uglink

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// settingsKey 凭据在 app_settings 里的键名。
const settingsKey = "uglink_credentials"

// StoredCredentials 落库形态（密码加密后存储）。
type StoredCredentials struct {
	UglinkID    string `json:"uglink_id"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	PasswordEnc string `json:"password_enc"`
	Enabled     bool   `json:"enabled"`
	// Weight 通道权重（1–999，越大越优先，不得等于内置的 100）；旧配置缺省按 50 读（见 api 的 normalizeWeight）。
	// 与备用地址同一语义：APP 按权重降序逐个探活；内置地址恒 100 是量尺中点，可高于也可低于。
	Weight int `json:"weight"`
}

// Store 负责凭据的读写与加解密。
type Store struct {
	db  *store.DB
	key []byte
}

// NewStore 构造凭据存储；key 由服务端密钥派生（与模型密钥同源）。
func NewStore(db *store.DB, key []byte) *Store {
	return &Store{db: db, key: key}
}

// Delete 清除已存配置（回到未配置态）；未配置时静默成功。
func (s *Store) Delete(ctx context.Context) error {
	return s.db.DelSetting(ctx, settingsKey)
}

// Load 读取凭据（解密密码）；未配置时返回 nil, nil。
func (s *Store) Load(ctx context.Context) (*Credentials, error) {
	raw, err := s.db.GetSetting(ctx, settingsKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var stored StoredCredentials
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, err
	}
	if !stored.Enabled {
		return nil, nil // 停用等价于未配置
	}
	password, err := crypto.Decrypt(s.key, stored.PasswordEnc)
	if err != nil {
		return nil, err
	}
	return &Credentials{
		UglinkID: stored.UglinkID,
		Port:     stored.Port,
		Username: stored.Username,
		Password: password,
	}, nil
}

// LoadForDisplay 读取配置供后台展示（密码不回显，只给出是否已设置）。
func (s *Store) LoadForDisplay(ctx context.Context) (*StoredCredentials, bool, error) {
	raw, err := s.db.GetSetting(ctx, settingsKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var stored StoredCredentials
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, false, err
	}
	hasPassword := stored.PasswordEnc != ""
	stored.PasswordEnc = ""
	return &stored, hasPassword, nil
}

// Save 写入凭据；password 为空表示沿用旧密码。
func (s *Store) Save(ctx context.Context, in StoredCredentials, password string) error {
	if password == "" {
		// 未提供新密码：保留旧密文
		raw, err := s.db.GetSetting(ctx, settingsKey)
		if err == nil {
			var old StoredCredentials
			if json.Unmarshal([]byte(raw), &old) == nil {
				in.PasswordEnc = old.PasswordEnc
			}
		}
	} else {
		enc, err := crypto.Encrypt(s.key, password)
		if err != nil {
			return err
		}
		in.PasswordEnc = enc
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return s.db.SetSetting(ctx, settingsKey, string(raw))
}
