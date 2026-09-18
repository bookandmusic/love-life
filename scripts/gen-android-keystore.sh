#!/bin/sh
# 生成 Android 签名密钥（upload keystore），并把 CI 需要的三项秘密写入本地 .env。
#
# 用途：APK 的 release 构建必须签名。密钥只需生成一次并长期复用——
# Android 要求「更新已安装的 App」使用同一把密钥，换了密钥旧包无法覆盖安装。
#
# 用法：
#   sh scripts/gen-android-keystore.sh                    # 交互输入密码
#   KEYSTORE_PASS=xxxxxx sh scripts/gen-android-keystore.sh   # 非交互（CI/自动化）
#
# 产物：
#   $HOME/.love-life-keys/upload-keystore.jks   签名密钥（默认放在仓库之外，避免误提交）
#   .env                                        三项 GitHub Secrets 值（.gitignore 已忽略）
#
# 生成后把 .env 中的三项录入 GitHub 仓库 Settings → Secrets and variables → Actions。
set -eu

REPO_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
KEYSTORE_DIR="${KEYSTORE_DIR:-$HOME/.love-life-keys}"
KEYSTORE="${KEYSTORE:-$KEYSTORE_DIR/upload-keystore.jks}"
ENV_FILE="${ENV_FILE:-$REPO_ROOT/.env}"
ALIAS="${KEY_ALIAS:-upload}"
VALIDITY=10000
CN="${KEY_CN:-love-life}"

die() { echo "错误：$*" >&2; exit 1; }

# ---------- 1. 定位 keytool（随 JDK 提供） ----------
if [ -n "${JAVA_HOME:-}" ] && [ -x "$JAVA_HOME/bin/keytool" ]; then
  KEYTOOL="$JAVA_HOME/bin/keytool"
elif command -v keytool >/dev/null 2>&1; then
  KEYTOOL=$(command -v keytool)
else
  die "找不到 keytool。请安装 JDK（如 Temurin 17）并确保在 PATH 中，或设置 JAVA_HOME。"
fi

# ---------- 2. 已有密钥则拒绝覆盖 ----------
# 覆盖会导致已安装的旧 APK 无法升级（签名不匹配），且 Play 上架后更是不可更换。
if [ -e "$KEYSTORE" ]; then
  die "$KEYSTORE 已存在，拒绝覆盖。
  换密钥会使已安装的 App 无法覆盖安装/更新；若确实要重建，请先自行备份并删除该文件。
  只是想把现有密钥重新写入 .env，可直接手工编辑 .env 或删除 .env 中相关行后重跑本脚本。"
fi

# ---------- 3. 取密码（不回显；也支持通过环境变量提供） ----------
password=""
if [ -n "${KEYSTORE_PASS:-}" ]; then
  password="$KEYSTORE_PASS"
else
  [ -t 0 ] || die "非交互环境请通过 KEYSTORE_PASS=<密码> 提供"
  printf '设置密钥库密码（至少 6 位）：' >&2
  stty -echo 2>/dev/null || true
  IFS= read -r p1
  stty echo 2>/dev/null || true
  printf '\n再次输入确认：' >&2
  stty -echo 2>/dev/null || true
  IFS= read -r p2
  stty echo 2>/dev/null || true
  printf '\n' >&2
  [ "$p1" = "$p2" ] || die "两次输入不一致"
  password="$p1"
fi
[ ${#password} -ge 6 ] || die "密码至少 6 位（keytool 要求）"

# ---------- 4. 生成密钥 ----------
# 用 -storepass:env 而非明文参数，避免密码出现在进程列表（ps）里。
# PKCS12 密钥库的 store 密码与 key 密码强制相同，故只需一个密码。
mkdir -p "$KEYSTORE_DIR"
chmod 700 "$KEYSTORE_DIR"
echo "生成密钥库：$KEYSTORE（别名 $ALIAS，有效期 $VALIDITY 天）"
KEYSTORE_PASS="$password" "$KEYTOOL" -genkeypair \
  -keystore "$KEYSTORE" \
  -storetype PKCS12 \
  -alias "$ALIAS" \
  -keyalg RSA -keysize 2048 -validity "$VALIDITY" \
  -storepass:env KEYSTORE_PASS \
  -dname "CN=$CN, O=$CN, C=CN" >/dev/null
chmod 600 "$KEYSTORE"

echo "已生成。证书指纹："
KEYSTORE_PASS="$password" "$KEYTOOL" -list -v -keystore "$KEYSTORE" \
  -storepass:env KEYSTORE_PASS 2>/dev/null | grep -E "SHA256:|Alias name:" | sed 's/^/  /'

# ---------- 5. base64（跨 GNU/BSD） ----------
if base64 --help 2>&1 | grep -q -- "-w"; then
  KEYSTORE_B64=$(base64 -w0 <"$KEYSTORE")
else
  KEYSTORE_B64=$(base64 <"$KEYSTORE" | tr -d '\n')
fi

# ---------- 6. 写入 .env（保留其它条目，仅替换这三项） ----------
tmp="$ENV_FILE.tmp.$$"
{
  if [ -f "$ENV_FILE" ]; then
    grep -v -E '^ANDROID_(KEYSTORE_BASE64|KEY_ALIAS|KEY_PASSWORD)=' "$ENV_FILE" || true
  fi
  cat <<EOF
# ---- Android 签名密钥（由 scripts/gen-android-keystore.sh 生成，勿提交）----
# 录入位置：GitHub 仓库 Settings → Secrets and variables → Actions
# 密钥库文件：$KEYSTORE
ANDROID_KEYSTORE_BASE64=$KEYSTORE_B64
ANDROID_KEY_ALIAS=$ALIAS
ANDROID_KEY_PASSWORD=$password
EOF
} >"$tmp"
mv "$tmp" "$ENV_FILE"
chmod 600 "$ENV_FILE"

cat <<EOF

完成。
  密钥库   $KEYSTORE
  秘密文件 $ENV_FILE（权限 600，已被 .gitignore 忽略）

接下来把三项秘密录入 GitHub（二选一）：

  A. 手工：仓库 → Settings → Secrets and variables → Actions → New repository secret
     名称分别为 ANDROID_KEYSTORE_BASE64 / ANDROID_KEY_ALIAS / ANDROID_KEY_PASSWORD，
     值取 $ENV_FILE 中对应行等号右侧。

  B. 用 gh 一键录入：
     while IFS='=' read -r k v; do case "\$k" in ANDROID_*) gh secret set "\$k" --body "\$v";; esac; done < "$ENV_FILE"

务必离线备份密钥库文件！丢失后无法再为已安装的 App 发布更新。
EOF