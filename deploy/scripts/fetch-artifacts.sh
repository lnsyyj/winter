#!/usr/bin/env sh
# 有网制包：把可观测性栈与 Server 二进制收进离线制品库（ADR-14 第一段）。
#
# 只在有网的控制机上执行；现场不需要外网，ansible 从本地 Nginx 源取包。
# 版本全部固定写死，禁止 latest；完成后生成 SHA256SUMS.txt 供搬运校验。
# 用法：sh deploy/scripts/fetch-artifacts.sh
#   GHPROXY=https://ghproxy.net   GitHub 代理；置空（GHPROXY=）则直连 GitHub
#   SERVER_BIN=/path/to/binary    覆盖 Server 二进制来源（默认 server/bin/winter-server）
#
# 放在有网且能直连 GitHub 的机器上跑最快；控制机若需走代理就设 GHPROXY。
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
REPO="$ROOT/deploy/artifacts/repo"
TARBALL="$REPO/tarball"
BIN="$REPO/bin"
TMP="$ROOT/deploy/.fetch-tmp"

VM_VERSION=v1.156.0
VL_VERSION=v1.54.0
AM_TAG=v0.35.0            # release tag 带 v
AM_VERSION=0.35.0         # 资产文件名不带 v（prometheus 的发布习惯与 VM 不同）
GF_VERSION=9.5.20
GHPROXY=${GHPROXY-https://ghproxy.net}
if [ -n "$GHPROXY" ]; then
    GH="$GHPROXY/https://github.com"
else
    GH="https://github.com"
fi
SERVER_BIN=${SERVER_BIN:-$ROOT/server/bin/winter-server}

mkdir -p "$TARBALL" "$BIN" "$TMP"

fetch() {
    url=$1
    dest=$2
    if [ -s "$dest" ]; then
        echo "已存在，跳过：$(basename "$dest")"
        return 0
    fi
    echo "下载：$(basename "$dest")  <-  $url"
    curl -fsSL --retry 3 --retry-delay 2 --connect-timeout 20 --max-time 1800 \
        -o "$dest.partial" "$url"
    mv "$dest.partial" "$dest"
    echo "完成：$(basename "$dest")  $(wc -c < "$dest") bytes"
}

VM_PKG="victoria-metrics-linux-amd64-$VM_VERSION.tar.gz"
VL_PKG="victoria-logs-linux-amd64-$VL_VERSION.tar.gz"
AM_PKG="alertmanager-$AM_VERSION.linux-amd64.tar.gz"
GF_PKG="grafana-$GF_VERSION.linux-amd64.tar.gz"
VMUTILS_PKG="vmutils-linux-amd64-$VM_VERSION.tar.gz"
VMALERT_PKG="vmalert-linux-amd64-$VM_VERSION.tar.gz"

fetch "$GH/VictoriaMetrics/VictoriaMetrics/releases/download/$VM_VERSION/$VM_PKG" "$TARBALL/$VM_PKG"
fetch "$GH/VictoriaMetrics/VictoriaLogs/releases/download/$VL_VERSION/$VL_PKG" "$TARBALL/$VL_PKG"
fetch "$GH/prometheus/alertmanager/releases/download/$AM_TAG/$AM_PKG" "$TARBALL/$AM_PKG"
# Grafana 9.5.20 是最后一个 tar.gz 小于 GitHub 单文件 100MB 上限的稳定版；
# 更高版本（11.x 起 >170MB）必须走搬运介质或分片，不能直接入库。
fetch "https://dl.grafana.com/oss/release/$GF_PKG" "$TARBALL/$GF_PKG"

# vmalert 不单独发布，只随 vmutils（约 140MB）提供。提取单个二进制重打包，
# 避免把全量工具包带进仓库；BOM 中会注明这是二次打包产物。
if [ ! -s "$TARBALL/$VMALERT_PKG" ]; then
    fetch "$GH/VictoriaMetrics/VictoriaMetrics/releases/download/$VM_VERSION/$VMUTILS_PKG" "$TMP/$VMUTILS_PKG"
    mkdir -p "$TMP/vmutils" "$TMP/vmalert"
    tar -xzf "$TMP/$VMUTILS_PKG" -C "$TMP/vmutils"
    vmbin=$(find "$TMP/vmutils" -type f -name 'vmalert-prod' | head -1)
    if [ -z "$vmbin" ]; then
        echo "vmutils 中未找到 vmalert-prod，vmalert 重打包失败" >&2
        exit 1
    fi
    cp "$vmbin" "$TMP/vmalert/vmalert-prod"
    tar -czf "$TARBALL/$VMALERT_PKG" -C "$TMP/vmalert" vmalert-prod
    echo "vmalert 二次打包完成：$VMALERT_PKG"
fi

# Server 单二进制：只复制交叉编译产物，现场绝不编译（ADR-14）
if [ -s "$BIN/winter-server" ]; then
    echo "Server 二进制已就位：$BIN/winter-server"
elif [ -s "$SERVER_BIN" ]; then
    cp "$SERVER_BIN" "$BIN/winter-server"
    echo "Server 二进制：$BIN/winter-server"
else
    echo "警告：未找到 $SERVER_BIN，winter-server 制品缺失" >&2
    echo "      先执行 GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o server/bin/winter-server ." >&2
fi

# 校验清单：现场搬运后逐项 sha256sum -c（ADR-14 第二段）
(cd "$REPO" && find bin tarball pip rpm -type f -print | sort | xargs sha256sum) > "$REPO/SHA256SUMS.txt"

echo "===== 制品清单 ====="
ls -lh "$TARBALL" "$BIN"
echo "===== 校验清单 ====="
cat "$REPO/SHA256SUMS.txt"

# 清理下载的中间产物（vmutils 全量包只用于提取 vmalert）
if [ -d "$TMP" ]; then
    rm -r "$TMP"
fi
