#!/usr/bin/env bash
# test-deb.sh — end-to-end .deb package validation in an Ubuntu container.
#
# Cross-compiles the binary for linux/amd64, builds a .deb, starts an Ubuntu
# container via podman/docker, installs the package, and verifies file layout, user
# creation, dry-run, and live DNS queries.
#
# Usage:
#   ./scripts/test-deb.sh
#
# Requirements: Go toolchain, podman or docker, nfpm (go tool).
set -euo pipefail

# Auto-detect container runtime: prefer podman, fall back to docker.
if command -v podman >/dev/null 2>&1; then
    CTR=podman
elif command -v docker >/dev/null 2>&1; then
    CTR=docker
else
    echo "Error: neither podman nor docker found in PATH" >&2
    exit 1
fi
echo "Using container runtime: $CTR"

CONTAINER_NAME="shadowdns-deb-test-$$"
PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TESTDATA_DIR="$PROJECT_ROOT/.local/container-testdata"
CONTAINER_TESTDATA="/etc/shadowdns"
IMAGE="docker.io/library/ubuntu:24.04"
PLATFORM="linux/amd64"
DEB_NAME="shadowdns_test_amd64.deb"

cleanup() {
    echo "--- Cleanup ---"
    $CTR rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
    rm -f "$PROJECT_ROOT/$DEB_NAME"
    rm -f "$PROJECT_ROOT/bin/shadowdns-linux-amd64"
    rm -rf "$TESTDATA_DIR"
}
trap cleanup EXIT

cd "$PROJECT_ROOT"

# -------------------------------------------------------------------
# Step 1: Cross-compile for linux/amd64 and build .deb
# -------------------------------------------------------------------
echo "--- Cross-compile + build test .deb ---"
make build-linux
go tool nfpm package --packager deb --target "$DEB_NAME"

# -------------------------------------------------------------------
# Step 3: Generate testdata with mock GeoIP
# -------------------------------------------------------------------
echo "--- Generate testdata ---"
rm -rf "$TESTDATA_DIR"
go run scripts/gen-container-testdata.go -out "$TESTDATA_DIR" -target "$CONTAINER_TESTDATA"

# -------------------------------------------------------------------
# Step 4: Start container
# -------------------------------------------------------------------
echo "--- Start container ($PLATFORM) ---"
$CTR run --platform "$PLATFORM" -d --name "$CONTAINER_NAME" \
    -v "$PROJECT_ROOT/$DEB_NAME:/tmp/shadowdns.deb:ro" \
    -v "$TESTDATA_DIR:/tmp/testdata:ro" \
    "$IMAGE" sleep infinity

# -------------------------------------------------------------------
# Step 5: Install .deb
# -------------------------------------------------------------------
echo "--- Install .deb ---"
$CTR exec "$CONTAINER_NAME" dpkg -i /tmp/shadowdns.deb

# -------------------------------------------------------------------
# Step 6: Verify file layout and user
# -------------------------------------------------------------------
echo "--- Verify installation ---"
$CTR exec "$CONTAINER_NAME" test -x /usr/bin/shadowdns
echo "  [OK] /usr/bin/shadowdns exists and is executable"

$CTR exec "$CONTAINER_NAME" test -f /lib/systemd/system/shadowdns.service
echo "  [OK] /lib/systemd/system/shadowdns.service exists"

$CTR exec "$CONTAINER_NAME" test -f /etc/shadowdns/named.conf.example
echo "  [OK] /etc/shadowdns/named.conf.example exists"

$CTR exec "$CONTAINER_NAME" test -f /etc/shadowdns/aliases.yaml.example
echo "  [OK] /etc/shadowdns/aliases.yaml.example exists"

$CTR exec "$CONTAINER_NAME" getent passwd shadowdns >/dev/null
echo "  [OK] shadowdns user exists"

$CTR exec "$CONTAINER_NAME" getent group shadowdns >/dev/null
echo "  [OK] shadowdns group exists"

$CTR exec "$CONTAINER_NAME" test -d /var/log/shadowdns
echo "  [OK] /var/log/shadowdns/ directory exists"

OWNER=$($CTR exec "$CONTAINER_NAME" stat -c '%U' /var/log/shadowdns)
if [ "$OWNER" = "shadowdns" ]; then
    echo "  [OK] /var/log/shadowdns/ owned by shadowdns"
else
    echo "  [FAIL] /var/log/shadowdns/ owned by '$OWNER', expected 'shadowdns'"
    exit 1
fi

# -------------------------------------------------------------------
# Step 7: Copy testdata and run -dry-run
# -------------------------------------------------------------------
echo "--- Dry-run test ---"
$CTR exec "$CONTAINER_NAME" sh -c '
    cp /tmp/testdata/named.conf /etc/shadowdns/named.conf &&
    cp /tmp/testdata/aliases.yaml /etc/shadowdns/aliases.yaml &&
    cp /tmp/testdata/master.zones /etc/shadowdns/master.zones &&
    cp -r /tmp/testdata/master /etc/shadowdns/master &&
    cp -r /tmp/testdata/geoip /etc/shadowdns/geoip
'
$CTR exec "$CONTAINER_NAME" shadowdns \
    --named-conf /etc/shadowdns/named.conf \
    --aliases /etc/shadowdns/aliases.yaml \
    --dry-run
echo "  [OK] --dry-run exited successfully"

# -------------------------------------------------------------------
# Step 8: Start server and query DNS
# -------------------------------------------------------------------
echo "--- DNS query test ---"
$CTR exec "$CONTAINER_NAME" apt-get update -qq >/dev/null 2>&1
$CTR exec "$CONTAINER_NAME" apt-get install -y -qq dnsutils >/dev/null 2>&1

$CTR exec -d "$CONTAINER_NAME" shadowdns \
    --named-conf /etc/shadowdns/named.conf \
    --aliases /etc/shadowdns/aliases.yaml \
    --listen 127.0.0.1:1053

# Wait for the server to start accepting queries.
sleep 2

ANSWER=$($CTR exec "$CONTAINER_NAME" dig @127.0.0.1 -p 1053 example.com A +short 2>&1)
if [ -n "$ANSWER" ]; then
    echo "  [OK] example.com A → $ANSWER"
else
    echo "  [FAIL] empty response for example.com A"
    exit 1
fi

ALIAS_ANSWER=$($CTR exec "$CONTAINER_NAME" dig @127.0.0.1 -p 1053 backup.example A +short 2>&1)
if [ -n "$ALIAS_ANSWER" ]; then
    echo "  [OK] backup.example A (alias) → $ALIAS_ANSWER"
else
    echo "  [FAIL] empty response for backup.example A"
    exit 1
fi

echo ""
echo "=== All tests passed ==="
