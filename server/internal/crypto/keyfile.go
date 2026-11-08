package crypto

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// cryptoKeyFile 静态加密密钥材料的落盘文件名（数据目录内）。
const cryptoKeyFile = "crypto_key"

// LoadKey 读静态加密密钥材料；缺失时用 legacySeed 落盘一份再用。
//
// 密钥材料与 JWT 签名密钥分开存：轮换 `jwt_secret` 只影响已签发的令牌与 HMAC，
// 不会让已存的供应商 Key、绿联凭据、代理密码、地图 Key 解不开。
// legacySeed 只在首次升级时用于兼容已有密文——升级前后密钥材料相同，历史密文照旧可解；
// 此后两者各自独立，改 `jwt_secret` 不再牵动加密密钥。
func LoadKey(dataDir string, legacySeed []byte) ([]byte, error) {
	path := filepath.Join(dataDir, cryptoKeyFile)
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if len(b) < 32 {
			return nil, fmt.Errorf("%s 内容过短（%d 字节），无法作为加密密钥", cryptoKeyFile, len(b))
		}
		return b, nil
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	if len(legacySeed) < 32 {
		return nil, errors.New("缺少加密密钥材料：数据目录无 crypto_key，且没有可继承的旧密钥")
	}
	if err := os.WriteFile(path, legacySeed, 0o600); err != nil {
		return nil, err
	}
	return legacySeed, nil
}
