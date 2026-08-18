#!/usr/bin/env bash
# Verify the published container configuration contract for a local image.
#
# Values are extracted with jq from a single `image inspect` round-trip rather
# than with Go templates. Runtimes disagree on the template type of the inspect
# result — podman exposes `.Config` as a struct while recent docker exposes it
# as a map, so `{{json .Config.Healthcheck}}` resolves to null on one and fails
# with "map has no entry for key" on the other. The marshalled JSON is identical
# on both, so extracting from it keeps this script runtime-agnostic.
set -euo pipefail

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
    echo "Usage: $0 <image> [expected-version]" >&2
    exit 2
fi

IMAGE=$1
EXPECTED_VERSION=${2:-dev}
EXPECTED_SOURCE=${EXPECTED_SOURCE:-https://example.org/shadowdns}
EXPECTED_REVISION=${EXPECTED_REVISION:-unknown}
EXPECTED_CREATED=${EXPECTED_CREATED:-1970-01-01T00:00:00Z}

if ! command -v jq >/dev/null 2>&1; then
    echo "Error: jq is required but not found in PATH" >&2
    exit 1
fi

if [ -z "${CONTAINER_RUNTIME:-}" ]; then
    CONTAINER_RUNTIME=$("$(dirname "$0")/container-runtime.sh")
fi
CTR=$CONTAINER_RUNTIME

assert_equal() {
    local label=$1
    local expected=$2
    local actual=$3

    if [ "$actual" != "$expected" ]; then
        printf 'FAIL: %s: expected %q, got %q\n' "$label" "$expected" "$actual" >&2
        exit 1
    fi
    printf 'OK: %s\n' "$label"
}

INSPECT_JSON=$("$CTR" image inspect "$IMAGE" --format '{{json .}}')

# Read one value out of the cached inspect result; no further daemon calls.
field() {
    printf '%s' "$INSPECT_JSON" | jq -r "$1"
}

assert_equal architecture amd64 "$(field '.Architecture')"
assert_equal user 65532:65532 "$(field '.Config.User')"
assert_equal entrypoint '["/usr/local/bin/shadowdns"]' "$(field '.Config.Entrypoint | tojson')"
assert_equal command '["--named-conf","/etc/shadowdns/named.conf","--config","/etc/shadowdns/shadowdns.yaml","--listen","0.0.0.0:5353","--no-color"]' "$(field '.Config.Cmd | tojson')"
assert_equal 5353/udp '{}' "$(field '.Config.ExposedPorts["5353/udp"] | tojson')"
assert_equal 5353/tcp '{}' "$(field '.Config.ExposedPorts["5353/tcp"] | tojson')"
assert_equal 9153/tcp '{}' "$(field '.Config.ExposedPorts["9153/tcp"] | tojson')"
# `keys` sorts, so the full set compares deterministically and an extra
# published port fails the check.
assert_equal exposed-ports 5353/tcp,5353/udp,9153/tcp "$(field '.Config.ExposedPorts | keys | join(",")')"
# Absent on docker, null on podman — jq normalises both to null.
assert_equal healthcheck null "$(field '.Config.Healthcheck | tojson')"
assert_equal source "$EXPECTED_SOURCE" "$(field '.Config.Labels["org.opencontainers.image.source"]')"
assert_equal revision "$EXPECTED_REVISION" "$(field '.Config.Labels["org.opencontainers.image.revision"]')"
assert_equal image-version "$EXPECTED_VERSION" "$(field '.Config.Labels["org.opencontainers.image.version"]')"
assert_equal created "$EXPECTED_CREATED" "$(field '.Config.Labels["org.opencontainers.image.created"]')"
assert_equal version "$EXPECTED_VERSION" "$("$CTR" run --rm "$IMAGE" --version)"
