#!/usr/bin/env bash
# smoke.sh — smoke test for ShadowDNS --dry-run mode.
#
# Builds the binary, copies testdata/integration into /tmp/shadowdns-smoke,
# substitutes TESTDATA_DIR_PLACEHOLDER with the real path, generates minimal
# GeoIP mmdb files using Go, and runs the binary with --dry-run.
#
# Memory is measured via /usr/bin/time on both macOS and Linux.
#
# Usage:
#   ./scripts/smoke.sh [--verbose]
#
# Requirements: Go toolchain in PATH, standard POSIX utilities.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

SMOKE_DIR="${TMPDIR:-/tmp}"
SMOKE_DIR="${SMOKE_DIR%/}/shadowdns-smoke"
# Matches the `$BINARY` path set in the Makefile: bin/shadowdns-<GOOS>-<GOARCH>.
BINARY="${REPO_ROOT}/bin/shadowdns-$(go env GOOS)-$(go env GOARCH)"
FIXTURE_SRC="${REPO_ROOT}/testdata/integration"

VERBOSE=0
[[ "${1:-}" == "--verbose" ]] && VERBOSE=1

info() { echo "[smoke] $*"; }
die()  { echo "[smoke] ERROR: $*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# 1. Build the binary.
# ---------------------------------------------------------------------------
if [[ ! -x "${BINARY}" ]]; then
	info "Building binary..."
	(cd "${REPO_ROOT}" && make build)
fi
info "Binary: ${BINARY}"

# ---------------------------------------------------------------------------
# 2. Prepare the smoke directory.
# ---------------------------------------------------------------------------
info "Preparing smoke fixture in ${SMOKE_DIR}..."
rm -rf "${SMOKE_DIR}"
mkdir -p "${SMOKE_DIR}"

# Copy fixtures (skip geoip/ — we generate it below).
cp    "${FIXTURE_SRC}/named.conf"    "${SMOKE_DIR}/named.conf"
cp    "${FIXTURE_SRC}/master.zones"  "${SMOKE_DIR}/master.zones"
cp    "${FIXTURE_SRC}/aliases.yaml"  "${SMOKE_DIR}/aliases.yaml"

# Generate a minimal unified shadowdns.yaml for --config. The historical
# standalone aliases.yaml used `root: [backups]` shape; the unified schema
# uses `aliases: {backup: root}`. Convert by inverting — testdata ships only
# `example.com: [backup.example]`, so the inverted map is a single entry.
cat > "${SMOKE_DIR}/shadowdns.yaml" <<'YAML'
aliases:
  backup.example: example.com
YAML
# Recursively copy the full master/ tree (zone files plus include fragments
# that may live in subdirectories such as cnames/).
cp -R "${FIXTURE_SRC}/master"        "${SMOKE_DIR}/master"

# Substitute TESTDATA_DIR_PLACEHOLDER → SMOKE_DIR.
sed -i.bak "s|TESTDATA_DIR_PLACEHOLDER|${SMOKE_DIR}|g" "${SMOKE_DIR}/named.conf"
rm -f "${SMOKE_DIR}/named.conf.bak"

# Rewrite relative include and file paths to absolute paths.
sed -i.bak 's|include "master.zones";|include "'"${SMOKE_DIR}/master.zones"'";|g' "${SMOKE_DIR}/named.conf"
rm -f "${SMOKE_DIR}/named.conf.bak"

sed -i.bak 's|file "master/|file "'"${SMOKE_DIR}/master/"'|g' "${SMOKE_DIR}/master.zones"
rm -f "${SMOKE_DIR}/master.zones.bak"

# ---------------------------------------------------------------------------
# 3. Generate GeoIP mmdb files using Go.
# ---------------------------------------------------------------------------
GEOIP_DIR="${SMOKE_DIR}/geoip"
mkdir -p "${GEOIP_DIR}"

info "Generating GeoIP mmdb files..."
# Write and run a small Go script using PEP-723-equivalent inline metadata.
MMDB_GEN="${SMOKE_DIR}/gen_mmdb.go"
cat > "${MMDB_GEN}" << 'GOEOF'
//go:build ignore

package main

import (
	"log"
	"net"
	"os"
	"path/filepath"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

func writeDB(path string, opts mmdbwriter.Options, records map[string]mmdbtype.DataType) {
	w, err := mmdbwriter.New(opts)
	if err != nil {
		log.Fatalf("create writer: %v", err)
	}
	_, all, _ := net.ParseCIDR("0.0.0.0/0")
	if err := w.Insert(all, mmdbtype.Map{}); err != nil {
		log.Fatalf("insert default: %v", err)
	}
	for cidr, rec := range records {
		_, ipnet, _ := net.ParseCIDR(cidr)
		if err := w.Insert(ipnet, rec); err != nil {
			log.Fatalf("insert %s: %v", cidr, err)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		log.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if _, err := w.WriteTo(f); err != nil {
		log.Fatalf("write %s: %v", path, err)
	}
}

func main() {
	outDir := os.Args[1]

	writeDB(filepath.Join(outDir, "GeoLite2-Country.mmdb"),
		mmdbwriter.Options{DatabaseType: "GeoLite2-Country", RecordSize: 24, IncludeReservedNetworks: true},
		map[string]mmdbtype.DataType{
			"192.0.2.0/24":   mmdbtype.Map{"country": mmdbtype.Map{"iso_code": mmdbtype.String("TH")}},
			"198.51.100.0/24": mmdbtype.Map{"country": mmdbtype.Map{"iso_code": mmdbtype.String("JP")}},
		},
	)
	writeDB(filepath.Join(outDir, "GeoLite2-ASN.mmdb"),
		mmdbwriter.Options{DatabaseType: "GeoLite2-ASN", RecordSize: 24, IncludeReservedNetworks: true},
		map[string]mmdbtype.DataType{
			"203.0.113.0/24": mmdbtype.Map{
				"autonomous_system_number":       mmdbtype.Uint32(64500),
				"autonomous_system_organization": mmdbtype.String("AS64500 Test ASN"),
			},
		},
	)
	log.Printf("GeoIP mmdb files written to %s", outDir)
}
GOEOF

# Run the generator inside the repo so it can use go.mod dependencies.
(cd "${REPO_ROOT}" && go run "${MMDB_GEN}" "${GEOIP_DIR}")
info "GeoIP files ready."

# ---------------------------------------------------------------------------
# 4. Run the binary with -dry-run, measuring memory.
# ---------------------------------------------------------------------------
info "Running shadowdns --dry-run..."

TIME_CMD="/usr/bin/time"
if [[ "$(uname)" == "Darwin" ]]; then
	TIME_ARGS="-l"
	MEM_GREP="maximum resident set size"
else
	TIME_ARGS="-v"
	MEM_GREP="Maximum resident set size"
fi

OUTPUT_FILE="${SMOKE_DIR}/dry_run_output.txt"

# Run with time; capture combined output.
"${TIME_CMD}" ${TIME_ARGS} "${BINARY}" \
	--named-conf "${SMOKE_DIR}/named.conf" \
	--config     "${SMOKE_DIR}/shadowdns.yaml" \
	--dry-run    \
	2>&1 | tee "${OUTPUT_FILE}"

info "Smoke test passed."

# Extract and display the memory figure.
MEM_LINE="$(grep -i "${MEM_GREP}" "${OUTPUT_FILE}" || true)"
if [[ -n "${MEM_LINE}" ]]; then
	info "Memory: ${MEM_LINE}"
fi

info "Output saved to: ${OUTPUT_FILE}"
