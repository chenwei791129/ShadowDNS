<p align="center">
  <img src="assets/logo.png" alt="ShadowDNS Logo" width="480">
</p>

# ShadowDNS

[![CI](https://github.com/chenwei791129/ShadowDNS/actions/workflows/ci.yml/badge.svg)](https://github.com/chenwei791129/ShadowDNS/actions/workflows/ci.yml)

An authoritative DNS server with first-class zone aliasing for memory-efficient backup-domain serving.

## Why ShadowDNS?

Hosting many backup domains on BIND requires loading one complete zone copy per backup per view. In a typical split-horizon deployment with 3,000 backup domains across 7 views, that means roughly 21,000 redundant zone copies in memory — each backup a near-identical replica of its root domain, differing only in the zone name. At an average zone size of 10 KB, this accounts for approximately 210 MB of memory that carries no useful information.

ShadowDNS eliminates this waste through zone aliasing. Only root domains are fully loaded into memory. Backup domains are represented by a pointer to their root, and all queries against a backup zone are served on the fly via in-bailiwick rewriting: the answer looks exactly as if a complete backup zone had been loaded, but the server retains only one authoritative copy of the underlying data. In the reference deployment, this reduces memory usage by approximately 80% compared to the equivalent BIND master.

The design goal is transparent compatibility: clients querying backup domains see no difference in responses; existing BIND slaves continue to receive AXFR as before; and the management system that generates `named.conf` and zone files requires no changes. ShadowDNS reads your existing configuration files directly.

## How it works

```text
Client query
     |
     v
[ View Matcher ]
     |   Evaluates match-clients rules in declaration order (GeoIP country,
     |   GeoIP ASN, IP/CIDR, any). First match wins. Returns a view name.
     |
     v
[ Alias Resolver ]
     |   Checks whether the queried zone is a backup alias. If so, rewrites
     |   the query name from backup.domain to root.domain before lookup,
     |   and records the original backup name for the response.
     |
     v
[ Zone Lookup ]
     |   Finds the matching owner entry in the selected view's in-memory
     |   zone tree (a map[ownerName][]RR). O(1) lookup per owner name.
     |
     v
[ In-Bailiwick Rewrite ]
     |   Rewrites owner names back to the backup domain. For RDATA fields
     |   that contain a DNS name (CNAME target, NS, MX, SRV, SOA MNAME/
     |   RNAME): if the target points into the root zone, it is rewritten to
     |   the backup zone. Targets pointing elsewhere (e.g., a third-party
     |   CDN hostname) are preserved unchanged.
     |
     v
Response sent to client
```

**View Matcher**: Each view's `match-clients` block is compiled into an ordered rule slice at startup. Rules are evaluated left to right; the first rule that matches the client's source IP determines the view. If no view matches, the server returns REFUSED. GeoIP lookups use MaxMind's mmdb format read directly into memory at startup.

**Alias Resolver**: At query time, the resolver performs a longest-suffix match against the alias map (built from `aliases.yaml` at startup). A backup zone entry is a thin pointer — the resolver strips the backup suffix, replaces it with the root suffix, and hands the rewritten name to the zone lookup. The original backup name is retained so the rewrite stage can restore it.

**Zone Lookup**: Zone data is stored as `map[viewName]map[zoneName]*Zone`. Each `Zone` holds a `map[ownerName][]dns.RR`. All structures are read-only after startup; no locking is required on the read path. Backup override records (TXT, MX, SRV from a backup zone's own file, if provided) are stored separately and merged in after the root lookup.

**In-Bailiwick Rewrite**: The rewrite rule is intentionally conservative. Owner names are always rewritten (they are always in-bailiwick by definition). RDATA names are rewritten only when they point into the root zone — ensuring that the rewritten name will also resolve correctly through the same alias mechanism. A/AAAA records carry IP addresses and are never rewritten. TXT RDATA is opaque and is never rewritten, even if it contains a string that matches the root domain name.

## Compatibility with BIND

### Supported

- `named.conf` options block (`directory`, `geoip-directory`, `listen-on`, `allow-transfer`, `recursion`, `minimal-responses`, `version`, `hostname`, `transfer-format`)
- `view "<name>" { match-clients { ... }; ... }` with first-match semantics
- `match-clients` rule types: `geoip country <ISO-2>`, `geoip asnum "AS<N> <description>"`, bare IPv4 address, IPv4 CIDR prefix, `any`
- Zone files in RFC 1035 master file format (`$TTL`, `$ORIGIN`, `@`, multi-line `(...)`, `;` comments)
- `type master;` zones
- AXFR (full zone transfer over TCP) for both root zones and alias zones
- NOTIFY outbound to slave nameservers on startup and after reload
- `allow-transfer` ACL enforcement
- Split-horizon responses (different answers per view for the same query)
- SOA inheritance for backup zones (serial tracks the root zone; slaves detect changes correctly)

### Planned

- IXFR (incremental zone transfer) — slaves currently receive a full AXFR on each NOTIFY
- DNSSEC — signing, NSEC/NSEC3, DS records
- IPv6 listener
- DNS Cookies (RFC 7873) — server-side cookie validation to mitigate source IP spoofing
- Response Rate Limiting (RRL) — throttle excessive responses to mitigate DNS amplification attacks
- EDNS Client Subnet (ECS, RFC 7871) — improved GeoIP accuracy when queries arrive via resolvers
- Access logging — structured log file for query/response auditing
- Health check endpoint — HTTP `/healthz` for load balancer probes
- CNAME Flattening — resolve CNAME targets at query time and return A/AAAA directly, allowing CNAME to coexist with other record types at the zone apex

### Not supported

- Dynamic Update (RFC 2136) — not planned; all record changes go through zone file edits and reload
- Recursion — ShadowDNS is authoritative-only; `recursion no` is always in effect
- `type slave` or `type forward` zones — rejected at startup with a fatal error
- `allow-update`, `dnssec-enable` directives — rejected at startup

### Feature comparison

| Feature                        | BIND (master) | ShadowDNS    |
|-------------------------------|---------------|--------------|
| RFC 1035 zone file parsing    | Yes           | Yes          |
| Split-horizon views           | Yes           | Yes          |
| GeoIP country match           | Yes           | Yes          |
| GeoIP ASN match               | Yes           | Yes          |
| IP / CIDR match               | Yes           | Yes          |
| AXFR                          | Yes           | Yes          |
| NOTIFY                        | Yes           | Yes          |
| Zone aliasing (backup domain) | No            | Yes          |
| Hot reload (SIGHUP)           | Yes           | Yes          |
| Prometheus metrics            | No            | Yes          |
| IXFR                          | Yes           | Planned      |
| DNSSEC                        | Yes           | Planned      |
| IPv6 listener                 | Yes           | Planned      |
| DNS Cookies (RFC 7873)        | No            | Planned      |
| Response Rate Limiting (RRL)  | No            | Planned      |
| EDNS Client Subnet (ECS)      | No            | Planned      |
| Access logging                | Yes           | Planned      |
| Health check endpoint         | No            | Planned      |
| CNAME Flattening              | No            | Planned      |
| Dynamic Update                | Yes           | No           |
| Recursion                     | Configurable  | Always off   |

## Quick start

**1. Install Go 1.25+ and clone the repository.**

```bash
git clone https://github.com/example/shadowdns.git
cd shadowdns
```

**2. Build the binary.**

```bash
make build
```

The binary is written to `bin/shadowdns`.

**3. Place GeoIP databases in your GeoIP directory.**

ShadowDNS reads the directory from `geoip-directory` in `named.conf` (default: `/usr/local/share/GeoIP/`). The following files must be present:

```text
/usr/local/share/GeoIP/GeoLite2-Country.mmdb
/usr/local/share/GeoIP/GeoLite2-ASN.mmdb
```

Download these from [MaxMind](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data). ShadowDNS will refuse to start if either file is missing.

**4. Run ShadowDNS.**

```bash
./bin/shadowdns \
    --named-conf /etc/namedb/named.conf \
    --aliases    /etc/namedb/aliases.yaml
```

ShadowDNS listens on `:53` (UDP and TCP) by default. Use `--listen` to override.

## Configuration

### Command-line flags

| Flag              | Default | Required | Description                                              |
|-------------------|---------|----------|----------------------------------------------------------|
| `-named-conf`     | —       | Yes      | Path to `named.conf`. The `geoip-directory` option inside this file controls where mmdb files are read from. |
| `-aliases`        | —       | No       | Path to `aliases.yaml`. If omitted or file is absent, all zones are treated as root zones (no aliasing). |
| `-listen`         | `:53`   | No       | UDP/TCP listen address. Accepts any `host:port` form.    |
| `-metrics-addr`   | `:9153` | No       | Prometheus `/metrics` HTTP listen address. Empty string disables. |
| `-dry-run`        | `false` | No       | Load configuration and zones, log a summary, then exit without starting listeners. |
| `-reload`         | `false` | No       | Send SIGHUP to a running server. Requires `-named-conf`. |
| `-version`        | `false` | No       | Print version and exit.                                  |

### aliases.yaml schema

`aliases.yaml` maps each root domain to one or more backup domains. Backup domains will be served by rewriting queries to the corresponding root domain rather than loading their own zone data.

```yaml
# aliases.yaml
#
# Each key is a root domain (fully-loaded zone).
# Each value is a list of backup domains that alias to that root.

example.com:
  - backup-example.com
  - mirror-example.com

another-root.com:
  - backup-another.com
```

Rules:
- A backup domain may appear under exactly one root. Duplicates cause a fatal startup error.
- A domain cannot alias itself.
- Domains not listed here are treated as independent root zones and are loaded in full.
- Backup zones may optionally provide their own zone file containing TXT, MX, or SRV override records. A, AAAA, CNAME, NS, and SOA records in a backup zone file are discarded with a warning — those record types are always inherited from the root.

### GeoIP databases

The `geoip-directory` option in `named.conf` specifies where ShadowDNS looks for mmdb files. Both files are required:

| File                       | Used for                    |
|----------------------------|-----------------------------|
| `GeoLite2-Country.mmdb`    | `geoip country` rules       |
| `GeoLite2-ASN.mmdb`        | `geoip asnum` rules         |

ShadowDNS uses the same data source as BIND's `geoip` module, so GeoIP view assignment will be consistent when running both systems in parallel during migration.

### named.conf example

```text
options {
    directory           "/etc/namedb";
    geoip-directory     "/usr/local/share/GeoIP/";
    listen-on           { any; };
    listen-on-v6        { none; };
    recursion           no;
    minimal-responses   yes;
    version             none;
    hostname            none;
    allow-transfer      { 192.0.2.10; 192.0.2.11; };
};

include "master.zones";
```

## Project status

ShadowDNS v1 is a foundation implementation. The config loader, zone parser, view matcher, alias resolver, DNS server, zone transfer, and integration test suite are complete.

This software has not yet been deployed to production. Integration testing against a production-scale dataset (12,000+ zone files across 7 views) is planned before any production cutover. See the full architectural decisions and four-phase migration plan at [openspec/changes/shadowdns-foundation/design.md](openspec/changes/shadowdns-foundation/design.md).

Known limitations to address before production use:

- IXFR is not supported; slaves perform a full AXFR on each NOTIFY.

## Pre-deployment Checklist

The following items are open questions from [design.md](openspec/changes/shadowdns-foundation/design.md) that **must be verified by ops** before switching production traffic to ShadowDNS.

### 1. `view-other` must be the last view in match-clients order

ShadowDNS uses first-match semantics (identical to BIND). Any view whose `match-clients` block contains `any;` will match **every** client. If such a view appears before a more-specific view (e.g., `view-th` with GeoIP rules), the GeoIP view will never be reached — all traffic will fall into the `any` view.

**Required action**: confirm that the view whose `match-clients` is `any;` is declared **last** in `master.zones`. ShadowDNS will log a warning at startup if a non-last view uses `any`, but this does not prevent startup.

### 2. ASN description string format must match the parser expectation

`match-clients` rules of the form `geoip asnum "AS64500 Test ASN"` are parsed by extracting the leading numeric component (the regex `^AS(\d+)\s`). The description text after the space is ignored. This means:

- `"AS64500 Test ASN"` and `"AS64500 Any Description"` both match ASN 64500.
- If a rule string does **not** start with `AS` followed by digits and a space (e.g., `"64500"` without the `AS` prefix), the parser will fail at startup with an error.

**Required action**: verify that all `geoip asnum` entries in your `master.zones` follow the `"AS<number> <description>"` format before deploying. Cross-check with the actual strings in your `named.conf` / `master.zones` generated by the management system.

### 3. Run `--dry-run` against production config before cutover

```bash
make build

./bin/shadowdns \
    --named-conf /etc/namedb/named.conf \
    --aliases    /etc/namedb/aliases.yaml \
    --dry-run
```

A successful exit (code 0) confirms that every zone file parses without error and the GeoIP databases are readable. See [docs/benchmark.md](docs/benchmark.md) for memory profiling guidance.

### 4. NOTIFY outbound targets

ShadowDNS sends NOTIFY to the NS records of each zone (BIND default behaviour). If your deployment requires `also-notify` targets that are not in NS records, add those hosts to the NS records or wait for a future version that supports `also-notify`.

## License

TBD
