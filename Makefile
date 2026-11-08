# 命令统一入口，按功能分组：本地开发（浏览器为主）/ 本地测试 / 构建（二进制 / 镜像 / APK）。
# 日常开发后端 go run 直跑，不涉及构建产物；目标按功能分组。
# 变量均可被命令行覆盖，如：make verify-server GOFLAGS=-count=1 / make serve DATA_DIR=$PWD/server/data-dev

ADMIN_DIR   := admin
SERVER_DIR  := server
WEBUI_DIST  := server/internal/webui/dist
APP_DIR     := app
BIN         := bin
SERVER_BIN  := $(BIN)/server
COMPOSE     := docker compose

# 本机开发默认凭据：**只给 `make serve` 用**，不参与部署。
# `make up` 刻意不透传这两个值——否则会把弱口令塞进 compose 的必填变量，
# 覆盖部署者在 .env / shell 里设置的口令。部署缺失即由 compose 报错。
ADMIN_USER  ?= admin
ADMIN_PASS  ?= admin123456
# 数据目录（serve 用）：默认仓库内 server/data；换目录直接覆盖，如 make serve DATA_DIR=$PWD/server/data-prod
DATA_DIR    ?= $(CURDIR)/server/data
# 前端 dev server 代理目标：默认连本机后端（make serve）；换地址时 make admin-dev API_PROXY=http://<host>:8080
API_PROXY   ?= http://127.0.0.1:8080
# 目标平台（本机默认 linux；镜像多架构构建由 Dockerfile 传 GOARCH）
GOOS        ?= linux
GOARCH      ?=
# 本地签名密钥目录（与 scripts/gen-android-keystore.sh 默认落点一致）
KEYSTORE_DIR ?= $(HOME)/.love-life-keys
# go 环境**不设默认值**：GOPROXY / GOMODCACHE / GOCACHE 一律由 go env 与 shell 决定。
# 需要临时换目录时按次传入，例如：make verify-server GOCACHE=$PWD/.gocache
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

.PHONY: deps reset serve admin-dev \
        verify verify-docs verify-server verify-admin test-race \
        build build-admin ensure-admin-dist copy-admin build-server build-apk clean compose-build up down logs \
        android-keystore android-sign-env verify-app

# ============================================================
# 本地开发（浏览器为主）：装依赖、三端 dev server、数据重置
# ============================================================

deps:
	cd $(SERVER_DIR) && $(GO_ENV) go mod download
	cd $(ADMIN_DIR) && pnpm install

reset:   # 清空 $(DATA_DIR)；交互确认，输 n 不动数据
	@printf '将清空 %s（admin / token / 成员 / 配置全部丢失），确认？[y/N] ' '$(DATA_DIR)'; \
	read a; case "$$a" in [yY]) ;; *) echo 已取消; exit 1;; esac; \
	rm -rf '$(DATA_DIR)' && mkdir -p '$(DATA_DIR)'

serve: copy-admin   # 后端 go run 直跑：改了代码即生效，无需先构建（日常开发路径；发布产物见「构建」组）
	cd $(SERVER_DIR) && $(GO_ENV) DATA_DIR=$(DATA_DIR) ADMIN_USER=$(ADMIN_USER) ADMIN_PASS=$(ADMIN_PASS) go run ./cmd/server

admin-dev:   # 后台管理页 dev server（:5173）
	cd $(ADMIN_DIR) && API_PROXY=$(API_PROXY) pnpm run dev

# ============================================================
# 本地测试：三端自动化验证
# ============================================================

verify: verify-docs verify-server verify-admin   # fail-fast；make -k verify 跑完全部再报
# APP 检查独立于 verify：make verify-app（需本机 JDK 25 + Android SDK）

verify-docs:   # 文档/注释门禁：必需文档、无未收口小节、注释不引文档不写历史、路径:行号可解析
	node scripts/verify-docs.mjs

verify-server:
	cd $(SERVER_DIR) && $(GO_ENV) go build ./... && $(GO_ENV) go vet ./... && $(GO_ENV) go test ./...
	@unfmt=$$(cd $(SERVER_DIR) && gofmt -l ./cmd ./internal); \
		if [ -n "$$unfmt" ]; then \
			echo "gofmt 未通过，先 gofmt -w 再提交:"; echo "$$unfmt"; exit 1; \
		fi

verify-app:   # APP 检查（资源/清单/AAPT + 编译 + 两侧单测 + Lint，一条 gradle 链路）。**不出包、不需要地址**——出包只有 make build-apk
	cd $(APP_DIR) && ./gradlew \
		:app:processDebugResources :app:compileDebugKotlin \
		:app:testDebugUnitTest :core:designsystem:testDebugUnitTest \
		:app:lintDebug :core:designsystem:lintDebug

verify-admin:
	cd $(ADMIN_DIR) && pnpm run verify   # vue-tsc + eslint + 样式守卫（scripts/guard-styles.mjs）

# 竞态检测：不进 verify / CI。`-race` 需要 cgo，目标内显式开启，别指望环境默认。
test-race:
	cd $(SERVER_DIR) && $(GO_ENV) CGO_ENABLED=1 go test -race -timeout 30m ./internal/...

# ============================================================
# 构建（二进制 / 镜像 / APK）：三个发布产物，日常开发不碰
# ============================================================

# ---- 二进制 ----

build: build-admin build-server   # copy-admin 由 build-server 依赖带入

build-admin:
	cd $(ADMIN_DIR) && pnpm install --frozen-lockfile && pnpm run build

# 确保 admin/dist 是新鲜的：不存在、或比 admin 源码旧时先重建，否则直接放过。
# 为什么是条件式而不是无条件：Docker 的 go-build 阶段没有 Node，`admin/` 下只有从上一阶段
# 拷来的 dist/、没有 src/——条件判断会走到「不旧」分支跳过，因此无需为 Docker 另开旁路。
ensure-admin-dist:
	@stale=; \
	if [ ! -f $(ADMIN_DIR)/dist/index.html ]; then stale="dist 不存在"; \
	else newer=$$(find $(ADMIN_DIR)/src $(ADMIN_DIR)/index.html $(ADMIN_DIR)/package.json \
		-newer $(ADMIN_DIR)/dist/index.html 2>/dev/null | head -1); \
		[ -n "$$newer" ] && stale="源码比 dist 新（如 $$newer）"; fi; \
	if [ -n "$$stale" ]; then \
		echo "admin/dist 需要重建：$$stale"; \
		$(MAKE) build-admin; \
	fi

# 将 admin/dist 拷入 server 内嵌目录；保留 .gitkeep 占位，清除其余历史产物以免旧资源残留。
# 目标目录不存在时先建：.dockerignore 排除了 webui/dist，镜像构建时该目录不存在，find 会因起点缺失而失败。
copy-admin: ensure-admin-dist
	@mkdir -p $(WEBUI_DIST)
	@find $(WEBUI_DIST) -mindepth 1 ! -name .gitkeep -delete
	@cp -R $(ADMIN_DIR)/dist/. $(WEBUI_DIST)/

# 依赖 copy-admin：webui/dist 被 go:embed 内嵌，不先刷新就会把上一次构建的后台页面打进二进制
# copy-admin 又依赖 ensure-admin-dist，
# 故二进制里嵌的永远是当前源码构建出来的页面。
build-server: copy-admin   # 发布产物：静态 linux 二进制（内嵌后台页面；modernc sqlite 纯 Go 无需 CGO）
	mkdir -p $(BIN)
	cd $(SERVER_DIR) && $(GO_ENV) CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -trimpath -ldflags="-s -w" -o ../$(SERVER_BIN) ./cmd/server

clean:
	@rm -rf $(ADMIN_DIR)/dist $(BIN)
	@find $(WEBUI_DIST) -mindepth 1 ! -name .gitkeep -delete

# ---- 镜像 ----

compose-build:   # 经 compose 构建本地镜像（Dockerfile 内部调 make，与本地同一构建链）；随后 make up 直接用本地产物
	$(COMPOSE) build

up:   # 启动（本地无镜像时按 compose 的 build 构建；要拉远端先 docker compose pull）
	$(COMPOSE) up -d

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f

# ---- APK ----

android-keystore:   # 一次性：生成签名密钥并把三项 CI secret 写入本地 .env
	sh scripts/gen-android-keystore.sh

android-sign-env:   # 生成本地签名配置 keystore.properties；已有密钥则复用，不覆盖
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
	for f in $(APP_DIR)/keystore.properties; do \
	  { printf 'keyAlias=%s\n' "$$ANDROID_KEY_ALIAS"; \
	    printf 'password=%s\n' "$$ANDROID_KEY_PASSWORD"; \
	    printf 'storeFile=%s\n' "$(KEYSTORE_DIR)/upload-keystore.jks"; } > $$f; \
	  echo "已写 $$f"; \
	done

# APP 产物（地址在构建期注入，仓库内不写死）
APK_NATIVE_OUT ?= $(BIN)/apk
# 变体：debug（默认）/ release（另需 app/keystore.properties，见 make android-sign-env）
NATIVE_VARIANT ?= debug
ifeq ($(NATIVE_VARIANT),release)
NATIVE_GRADLE_TASK := :app:assembleRelease
NATIVE_APK_SRC     := $(APP_DIR)/app/build/outputs/apk/release/app-release.apk
else ifeq ($(NATIVE_VARIANT),debug)
NATIVE_GRADLE_TASK := :app:assembleDebug
NATIVE_APK_SRC     := $(APP_DIR)/app/build/outputs/apk/debug/app-debug.apk
else
$(error NATIVE_VARIANT 只能是 debug 或 release，当前为 $(NATIVE_VARIANT))
endif

build-apk:   # 原生 APK（必须给 APP_DEFAULT_BASE；NATIVE_VARIANT=release 出签名包；NATIVE_VERSION=x.y.z 显式版本号，发布包由 CI 从 tag 传入）
	@if [ -z "$(APP_DEFAULT_BASE)" ]; then \
		echo "缺少 APP_DEFAULT_BASE——请指定该包要连的服务地址，例如：" >&2; \
		echo "  APP_DEFAULT_BASE=http://192.168.1.10:8080 make build-apk" >&2; \
		echo "（地址在构建期注入）" >&2; \
		exit 1; \
	fi
	@if [ "$(NATIVE_VARIANT)" = "release" ] && [ ! -f $(APP_DIR)/keystore.properties ]; then \
		echo "release 包需要签名配置：先跑 make android-sign-env（生成 $(APP_DIR)/keystore.properties）" >&2; \
		exit 1; \
	fi
	@# NATIVE_VERSION：显式版本号。发布包由 CI 从 tag 传入；本地造「覆盖安装升级」场景时也用它，不动代码默认值
	cd $(APP_DIR) && ./gradlew $(NATIVE_GRADLE_TASK) $(if $(NATIVE_VERSION),-PAPP_VERSION_NAME=$(NATIVE_VERSION),)
	@mkdir -p $(APK_NATIVE_OUT)
	@cp $(NATIVE_APK_SRC) $(APK_NATIVE_OUT)/lovelife-native-$(NATIVE_VARIANT).apk
	@echo "APK: $(APK_NATIVE_OUT)/lovelife-native-$(NATIVE_VARIANT).apk（内置地址 $(APP_DEFAULT_BASE)，变体 $(NATIVE_VARIANT)）"

