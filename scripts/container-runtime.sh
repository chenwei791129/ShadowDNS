#!/usr/bin/env bash
# Print the preferred container runtime executable.
#
# A runtime only counts when its CLI is on PATH *and* it can reach a working
# daemon/machine: an installed-but-idle podman (e.g. a macOS machine that was
# never started) must not shadow a usable docker.
set -euo pipefail

for runtime in podman docker; do
    if command -v "$runtime" >/dev/null 2>&1 && "$runtime" info >/dev/null 2>&1; then
        printf '%s\n' "$runtime"
        exit 0
    fi
done

echo "Error: no usable container runtime found — podman and docker are absent from PATH or unreachable" >&2
exit 1
