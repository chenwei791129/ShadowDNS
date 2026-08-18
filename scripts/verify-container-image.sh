#!/usr/bin/env bash
# Verify the published container configuration contract for a local image.
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

IFS=$'\t' read -r architecture user entrypoint command port_udp port_tcp port_metrics ports healthcheck source revision image_version created < <(
    "$CTR" image inspect "$IMAGE" --format \
        '{{.Architecture}}\t{{.Config.User}}\t{{json .Config.Entrypoint}}\t{{json .Config.Cmd}}\t{{index .Config.ExposedPorts "5353/udp"}}\t{{index .Config.ExposedPorts "5353/tcp"}}\t{{index .Config.ExposedPorts "9153/tcp"}}\t{{range $port, $_ := .Config.ExposedPorts}}{{$port}},{{end}}\t{{json .Config.Healthcheck}}\t{{index .Config.Labels "org.opencontainers.image.source"}}\t{{index .Config.Labels "org.opencontainers.image.revision"}}\t{{index .Config.Labels "org.opencontainers.image.version"}}\t{{index .Config.Labels "org.opencontainers.image.created"}}'
)

assert_equal architecture amd64 "$architecture"
assert_equal user 65532:65532 "$user"
assert_equal entrypoint '["/usr/local/bin/shadowdns"]' "$entrypoint"
assert_equal command '["--named-conf","/etc/shadowdns/named.conf","--config","/etc/shadowdns/shadowdns.yaml","--listen","0.0.0.0:5353","--no-color"]' "$command"
assert_equal 5353/udp '{}' "$port_udp"
assert_equal 5353/tcp '{}' "$port_tcp"
assert_equal 9153/tcp '{}' "$port_metrics"
ports=${ports%,}
assert_equal exposed-ports 5353/tcp,5353/udp,9153/tcp "$ports"
assert_equal healthcheck null "$healthcheck"
assert_equal source "$EXPECTED_SOURCE" "$source"
assert_equal revision "$EXPECTED_REVISION" "$revision"
assert_equal image-version "$EXPECTED_VERSION" "$image_version"
assert_equal created "$EXPECTED_CREATED" "$created"
assert_equal version "$EXPECTED_VERSION" "$("$CTR" run --rm "$IMAGE" --version)"
