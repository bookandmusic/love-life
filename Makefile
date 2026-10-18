# 命令统一入口，按功能分组：本地开发（浏览器为主）/ 本地测试 / 构建（二进制 / 镜像 / APK）。
# 日常开发后端 go run 直跑，不涉及构建产物；目标速查与口径在 DEVELOPMENT「命令速查」。
# 变量均可被命令行覆盖，如：make verify-server GOFLAGS=-count=1 / make up VERSION=0.2.0

ADMIN_DIR   := admin
SERVER_DIR  := server
WEBUI_DIST  := server/internal/webui/dist
APP_DIR     := app
BIN         := bin
SERVER_BIN  := $(BIN)/server
COMPOSE     := docker compose

# 本机开发默认凭据（弱口令，仅限本机；README 快速开始由部署者自设）
ADMIN_USER  ?= admin
ADMIN_PASS  ?= admin123456
# 前端 dev server 代理目标：默认连真实后端；连 mock 时 make app-dev API_PROXY=http://127.0.0.1:8787
API_PROXY   ?= http://127.0.0.1:8080
# 本机部署的镜像版本（透传 compose 的 LOVE_LIFE_VERSION；tag 集合见 README「快速开始」）
VERSION     ?= 0.1.0
# 本地签名密钥目录（与 scripts/gen-android-keystore.sh 默认落点一致）
KEYSTORE_DIR ?= $(HOME)/.love-life-keys
# go 环境**不设默认值**：GOPROXY / GOMODCACHE / GOCACHE 一律由 go env 与用户环境决定
# （`go env -w` 或 shell 导出都算）。受限环境（默认 GOCACHE 不可写等）按需临时覆盖：
#   make verify-server GOCACHE=$PWD/.gocache GOMODCACHE=/tmp/love-life-gomodcache
# 或一次性 `go env -w GOCACHE=…`——别把环境特例固化成仓库默认，见 DEVELOPMENT「二进制」。
GO_ENV       =
ifneq ($(strip $(GOPROXY)),)
GO_ENV      += GOPROXY=$(GOPROXY)
endif
ifneq ($(strip $(GOMODCACHE)),)
GO_ENV      += GOMODCACHE=$(GOMODCACHE)
endif
ifneq ($(strip $(GOCACHE)),)
GO_ENV      += GOCACHE=$(GOCACHE)
endif

.PHONY: deps mock reset serve admin-dev app-dev \
        verify verify-server verify-app verify-admin \
        build build-admin copy-admin build-server clean compose-build up down logs \
        android-keystore android-sign-env android-apk app-icon

# ============================================================
# 本地开发（浏览器为主）：装依赖、起 mock、三端 dev server、数据重置
# ============================================================

deps:
	cd $(SERVER_DIR) && $(GO_ENV) go mod download
	cd $(ADMIN_DIR) && npm install
	cd $(APP_DIR) && pnpm install

mock:   # 离线开发辅助（:8787，MOCK_PORT 可改）
	cd $(ADMIN_DIR) && npm run mock

reset:   # 清空 server/data；交互确认，输 n 不动数据
	@printf '将清空 server/data（admin / token / 成员 / 配置全部丢失），确认？[y/N] '; \
	read a; case "$$a" in [yY]) ;; *) echo 已取消; exit 1;; esac; \
	rm -rf server/data && mkdir -p server/data

serve:   # 后端 go run 直跑：改了代码即生效，无需先构建（日常开发路径；发布产物见「构建」组）
	cd $(SERVER_DIR) && $(GO_ENV) DATA_DIR=$(CURDIR)/server/data ADMIN_USER=$(ADMIN_USER) ADMIN_PASS=$(ADMIN_PASS) go run ./cmd/server

admin-dev:   # 后台管理页 dev server（:5173）
	cd $(ADMIN_DIR) && API_PROXY=$(API_PROXY) npm run dev

app-dev:   # APP 前端 dev server（:1420）；连 mock 时 make app-dev API_PROXY=http://127.0.0.1:8787
	cd $(APP_DIR) && API_PROXY=$(API_PROXY) pnpm dev

# ============================================================
# 本地测试：三端自动化验证（口径见 AGENTS「验证命令」）
# ============================================================

verify: verify-server verify-app verify-admin   # fail-fast；make -k verify 跑完全部再报

verify-server:
	cd $(SERVER_DIR) && $(GO_ENV) go build ./... && $(GO_ENV) go test ./...

verify-app:
	cd $(APP_DIR) && npx vue-tsc --noEmit && npx eslint src --max-warnings=0 && npm test

verify-admin:
	cd $(ADMIN_DIR) && npx vue-tsc --noEmit

# ============================================================
# 构建（二进制 / 镜像 / APK）：三个发布产物，日常开发不碰
# ============================================================

# ---- 二进制 ----

build: build-admin build-server   # copy-admin 由 build-server 依赖带入

build-admin:
	cd $(ADMIN_DIR) && npm ci && npm run build

# 将 admin/dist 拷入 server 内嵌目录；保留 .gitkeep 占位，清除其余历史产物以免旧资源残留。
# 目标目录不存在时先建：.dockerignore 排除了 webui/dist，镜像构建时该目录不存在，find 会因起点缺失而失败。
copy-admin:
	@mkdir -p $(WEBUI_DIST)
	@find $(WEBUI_DIST) -mindepth 1 ! -name .gitkeep -delete
	@cp -R $(ADMIN_DIR)/dist/. $(WEBUI_DIST)/

# 依赖 copy-admin：webui/dist 被 go:embed 内嵌，不先刷新就会把上一次构建的后台页面打进二进制
# （历史上出现过「源码已删的按钮仍在单二进制里」）。make serve 走裸 go run，不在此列。
build-server: copy-admin   # 发布产物：静态 linux 二进制（内嵌后台页面；modernc sqlite 纯 Go 无需 CGO）
	mkdir -p $(BIN)
	cd $(SERVER_DIR) && $(GO_ENV) CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o ../$(SERVER_BIN) ./cmd/server

clean:
	@rm -rf $(ADMIN_DIR)/dist $(BIN)
	@find $(WEBUI_DIST) -mindepth 1 ! -name .gitkeep -delete

# ---- 镜像 ----

compose-build:   # 经 compose 构建本地镜像（Dockerfile 内部调 make，与本地同一构建链）；随后 make up 直接用本地产物
	LOVE_LIFE_VERSION=$(VERSION) $(COMPOSE) build

up:   # 启动（本地无镜像则拉远端；版本用 make up VERSION=<x.y.z>）
	LOVE_LIFE_VERSION=$(VERSION) $(COMPOSE) up -d

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f

# ---- APK ----

android-keystore:   # 一次性：生成签名密钥并把三项 CI secret 写入本地 .env
	sh scripts/gen-android-keystore.sh

android-sign-env:   # 生成本地签名配置 keystore.properties（供 android-apk 出签名包）；已有密钥则复用，不覆盖
	@set -e; \
	test -f .env || { echo "缺少 .env——先 make android-keystore"; exit 1; }; \
	set -a; . ./.env; set +a; \
	mkdir -p $(KEYSTORE_DIR); \
	if [ -f "$(KEYSTORE_DIR)/upload-keystore.jks" ]; then \
	  echo "复用已有密钥 $(KEYSTORE_DIR)/upload-keystore.jks"; \
	else \
	  printf '%s' "$$ANDROID_KEYSTORE_BASE64" | base64 -d > $(KEYSTORE_DIR)/upload-keystore.jks; \
	  echo "已从 .env 解出密钥到 $(KEYSTORE_DIR)/upload-keystore.jks"; \
	fi; \
	{ printf 'keyAlias=%s\n' "$$ANDROID_KEY_ALIAS"; \
	  printf 'password=%s\n' "$$ANDROID_KEY_PASSWORD"; \
	  printf 'storeFile=%s\n' "$(KEYSTORE_DIR)/upload-keystore.jks"; } \
	  > $(APP_DIR)/src-tauri/gen/android/keystore.properties; \
	echo "已写 $(APP_DIR)/src-tauri/gen/android/keystore.properties"

android-apk:   # 本地 release 包（APK + AAB）；签名需先 make android-sign-env，产物在 gen/android/app/build/outputs
	cd $(APP_DIR) && pnpm tauri android build

app-icon:   # 源图 app/src-tauri/icons/icon.png（512×512）；替换后重生成全套图标
	cd $(APP_DIR) && pnpm tauri icon src-tauri/icons/icon.png
