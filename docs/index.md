# ShadowDNS

<p align="center">
  <img src="assets/logo.png" alt="ShadowDNS Logo" width="480">
</p>

ShadowDNS is an authoritative DNS server whose core feature is **zone aliasing**: serving a large number of backup domains at an extremely low memory cost, while remaining fully transparent and compatible with clients, BIND slaves, and existing management systems.

## Why ShadowDNS?

When serving a large number of backup domains on BIND, every backup domain requires a full zone copy to be loaded in every view. Take a typical split-horizon deployment as an example — 3,000 backup domains × 7 views — and roughly 21,000 nearly identical zone copies exist in memory, differing only in the zone name. With an average zone size of 10 KB, this amounts to about 210 MB of memory overhead carrying no useful information whatsoever.

ShadowDNS eliminates this waste through zone aliasing:

- Only the root domain is fully loaded into memory.
- A backup domain is just a pointer to the root.
- Queries against a backup zone are rewritten on the fly via **in-bailiwick rewriting**: the response looks exactly the same as if a complete backup zone had been loaded, but the server keeps only a single copy of the authoritative data.

In the reference deployment, memory usage is reduced by about **80%** compared with an equivalent BIND master.

## Design Goal: Transparent Compatibility

- Clients querying backup domains see no difference in responses.
- Existing BIND slaves keep receiving zone transfers via AXFR as usual.
- Management systems that generate `named.conf` and zone files require no changes — ShadowDNS reads the existing configuration files directly.

## Feature Comparison with BIND

| Feature                            | BIND (master) | ShadowDNS  |
|------------------------------------|---------------|------------|
| BIND `named.conf` drop-in          | Native        | Yes        |
| RFC 1035 zone file parsing         | Yes           | Yes        |
| Split-horizon views                | Yes           | Yes        |
| GeoIP country matching             | Yes           | Yes        |
| GeoIP ASN matching                 | Yes           | Yes        |
| IP / CIDR matching                 | Yes           | Yes        |
| AXFR                               | Yes           | Yes        |
| NOTIFY                             | Yes           | Yes        |
| Wildcard records (RFC 4592)        | Yes           | Yes        |
| Zone aliasing (backup domains)     | No            | Yes        |
| Hot reload (SIGHUP)                | Yes           | Yes        |
| Prometheus metrics                 | No            | Yes        |
| Grafana dashboard (built-in)       | No            | Yes        |
| IXFR                               | Yes           | No         |
| DNSSEC                             | Yes           | No         |
| IPv6 listener                      | Yes           | Yes        |
| DNS Cookies (RFC 7873)             | Yes           | Yes        |
| Response Rate Limiting (RRL)       | Yes           | Yes        |
| EDNS Client Subnet (ECS, RFC 7871) | No            | Yes (opt-in via `--ecs-enable`, default off) |
| Query logging (BIND format)        | Yes           | Yes        |
| CNAME Flattening (external targets) | No           | No         |
| In-bailiwick CNAME Flattening      | No            | Planned    |
| CNAME chain collapsing in responses | No           | Yes (opt-in per alias group, default off) |
| Dynamic Update                     | Yes           | No         |
| Recursion                          | Configurable  | Always off |

!!! note "Project status"
    ShadowDNS is currently in the v0.x experimental stage and has not been deployed to production. Before switching production traffic over, the plan is to complete integration testing with a production-scale dataset (7 views, 12,000+ zone files).

## Next Steps

- [Quick Start](getting-started.md) — the shortest path from build to launch
- [Installation](installation.md) — building from source and installing the `.deb` package
- [How Zone Aliasing Works](guides/zone-aliasing.md) — the query processing pipeline and rewrite rules
- [Migrating from BIND](migration.md) — the four-phase cutover procedure and rollback strategy
