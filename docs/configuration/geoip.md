# GeoIP Databases

ShadowDNS's view matching uses GeoIP databases in MaxMind mmdb format, the same data source as BIND's `geoip` module — so when running both systems in parallel during migration, GeoIP view decisions will be consistent.

## Required files

GeoIP databases are **conditional**: the mmdb files are only needed when any view's `match-clients` uses `geoip country` / `geoip asnum` rules, or when the `geoip-directory` option is set in `named.conf`. An absent `geoip-directory` and an empty one (`geoip-directory "";`) are equivalent — both count as unset.

When `geoip-directory` is set (non-empty), it specifies the directory containing the mmdb files (e.g. `/usr/local/share/GeoIP/`), and **both** of the following files must exist; if either is missing, ShadowDNS refuses to start:

| File | Purpose |
|------|------|
| `GeoLite2-Country.mmdb` | `geoip country` rules |
| `GeoLite2-ASN.mmdb` | `geoip asnum` rules |

Download source: [MaxMind GeoLite2](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data).

When `geoip-directory` is unset but at least one view uses geo rules, startup and `--dry-run` fail with a configuration error naming the first offending view, including its source file path and line number:

```text
loading GeoIP: /etc/shadowdns/named.conf:42: view "asia" uses geoip match-clients rules but geoip-directory is not set in named.conf options
```

(On a SIGHUP reload the wrapper prefix is `reloading GeoIP:` and the running configuration is kept.)

When `geoip-directory` is unset and no view uses geo rules, the server starts and serves without any mmdb file — `any`, IP, and CIDR rules work unchanged.

## Loading and updates

- The mmdb files are read directly into memory.
- Every SIGHUP reload **reopens** the mmdb files — after dropping in MaxMind's monthly update, sending SIGHUP is all it takes; no process restart needed.
- After a successful reload, the `shadowdns_geoip_db_info` gauge reflects the new `build_time`, which can be used to confirm the update took effect.
- The same conditional logic applies on reload, so GeoIP can be enabled or disabled by a SIGHUP: setting `geoip-directory` loads the mmdb files on the spot (failure keeps the old configuration), while unsetting it — with no geo rules left — keeps the server running without any databases.

## Running without GeoIP

When no GeoIP databases are loaded:

- The metrics endpoint exposes **no `shadowdns_geoip_db_info` series**, and a reload that disables GeoIP deletes the previously exported series.
- With `--ecs-enable` active, ShadowDNS emits a **Warn** log — once at startup, and again on any reload that ends in that state — because ECS cannot influence view selection without GeoIP databases; only the [ECS option echo](../guides/ecs.md#response-echo) behavior remains.
- The "shadowdns ready", "reload complete", and dry-run summary logs each carry a boolean `geoip_enabled` field, so whether databases are loaded is always auditable from the log.

## Monthly update SOP

```bash
# 1. Place the updated mmdb files into the GeoIP directory
cp GeoLite2-Country.mmdb GeoLite2-ASN.mmdb /usr/local/share/GeoIP/

# 2. Trigger a hot reload
shadowdns reload --named-conf /etc/shadowdns/named.conf
# (or: sudo systemctl reload shadowdns)

# 3. Verify build_time via the metrics endpoint
curl -s localhost:9153/metrics | grep shadowdns_geoip_db_info
```
