#!/usr/bin/env sh
# Build the canonical linux/amd64 ShadowDNS binary.
set -eu

: "${OUTPUT:?OUTPUT is required}"
VERSION=${VERSION:-dev}

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -trimpath \
    -ldflags="-s -w -buildid= -X main.version=${VERSION}" \
    -o "$OUTPUT" \
    ./cmd/shadowdns

if [ -n "${SOURCE_DATE_EPOCH:-}" ]; then
    # GNU/BusyBox touch accepts an epoch directly; BSD touch (macOS) does not
    # and needs an explicit [[CC]YY]MMDDhhmm[.SS] stamp, which BSD date derives
    # from the epoch with -r. Try the GNU form first, fall back to the BSD one.
    touch -d "@${SOURCE_DATE_EPOCH}" "$OUTPUT" 2>/dev/null ||
        touch -t "$(date -u -r "${SOURCE_DATE_EPOCH}" +%Y%m%d%H%M.%S)" "$OUTPUT"
fi
