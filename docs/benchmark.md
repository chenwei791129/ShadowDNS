# ShadowDNS Startup Performance Benchmark

This document records how the `--dry-run` startup smoke test is run, along with sample output.

## What Is `--dry-run`?

The `--dry-run` flag makes ShadowDNS execute the full loading process (parsing `named.conf`, reading all zone files, loading the GeoIP mmdb files), but exit and print a summary **before** starting the UDP/TCP listeners. It is suitable for:

- Confirming the configuration file syntax is correct
- Computing a memory baseline without affecting DNS service
- A configuration validation step in CI/CD pipelines

## Build Steps

```bash
# Run from the repo root
go build -o ./shadowdns ./cmd/shadowdns
```

## Usage

```bash
./shadowdns \
    --named-conf /path/to/named.conf \
    --config     /path/to/shadowdns.yaml \
    --dry-run
```

On success it exits with code 0 and outputs:

```
level=INFO msg="dry-run: configuration loaded successfully" views=N zones=M
```

## Automated Smoke Test Script

`scripts/smoke.sh` automates the following steps:

1. Build the binary
2. Copy `testdata/integration/` to `/tmp/shadowdns-smoke/`, replacing `TESTDATA_DIR_PLACEHOLDER`
3. Generate test GeoIP mmdb files
4. Run `--dry-run` under `/usr/bin/time`, recording memory usage

```bash
./scripts/smoke.sh
```

## Sample Output (2026-04-13, Apple Silicon, testdata/integration fixture)

Execution environment: macOS Darwin 24.6.0, Apple M-series, Go 1.25.6

```
time=2026-04-13T23:28:01.556+08:00 level=INFO msg="shadowdns starting" \
    named_conf=...named.conf config=...shadowdns.yaml listen=:53
time=2026-04-13T23:28:01.557+08:00 level=INFO msg="loaded GeoIP country database" \
    path=.../geoip/GeoLite2-Country.mmdb
time=2026-04-13T23:28:01.558+08:00 level=INFO msg="loaded GeoIP ASN database" \
    path=.../geoip/GeoLite2-ASN.mmdb
time=2026-04-13T23:28:01.558+08:00 level=INFO msg="dry-run: configuration loaded successfully" \
    views=2 zones=4
        0.31 real         0.00 user         0.01 sys
             8437760  maximum resident set size
```

| Metric                         | Value             |
|-------------------------------|-------------------|
| Load time (real)               | 0.31 s            |
| Maximum RSS (maximum resident set size) | 8,437,760 bytes (≈ 8.0 MB) |
| View count (views)             | 2                 |
| Loaded zone count (zones)      | 4                 |

## Notes

- **This fixture is tiny**: only 2 views × 2 zones (1 root + 1 backup); memory is almost entirely consumed by the Go runtime.
- **Production-scale deployment estimate**: based on the numbers in Context (3,600 root domains × 7 views, average zone size 10 KB), ShadowDNS is expected to load only root zones (no duplicate loading of backups), giving roughly `3,600 × 7 × 10 KB ≈ 252 MB` plus the GeoIP mmdb files (about 60–80 MB), for a total of about **330–350 MB** — roughly 45–50% savings compared with BIND's ~630 MB (including redundant backups).
- Actual production memory must be measured with a production-scale configuration; `--dry-run` provides a baseline, and once actually listening, the Go runtime may grow slightly due to goroutine stacks and similar factors.
- It is recommended to add a `./scripts/smoke.sh` step in CI to ensure the configuration loads correctly after every merge.
