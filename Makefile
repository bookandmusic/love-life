# 单静态二进制构建编排：admin 构建 → 拷入 server/internal/webui/dist/ → go build。
# webui/dist 内容由前端构建产物填充（除 .gitkeep 外不入版本库），故 build-server 绑死在 copy-admin 之后。

ADMIN_DIR   := admin
SERVER_DIR  := server
WEBUI_DIST  := server/internal/webui/dist
BIN         := bin
SERVER_BIN  := $(BIN)/server

.PHONY: build-admin copy-admin build-server build clean run android-keystore

build: build-admin copy-admin build-server

build-admin:
	cd $(ADMIN_DIR) && npm ci && npm run build

# 将 admin/dist 拷入 server 内嵌目录；保留 .gitkeep 占位，清除其余历史产物以免旧资源残留。
copy-admin:
	@find $(WEBUI_DIST) -mindepth 1 ! -name .gitkeep -delete
	@cp -R $(ADMIN_DIR)/dist/. $(WEBUI_DIST)/

build-server:
	mkdir -p $(BIN)
	cd $(SERVER_DIR) && go build -o ../$(SERVER_BIN) ./cmd/server

run: build
	./$(SERVER_BIN)

# 生成 Android 签名密钥（一次性）并把三项 CI secret 写入本地 .env
# 生成后需把 .env 中的值录入 GitHub 仓库 secrets，详见 README「Android APK 构建」
android-keystore:
	sh scripts/gen-android-keystore.sh

clean:
	@rm -rf $(ADMIN_DIR)/dist $(BIN)
	@find $(WEBUI_DIST) -mindepth 1 ! -name .gitkeep -delete
