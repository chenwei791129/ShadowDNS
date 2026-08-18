#!/usr/bin/env bash
# test-container.sh — end-to-end runtime validation of the ShadowDNS container image.
#
# scripts/verify-container-image.sh only asserts the image's static
# configuration contract. This script complements it by starting the image the
# way a deployment does — non-root, read-only config mount, published high
# ports — and exercising the runtime behaviour that contract implies: UDP and
# TCP authoritative answers, the metrics endpoint, stderr-only logging, native
# SIGHUP reload, and bounded SIGTERM graceful shutdown.
#
# Usage:
#   make container-image        # build the image first
#   ./scripts/test-container.sh [image]
#
# Requirements: Go toolchain, podman or docker, dig, curl, and an image that
# has already been built (defaults to the local shadowdns:dev tag).
set -euo pipefail

IMAGE=${1:-shadowdns:dev}
PLATFORM="linux/amd64"
# Where the read-only config mount lands inside the container — must match the
# --named-conf/--config paths in the image's default CMD.
CONTAINER_CONFIG_DIR="/etc/shadowdns"
DNS_PORT=${DNS_PORT:-11053}
METRICS_PORT=${METRICS_PORT:-19153}

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TESTDATA_DIR="$PROJECT_ROOT/.local/container-image-testdata"
CONTAINER_NAME="shadowdns-image-test-$$"

CTR=$("$(dirname "$0")/container-runtime.sh")
echo "Using container runtime: $CTR"

for tool in dig curl; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        echo "Error: $tool is required but not found in PATH" >&2
        exit 1
    fi
done

# `dig +short` prints ";; communications error ..." to *stdout*, so a non-empty
# result is not by itself proof of an answer — drop comment lines.
dig_short() {
    dig "$@" +short 2>/dev/null | grep -v '^;' || true
}

cleanup() {
    echo "--- Cleanup ---"
    $CTR rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
    rm -rf "$TESTDATA_DIR"
}
trap cleanup EXIT

cd "$PROJECT_ROOT"

# -------------------------------------------------------------------
# Step 1: Generate the deployment-owned config directory
# -------------------------------------------------------------------
# Reuse the shared generator (scripts/test-deb.sh uses the same one) so the
# container path never grows a Docker-specific fixture format.
echo "--- Generate testdata ---"
rm -rf "$TESTDATA_DIR"
go run scripts/gen-container-testdata.go -out "$TESTDATA_DIR" -target "$CONTAINER_CONFIG_DIR"

# -------------------------------------------------------------------
# Step 2: Start the image with a read-only config mount
# -------------------------------------------------------------------
echo "--- Start container ($PLATFORM, read-only config mount, default command) ---"
$CTR run --platform "$PLATFORM" -d --name "$CONTAINER_NAME" \
    -p "127.0.0.1:$DNS_PORT:5353/udp" \
    -p "127.0.0.1:$DNS_PORT:5353/tcp" \
    -p "127.0.0.1:$METRICS_PORT:9153/tcp" \
    -v "$TESTDATA_DIR:$CONTAINER_CONFIG_DIR:ro" \
    "$IMAGE" >/dev/null

# Wait for the server to answer, surfacing container logs if it never does.
UDP_ANSWER=""
for _ in $(seq 1 30); do
    UDP_ANSWER=$(dig_short @127.0.0.1 -p "$DNS_PORT" example.com A +time=1 +tries=1)
    [ -n "$UDP_ANSWER" ] && break
    sleep 1
done
if [ -z "$UDP_ANSWER" ]; then
    echo "  [FAIL] container never answered example.com A over UDP"
    $CTR logs "$CONTAINER_NAME" 2>&1 | tail -30
    exit 1
fi
echo "  [OK] UDP example.com A -> $UDP_ANSWER"

# -------------------------------------------------------------------
# Step 3: Query paths — UDP, TCP, and alias resolution
# -------------------------------------------------------------------
echo "--- DNS query test ---"
TCP_ANSWER=$(dig_short @127.0.0.1 -p "$DNS_PORT" example.com A +tcp)
if [ -z "$TCP_ANSWER" ]; then
    echo "  [FAIL] empty response for example.com A over TCP"
    exit 1
fi
echo "  [OK] TCP example.com A -> $TCP_ANSWER"

ALIAS_ANSWER=$(dig_short @127.0.0.1 -p "$DNS_PORT" backup.example A)
if [ -z "$ALIAS_ANSWER" ]; then
    echo "  [FAIL] empty response for backup.example A (alias)"
    exit 1
fi
echo "  [OK] UDP backup.example A (alias) -> $ALIAS_ANSWER"

# -------------------------------------------------------------------
# Step 4: Metrics endpoint on the second published port
# -------------------------------------------------------------------
echo "--- Metrics test ---"
if ! BUILD_INFO=$(curl -fsS "http://127.0.0.1:$METRICS_PORT/metrics" | grep -m1 '^shadowdns_build_info'); then
    echo "  [FAIL] /metrics did not expose shadowdns_build_info"
    exit 1
fi
echo "  [OK] /metrics -> $BUILD_INFO"

# -------------------------------------------------------------------
# Step 5: Logs go to stderr, not stdout
# -------------------------------------------------------------------
echo "--- Logging test ---"
STDOUT_LOG=$($CTR logs "$CONTAINER_NAME" 2>/dev/null)
STDERR_LOG=$($CTR logs "$CONTAINER_NAME" 2>&1 1>/dev/null)
if ! printf '%s\n' "$STDERR_LOG" | grep -q 'shadowdns ready'; then
    echo "  [FAIL] startup log not found on container stderr"
    exit 1
fi
echo "  [OK] startup log written to stderr"
if [ -n "$STDOUT_LOG" ]; then
    echo "  [FAIL] container stdout is not empty: $STDOUT_LOG"
    exit 1
fi
echo "  [OK] container stdout is empty"

# -------------------------------------------------------------------
# Step 6: SIGHUP reaches PID 1 and triggers the native reload path
# -------------------------------------------------------------------
echo "--- SIGHUP reload test ---"
$CTR kill --signal HUP "$CONTAINER_NAME" >/dev/null
RELOADED=""
for _ in $(seq 1 40); do
    if $CTR logs "$CONTAINER_NAME" 2>&1 | grep -q 'reload complete'; then
        RELOADED=$($CTR logs "$CONTAINER_NAME" 2>&1 | grep 'reload complete' | tail -1)
        break
    fi
    sleep 0.25
done
if [ -z "$RELOADED" ]; then
    echo "  [FAIL] no 'reload complete' log after SIGHUP"
    exit 1
fi
echo "  [OK] SIGHUP reload -> $RELOADED"

# The container must still answer after the reload swap.
POST_RELOAD=$(dig_short @127.0.0.1 -p "$DNS_PORT" example.com A)
if [ -z "$POST_RELOAD" ]; then
    echo "  [FAIL] empty response for example.com A after reload"
    exit 1
fi
echo "  [OK] post-reload example.com A -> $POST_RELOAD"

# -------------------------------------------------------------------
# Step 7: SIGTERM stops gracefully within the runtime's stop timeout
# -------------------------------------------------------------------
echo "--- SIGTERM graceful shutdown test ---"
$CTR stop --time 10 "$CONTAINER_NAME" >/dev/null
STATE=$($CTR inspect "$CONTAINER_NAME" --format '{{.State.Status}} {{.State.ExitCode}}')
if [ "$STATE" != "exited 0" ]; then
    echo "  [FAIL] expected 'exited 0' after SIGTERM, got '$STATE'"
    exit 1
fi
echo "  [OK] SIGTERM -> graceful exit (status 0)"

echo ""
echo "=== All container runtime tests passed ==="
