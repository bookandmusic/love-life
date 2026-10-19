# 语法提示（可选；CI 用 buildkit 不依赖此指令）
# syntax=docker/dockerfile:1.7

# 构建链统一收在 Makefile：本文件的构建阶段只调 make 目标（build-admin / copy-admin build-server），
# 与本地 `make build` 同一条链——改构建参数只改 Makefile 一地。

# ===== Stage 1: 构建 admin 后台页面 =====
FROM node:22-alpine AS admin-build
RUN apk add --no-cache make
# 前端统一 pnpm：镜像自带 npm 但没有 pnpm，而 make build-admin 走的已是 pnpm。
# 版本口径与 CI 同源（package.json 的 packageManager 字段 + corepack）。
RUN corepack enable && corepack prepare pnpm@11 --activate
WORKDIR /app
COPY Makefile ./
COPY admin/ ./admin/
RUN make build-admin

# ===== Stage 2: 构建 Go 服务端二进制（内嵌 admin 产物）=====
FROM golang:1.25-alpine AS go-build
RUN apk add --no-cache make
# GOPROXY 经 build-arg 透传给 Makefile 的同名变量（GO_ENV 条件拼装）
ARG GOPROXY=https://proxy.golang.org,direct
WORKDIR /app
COPY Makefile ./
# 先下依赖，利用层缓存
COPY server/go.mod server/go.sum ./server/
RUN cd server && go mod download
# 拷服务端源码
COPY server/ ./server/
# 把 admin 构建产物作为 make copy-admin 的输入（占位 .gitkeep 不影响；此处用真实产物覆盖）
COPY --from=admin-build /app/admin/dist/ ./admin/dist/
RUN make copy-admin build-server

# ===== Stage 3: 运行时 =====
FROM alpine:3.20
# ca-certificates：HTTPS 出站（模型 API / 地图瓦片 / webhook）
# tzdata：进程时区（用户可见日期按部署者本地时区，ARCHITECTURE「数据模型」）
# su-exec：入口脚本降权用（启动时以 root 修正数据目录属主，再切回 app）
RUN apk add --no-cache ca-certificates tzdata su-exec
RUN addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=go-build /app/bin/server /app/server
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
# 数据目录由部署者通过卷挂载提供；此处仅建空目录作默认
# 注意：这里刻意不设 USER——入口脚本需以 root 修正挂载目录属主后再降权，
# 服务进程本身仍以非 root 的 app 用户运行（见 docker-entrypoint.sh）
RUN chmod 0755 /app/docker-entrypoint.sh && mkdir -p /data && chown -R app:app /app /data
EXPOSE 8080
ENV LISTEN=:8080 \
    DATA_DIR=/data \
    TZ=Asia/Shanghai
ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["/app/server"]
