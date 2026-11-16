#!/usr/bin/env sh
# Build packages with internet access: collect the observability stack and Server binaries into the offline artifact repository (ADR-14, first section).
#
# Run only on a control machine with internet access; the site needs no internet, ansible pulls packages from the local Nginx repo.
# All versions are pinned, latest is forbidden; on completion, generate SHA256SUMS.txt for transfer verification.
# Usage: sh deploy/scripts/fetch-artifacts.sh
#   GHPROXY=https://ghproxy.net   GitHub proxy; leave empty (GHPROXY=) for a direct GitHub connection
#   SERVER_BIN=/path/to/binary    override the Server binary source (default server/bin/winter-server)
#
# Runs fastest on a machine with internet access and direct GitHub connectivity; set GHPROXY if the control machine needs a proxy.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
REPO="$ROOT/deploy/artifacts/repo"
TARBALL="$REPO/tarball"
BIN="$REPO/bin"
TMP="$ROOT/deploy/.fetch-tmp"

VM_VERSION=v1.156.0
VL_VERSION=v1.54.0
AM_TAG=v0.35.0            # the release tag includes the v
AM_VERSION=0.35.0         # asset filenames do not include the v (Prometheus' release convention differs from VM's)
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
        echo "Already exists, skipping: $(basename "$dest")"
        return 0
    fi
    echo "Downloading: $(basename "$dest")  <-  $url"
    curl -fsSL --retry 3 --retry-delay 2 --connect-timeout 20 --max-time 1800 \
        -o "$dest.partial" "$url"
    mv "$dest.partial" "$dest"
    echo "Done: $(basename "$dest")  $(wc -c < "$dest") bytes"
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
# Grafana 9.5.20 is the last stable release whose tar.gz is under GitHub's 100MB single-file limit;
# later releases (11.x and above, >170MB) must use physical media transfer or splitting, and cannot be committed directly.
fetch "https://dl.grafana.com/oss/release/$GF_PKG" "$TARBALL/$GF_PKG"

# vmalert is not released separately; it ships only with vmutils (~140MB). Extract the single binary and repackage it,
# avoiding bringing the full tool suite into the repo; the BOM will note that this is a repackaged artifact.
if [ ! -s "$TARBALL/$VMALERT_PKG" ]; then
    fetch "$GH/VictoriaMetrics/VictoriaMetrics/releases/download/$VM_VERSION/$VMUTILS_PKG" "$TMP/$VMUTILS_PKG"
    mkdir -p "$TMP/vmutils" "$TMP/vmalert"
    tar -xzf "$TMP/$VMUTILS_PKG" -C "$TMP/vmutils"
    vmbin=$(find "$TMP/vmutils" -type f -name 'vmalert-prod' | head -1)
    if [ -z "$vmbin" ]; then
        echo "vmalert-prod not found in vmutils; vmalert repackaging failed" >&2
        exit 1
    fi
    cp "$vmbin" "$TMP/vmalert/vmalert-prod"
    tar -czf "$TARBALL/$VMALERT_PKG" -C "$TMP/vmalert" vmalert-prod
    echo "vmalert repackaging complete: $VMALERT_PKG"
fi

# Server single binary: only copy the cross-compiled artifact; never compile on site (ADR-14)
if [ -s "$BIN/winter-server" ]; then
    echo "Server binary in place: $BIN/winter-server"
elif [ -s "$SERVER_BIN" ]; then
    cp "$SERVER_BIN" "$BIN/winter-server"
    echo "Server binary: $BIN/winter-server"
else
    echo "Warning: $SERVER_BIN not found; the winter-server artifact is missing" >&2
    echo "      First run GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o server/bin/winter-server ." >&2
fi

# Checksum manifest: on site after transfer, run sha256sum -c item by item (ADR-14, second section)
(cd "$REPO" && find bin tarball pip rpm -type f -print | sort | xargs sha256sum) > "$REPO/SHA256SUMS.txt"

echo "===== Artifact list ====="
ls -lh "$TARBALL" "$BIN"
echo "===== Checksum manifest ====="
cat "$REPO/SHA256SUMS.txt"

# Clean up downloaded intermediate artifacts (the full vmutils package is used only to extract vmalert)
if [ -d "$TMP" ]; then
    rm -r "$TMP"
fi
