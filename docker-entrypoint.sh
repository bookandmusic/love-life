#!/bin/sh
# 容器入口：以 root 修正数据目录属主，随即降权到 app 运行。
#
# 为什么需要：bind mount（compose 的 ./data:/data）会**遮蔽**镜像内 /data 的属主，
# 容器内进程以 app(100:101) 运行时对宿主目录通常无写权限，SQLite 会以
# "unable to open database file (14)" 启动失败。这里启动时修正一次，
# 之后由 app 自己创建的文件天然属主正确，无需再改。
set -eu

DATA_DIR="${DATA_DIR:-/data}"
APP_UID_GID="app:app"

if [ "$(id -u)" = "0" ]; then
  mkdir -p "$DATA_DIR"
  # 仅在确不可写时递归修正，避免每次启动遍历海量媒体文件
  # ponytail: 顶层目录可写即跳过；若目录内出现混合属主（手工用 root 拷过数据），
  #           需部署者自行 chown -R 100:101 <数据目录>
  if ! su-exec "$APP_UID_GID" sh -c 'test -w "$1"' sh "$DATA_DIR"; then
    echo "entrypoint: 修正 $DATA_DIR 属主为 $APP_UID_GID" >&2
    chown -R "$APP_UID_GID" "$DATA_DIR"
  fi
  # 以 app 身份重新进入本脚本，进而 exec 真正的命令（默认 CMD 为 /app/server）
  exec su-exec "$APP_UID_GID" "$0" "$@"
fi

exec "$@"