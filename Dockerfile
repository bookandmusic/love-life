# 语法提示（可选；CI 用 buildkit 不依赖此指令）
# syntax=docker/dockerfile:1.7

# ===== Stage 1: 构建 admin 后台页面 =====
FROM node:22-alpine AS admin-build
WORKDIR /app/admin
# 先拷 lockfile，利用层缓存
COPY admin/package.json admin/package-lock.json ./
RUN npm ci
# 再拷源码构建
COPY admin/ ./
RUN npm run build

# ===== Stage 2: 构建 Go 服务端二进制（内嵌 admin 产物）=====
FROM golang:1.25-alpine AS go-build
WORKDIR /src
# Go 模块代理默认走官方源；网络受限（如国内）可覆盖：
#   docker build --build-arg GOPROXY=https://goproxy.cn,direct .
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
# 先拷 go.mod/go.sum，利用层缓存
COPY server/go.mod server/go.sum ./server/
RUN cd server && go mod download
# 拷服务端源码
COPY server/ ./server/
# 把 admin 构建产物拷入内嵌目录（占位 .gitkeep 不影响；此处用真实产物覆盖）
COPY --from=admin-build /app/admin/dist/ ./server/internal/webui/dist/
# modernc.org/sqlite 为纯 Go，可关闭 CGO
RUN cd server && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ===== Stage 3: 运行时 =====
FROM alpine:3.20
# ca-certificates：HTTPS 出站（模型 API / 地图瓦片 / webhook）
# tzdata：进程时区（DESIGN §12 「用户可见日期按部署者本地时区」）
# su-exec：入口脚本降权用（启动时以 root 修正数据目录属主，再切回 app）
RUN apk add --no-cache ca-certificates tzdata su-exec
RUN addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=go-build /out/server /app/server
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
