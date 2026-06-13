# Quick Start

This page walks you through the shortest path to getting ShadowDNS running: building from source, preparing the GeoIP databases, and starting the service.

## Prerequisites

- Go 1.26+
- MaxMind GeoLite2 databases (Country + ASN, see step 3 — only needed when views use `geoip` rules)
- An existing BIND configuration (`named.conf` and zone files)

## 1. Get the Source

```bash
git clone https://github.com/chenwei791129/ShadowDNS.git
cd ShadowDNS
```

## 2. Build

```bash
make build
```

The resulting binary is located at `bin/shadowdns-<GOOS>-<GOARCH>` (e.g., `bin/shadowdns-linux-amd64` on Linux/amd64, `bin/shadowdns-darwin-arm64` on Apple Silicon).

## 3. Prepare the GeoIP Databases

ShadowDNS reads the mmdb directory from the `geoip-directory` option in `named.conf` (e.g., `/usr/local/share/GeoIP/`). When `geoip-directory` is set, the following two files **must exist** — if either is missing, startup is refused:

```text
/usr/local/share/GeoIP/GeoLite2-Country.mmdb
/usr/local/share/GeoIP/GeoLite2-ASN.mmdb
```

Download them from [MaxMind](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data). If no view uses `geoip country` / `geoip asnum` rules, leave `geoip-directory` unset and skip this step — ShadowDNS then starts without any mmdb files. See [GeoIP Databases](configuration/geoip.md) for details.

## 4. Start ShadowDNS

Use the binary produced in step 2 (the example below assumes linux/amd64):

```bash
./bin/shadowdns-linux-amd64 \
    --named-conf /etc/bind/named.conf \
    --config     /etc/bind/shadowdns.yaml
```

ShadowDNS listens on `:53` (UDP and TCP) by default; this can be overridden with `--listen`.

For the contents and format of `shadowdns.yaml`, see [shadowdns.yaml Configuration](configuration/shadowdns-yaml.md).

## 5. Validate the Configuration with `--dry-run`

Before starting for real, it is recommended to first verify with `--dry-run` that all zone files parse correctly and the GeoIP databases are readable:

```bash
./bin/shadowdns-linux-amd64 \
    --named-conf /etc/bind/named.conf \
    --config     /etc/bind/shadowdns.yaml \
    --dry-run
```

`--dry-run` loads the configuration and zones, prints a summary, and exits immediately without opening any listener. An exit code of 0 means the configuration is valid.

## Next Steps

- [Installation](installation.md) — deploy to a Debian/Ubuntu host with the `.deb` package, managed by systemd
- [CLI Reference](reference/cli.md) — complete flag and subcommand documentation
- [Migrating from BIND](migration.md) — the full operational guide for production cutover
