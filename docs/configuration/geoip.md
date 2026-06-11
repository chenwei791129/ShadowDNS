# GeoIP Databases

ShadowDNS's view matching uses GeoIP databases in MaxMind mmdb format, the same data source as BIND's `geoip` module — so when running both systems in parallel during migration, GeoIP view decisions will be consistent.

## Required files

The `geoip-directory` option in `named.conf` specifies the directory containing the mmdb files (default `/usr/local/share/GeoIP/`). **Both** of the following files must exist; if either is missing, ShadowDNS refuses to start:

| File | Purpose |
|------|------|
| `GeoLite2-Country.mmdb` | `geoip country` rules |
| `GeoLite2-ASN.mmdb` | `geoip asnum` rules |

Download source: [MaxMind GeoLite2](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data).

## Loading and updates

- The mmdb files are read directly into memory.
- Every SIGHUP reload **reopens** the mmdb files — after dropping in MaxMind's monthly update, sending SIGHUP is all it takes; no process restart needed.
- After a successful reload, the `shadowdns_geoip_db_info` gauge reflects the new `build_time`, which can be used to confirm the update took effect.

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
