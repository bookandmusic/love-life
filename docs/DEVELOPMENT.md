# 情侣生活记录 APP — 开发与构建指南

> 部署与使用见 [README.md](../README.md)；产品需求与技术设计见 [REQUIREMENTS.md](REQUIREMENTS.md) / [DESIGN.md](DESIGN.md)。

## 本地开发

```bash
# 后端（默认监听 :8080，数据目录 ./data）
cd server && go run ./cmd/server

# 后台管理页（开发代理 /api → :8080；npm run mock 可起零依赖 mock 服务）
cd admin && npm install && npm run dev

# APP（仓库用 pnpm，lockfile 为 pnpm-lock.yaml）
cd app && pnpm install && pnpm dev
cd app && pnpm tauri dev           # 需 Rust
cd app && pnpm tauri android dev   # 需 Rust + Android SDK
```

开发流程、编码约定与各端验证命令见 [AGENTS.md](../AGENTS.md)。

## 从源码构建

```bash
make build   # admin 构建 → 拷入 server/internal/webui/dist/ → go build，产物在 bin/server
make clean   # 清理构建产物

# 运行（首次启动需提供 admin 凭据）
ADMIN_USER=admin ADMIN_PASS=<密码> ./bin/server
```

> [!NOTE]
> `server/internal/webui/dist/` 不入版本库，内容由 `make build` 填充；裸 `go build` 得到的二进制不包含后台页面。

## Android APK 构建（CI）

APK 统一由 GitHub Actions 在推 `v*` tag 时构建并签名，本地不打包。

### 1. 生成签名密钥并录入仓库 secrets（一次性，长期复用）

```bash
make android-keystore
```

脚本会交互式询问密码，然后：把密钥库生成到 `~/.love-life-keys/upload-keystore.jks`（放在仓库之外避免误提交，可用 `KEYSTORE_DIR` 覆盖）；把三项 secret 写入仓库根目录 `.env`（已被 gitignore），并打印 `gh` 一键录入命令。在仓库 Settings → Secrets and variables → Actions 录入，名称须完全一致：

| Secret 名 | 取值 |
|:--|:--|
| `ANDROID_KEYSTORE_BASE64` | 密钥库文件的 base64（脚本已生成） |
| `ANDROID_KEY_ALIAS` | key 别名（脚本默认 `upload`） |
| `ANDROID_KEY_PASSWORD` | keystore 与 key 的密码（PKCS12 两者强制相同） |

> [!CAUTION]
> **换了密钥，已安装的旧 APK 无法覆盖安装 / 更新。**密钥库请离线备份；未录入 secrets 时 workflow 会在签名步骤直接失败。

### 2. 触发构建

```bash
git tag v0.1.0 && git push origin v0.1.0
```

> [!IMPORTANT]
> tag 版本必须与 `app/src-tauri/tauri.conf.json` 的 `version` 一致，否则 workflow 直接失败。Actions 完成后 GitHub Releases 出现一份草稿（通用 APK + AAB）。

产物为单个通用 APK（含 `arm64-v8a` / `armeabi-v7a` / `x86_64` / `x86` 四种 ABI），加上供 Play Store 上架用的 AAB；workflow artifact 保留 90 天副本。下载 APK 后在 Android 设备上开启「未知来源安装」即可旁加载。

### 3. 维护事项

- `app/src-tauri/gen/android/` 纳入版本管理（Tauri 官方约定，工程自带 `.gitignore`，构建产物与 `keystore.properties` 不会入库）。签名配置与 release 明文开关写在 `app/build.gradle.kts`；`keystore.properties` 由 CI 构建时写入，本地开发可自行创建（缺失时 release 用未签名/默认配置，debug 不受影响）。
- 本地与 CI 都必须经 `pnpm tauri ...` 调用（如 `pnpm tauri android build`）。用 `node_modules/.bin/tauri` 直调会让 CLI 把生成的 Gradle 任务写成 `executable="node"`，Gradle 阶段报 `Cannot find module '<路径>/tauri'`（CI 的 init 会自愈回 `pnpm`，本地需注意）。
- 更换图标：`cd app && pnpm tauri icon src-tauri/icons/icon.png`。会同时更新 `src-tauri/icons/` 与 Android `res/`；整套默认图标重新生成一遍，diff 较大属正常。
