# day2-operations-guide Specification

## Purpose

Define what the ShadowDNS Day 2 operations guide must document for operators running ShadowDNS in production after migration cutover: reload failure detection, GeoIP database freshness, ephemeral record lifecycle awareness, rolling restarts, continuous answer consistency checks, query log disk management, upgrade/rollback SOP, and latency monitoring.

## Requirements

### Requirement: Day 2 reload failure detection

The operations guide SHALL document how to detect and respond to a silent SIGHUP reload failure. The guide SHALL prescribe metric-based alerting as the primary detection mechanism, using the `shadowdns_reload_total{result="failure"}` counter and the `shadowdns_config_last_reload_success_timestamp_seconds` gauge (both available as of the `reload-coverage-and-metrics` change). The guide SHALL include a serial probe procedure using `dig @127.0.0.1 <zone> SOA +short` and comparison against the expected serial as the per-push verification step. The guide SHALL define the expected serial as the value declared in the SOA record of the zone file on disk (obtainable via `grep -m1 -A1 'SOA' /path/to/zone/file` or equivalent). The guide SHALL document that a mismatch between the live-served serial and the on-disk serial indicates stale configuration is being served and SHALL prescribe alerting and zone file rollback steps. The guide SHALL additionally document a log-based check on the ERROR-level `reload failed` line in `/var/log/shadowdns/shadowdns.log` (console-encoded format: `ERROR` level tag followed by the `reload failed` message, not logfmt `level=` key-value pairs) as a supplementary measure for deployments without Prometheus scraping.

#### Scenario: Operator performs reload and verifies success

- **WHEN** operator sends SIGHUP to shadowdns and runs the serial probe against the zone SOA
- **THEN** the guide prescribes: (1) read the current serial from the zone file on disk, (2) run `dig @127.0.0.1 <zone> SOA +short` to obtain the live-served serial, (3) compare — if equal the reload succeeded; if the live serial is older than the on-disk serial, the reload failed silently and the operator SHALL alert and roll back the zone file

#### Scenario: Reload fails silently

- **WHEN** shadowdns receives SIGHUP but fails to reload (e.g., config parse error)
- **THEN** the guide documents that the process continues serving the previous configuration with the old serial, `shadowdns_reload_total{result="failure"}` increments (the prescribed alert fires on `increase(...) > 0`), `shadowdns_config_last_reload_success_timestamp_seconds` stops advancing, and the operator can additionally grep for the ERROR-level `reload failed` line in `/var/log/shadowdns/shadowdns.log`

<!-- @trace
source: migration-guide-improvements
updated: 2026-06-11
-->


<!-- @trace
source: migration-guide-improvements
updated: 2026-06-11
code:
  - internal/logging/reopen.go
  - internal/server/server.go
  - internal/server/handler.go
  - docs/migration.md
  - README.md
  - cmd/shadowdns/main.go
  - internal/metrics/metrics.go
tests:
  - cmd/shadowdns/querylog_test.go
  - internal/server/handler_ratelimit_test.go
  - internal/logging/reopen_test.go
  - cmd/shadowdns/main_test.go
  - internal/server/handler_querylog_test.go
  - internal/metrics/metrics_test.go
  - cmd/shadowdns/main_reload_test.go
  - cmd/shadowdns/main_ephemeral_test.go
  - internal/metrics/metrics_reload_test.go
-->

---
### Requirement: GeoIP DB expiry monitoring

The operations guide SHALL document how to monitor GeoIP database freshness using the `shadowdns_geoip_db_info{build_time}` metric. The guide SHALL prescribe an alert threshold of 35 days. The guide SHALL document that mmdb files are re-opened on every SIGHUP reload (as of the `reload-coverage-and-metrics` change); a full process restart is NOT required for GeoIP updates. The guide SHALL include a monthly maintenance procedure: download the new database archives from MaxMind, verify the archive checksum (MaxMind publishes SHA256 files for the tar.gz archives, not the bare mmdb files), extract, send SIGHUP to each instance one at a time, and verify the `shadowdns_geoip_db_info{build_time}` metric reflects the new build date on each instance before proceeding to the next (consulting reload metrics and the application log when the build date fails to update).

#### Scenario: GeoIP DB expires without update

- **WHEN** the mmdb build timestamp reported by `shadowdns_geoip_db_info` exceeds 35 days from the current date
- **THEN** the monitoring alert fires and the operator follows the monthly maintenance SOP to load the new mmdb via SIGHUP reload

#### Scenario: Monthly GeoIP maintenance

- **WHEN** operator performs the monthly GeoIP update
- **THEN** the guide prescribes: download new database archives, verify archive checksum, extract, send SIGHUP to one instance at a time, verify `shadowdns_geoip_db_info{build_time}` reflects the new build date on each instance before proceeding to the next

<!-- @trace
source: migration-guide-improvements
updated: 2026-06-11
-->


<!-- @trace
source: migration-guide-improvements
updated: 2026-06-11
code:
  - internal/logging/reopen.go
  - internal/server/server.go
  - internal/server/handler.go
  - docs/migration.md
  - README.md
  - cmd/shadowdns/main.go
  - internal/metrics/metrics.go
tests:
  - cmd/shadowdns/querylog_test.go
  - internal/server/handler_ratelimit_test.go
  - internal/logging/reopen_test.go
  - cmd/shadowdns/main_test.go
  - internal/server/handler_querylog_test.go
  - internal/metrics/metrics_test.go
  - cmd/shadowdns/main_reload_test.go
  - cmd/shadowdns/main_ephemeral_test.go
  - internal/metrics/metrics_reload_test.go
-->

---
### Requirement: Ephemeral record restart awareness

The operations guide SHALL document that ephemeral DNS-01 challenge records are stored in memory only and do not survive a config reload or process restart. The guide SHALL document that a successful SIGHUP reload unconditionally clears the ephemeral store (as implemented in `cmd/shadowdns/main.go` `reload()` and noted in `internal/ephemeral/store.go`: "ephemeral state does not survive a config reload"). The guide SHALL prescribe that operators verify no active DNS-01 challenge is in progress before either restarting or sending SIGHUP to shadowdns; because the ephemeral API exposes only PUT/DELETE endpoints and cannot enumerate records, the prescribed verification is querying the `_acme-challenge` TXT record via `dig` and/or checking ACME client logs. The guide SHALL recommend scheduling shadowdns restarts and reloads to avoid overlap with ACME certificate renewal windows.

#### Scenario: Operator restarts or reloads shadowdns while DNS-01 challenge is active

- **WHEN** operator is about to restart shadowdns OR send SIGHUP to trigger a reload
- **THEN** the guide prescribes a pre-operation checklist item: confirm no DNS-01 challenge is active by querying the `_acme-challenge` TXT record via `dig` or checking ACME client logs; if a challenge is active, wait for it to complete before proceeding with the restart or reload

<!-- @trace
source: migration-guide-improvements
updated: 2026-06-11
-->


<!-- @trace
source: migration-guide-improvements
updated: 2026-06-11
code:
  - internal/logging/reopen.go
  - internal/server/server.go
  - internal/server/handler.go
  - docs/migration.md
  - README.md
  - cmd/shadowdns/main.go
  - internal/metrics/metrics.go
tests:
  - cmd/shadowdns/querylog_test.go
  - internal/server/handler_ratelimit_test.go
  - internal/logging/reopen_test.go
  - cmd/shadowdns/main_test.go
  - internal/server/handler_querylog_test.go
  - internal/metrics/metrics_test.go
  - cmd/shadowdns/main_reload_test.go
  - cmd/shadowdns/main_ephemeral_test.go
  - internal/metrics/metrics_reload_test.go
-->

---
### Requirement: Rolling restart operations

The operations guide SHALL document that, as of the current release (post `reload-coverage-and-metrics`), zone data, GeoIP mmdb, RRL configuration, query log settings, and the `aliases:` section of `shadowdns.yaml` are all applied via SIGHUP reload; only CLI-flag changes (e.g., `--log-file`, `--listen`, `--metrics-addr`; all flags are process-lifetime sticky), listen-address changes, and `ephemeral_api:` section changes (the API server is constructed once at startup and is not re-read on reload) require a full process restart. The guide SHALL document that a cold-start QPS penalty of approximately 30% has been observed on the first dnspyre benchmark run after a restart (benchmark observation, not a service-capacity guarantee). The guide SHALL prescribe rolling restart procedure: restart one instance at a time, wait for the instance to warm up and QPS to return to baseline before proceeding to the next instance. The guide SHALL state that production deployment SHALL maintain at least 2 instances to enable rolling restarts without service interruption.

#### Scenario: Operator applies a configuration change requiring restart

- **WHEN** operator changes a setting that requires restart (any CLI flag, a listen-on / listen-on-v6 address change, or an `ephemeral_api:` section change)
- **THEN** the guide prescribes batching restart-requiring changes into a maintenance window, performing rolling restart one instance at a time, and verifying QPS recovery before proceeding to the next instance

<!-- @trace
source: migration-guide-improvements
updated: 2026-06-11
-->


<!-- @trace
source: migration-guide-improvements
updated: 2026-06-11
code:
  - internal/logging/reopen.go
  - internal/server/server.go
  - internal/server/handler.go
  - docs/migration.md
  - README.md
  - cmd/shadowdns/main.go
  - internal/metrics/metrics.go
tests:
  - cmd/shadowdns/querylog_test.go
  - internal/server/handler_ratelimit_test.go
  - internal/logging/reopen_test.go
  - cmd/shadowdns/main_test.go
  - internal/server/handler_querylog_test.go
  - internal/metrics/metrics_test.go
  - cmd/shadowdns/main_reload_test.go
  - cmd/shadowdns/main_ephemeral_test.go
  - internal/metrics/metrics_reload_test.go
-->

---
### Requirement: Continuous answer consistency regression

The operations guide SHALL document that answer-diff validation SHALL be performed as a routine Day 2 activity, not only at migration cutover. The guide SHALL prescribe running answer-diff after every zone change push, comparing two instances (e.g., BIND vs ShadowDNS, or old vs new version). The guide SHALL note that ShadowDNS alias/CNAME flattening rewrite logic SHALL be treated as a source of potential edge-case differences and these SHALL be investigated.

#### Scenario: Zone change is pushed to production

- **WHEN** a zone file update is deployed
- **THEN** the guide prescribes running the answer-diff script against both instances for the changed zone, recording any RDATA differences, and investigating any difference before declaring the deployment complete

<!-- @trace
source: migration-guide-improvements
updated: 2026-06-11
-->


<!-- @trace
source: migration-guide-improvements
updated: 2026-06-11
code:
  - internal/logging/reopen.go
  - internal/server/server.go
  - internal/server/handler.go
  - docs/migration.md
  - README.md
  - cmd/shadowdns/main.go
  - internal/metrics/metrics.go
tests:
  - cmd/shadowdns/querylog_test.go
  - internal/server/handler_ratelimit_test.go
  - internal/logging/reopen_test.go
  - cmd/shadowdns/main_test.go
  - internal/server/handler_querylog_test.go
  - internal/metrics/metrics_test.go
  - cmd/shadowdns/main_reload_test.go
  - cmd/shadowdns/main_ephemeral_test.go
  - internal/metrics/metrics_reload_test.go
-->

---
### Requirement: Query log disk management

The operations guide SHALL document that authoritative DNS query logs generate high volume and SHALL prescribe steps to verify logrotate configuration in `packaging/logrotate.shadowdns`. The guide SHALL note that the appropriate rotation frequency depends on actual query volume and SHALL recommend capacity testing. The guide SHALL document that application-layer errors appear in `/var/log/shadowdns/shadowdns.log` (requires sudo) and are distinct from query logs.

#### Scenario: Query log disk usage exceeds threshold

- **WHEN** `/var/log/shadowdns` disk usage exceeds the configured threshold
- **THEN** the guide prescribes: check logrotate configuration frequency, verify logrotate is running via `logrotate -d /etc/logrotate.d/shadowdns`, and adjust rotation frequency or retention period to match actual query volume

<!-- @trace
source: migration-guide-improvements
updated: 2026-06-11
-->


<!-- @trace
source: migration-guide-improvements
updated: 2026-06-11
code:
  - internal/logging/reopen.go
  - internal/server/server.go
  - internal/server/handler.go
  - docs/migration.md
  - README.md
  - cmd/shadowdns/main.go
  - internal/metrics/metrics.go
tests:
  - cmd/shadowdns/querylog_test.go
  - internal/server/handler_ratelimit_test.go
  - internal/logging/reopen_test.go
  - cmd/shadowdns/main_test.go
  - internal/server/handler_querylog_test.go
  - internal/metrics/metrics_test.go
  - cmd/shadowdns/main_reload_test.go
  - cmd/shadowdns/main_ephemeral_test.go
  - internal/metrics/metrics_reload_test.go
-->

---
### Requirement: Upgrade and rollback SOP

The operations guide SHALL document the standard upgrade procedure for v0.x.x releases, including: download new package, run `shadowdns --dry-run` to validate new config parsing, perform rolling restart, and rollback procedure on failure. The guide SHALL document that v0.x.x releases SHALL be assumed to potentially include breaking CLI/config changes, and `--dry-run` validation is mandatory before applying. The guide SHALL document rollback as: reinstall previous `.deb` package via `dpkg -i`, restart service, verify operation.

#### Scenario: Operator upgrades to a new shadowdns release

- **WHEN** a new shadowdns `.deb` package is available
- **THEN** the guide prescribes: (1) run `shadowdns --dry-run --named-conf <path> --config <path>` against the current config, (2) if dry-run passes, perform rolling restart with the new package, (3) if any instance fails to start, reinstall the previous package and restart

<!-- @trace
source: migration-guide-improvements
updated: 2026-06-11
-->


<!-- @trace
source: migration-guide-improvements
updated: 2026-06-11
code:
  - internal/logging/reopen.go
  - internal/server/server.go
  - internal/server/handler.go
  - docs/migration.md
  - README.md
  - cmd/shadowdns/main.go
  - internal/metrics/metrics.go
tests:
  - cmd/shadowdns/querylog_test.go
  - internal/server/handler_ratelimit_test.go
  - internal/logging/reopen_test.go
  - cmd/shadowdns/main_test.go
  - internal/server/handler_querylog_test.go
  - internal/metrics/metrics_test.go
  - cmd/shadowdns/main_reload_test.go
  - cmd/shadowdns/main_ephemeral_test.go
  - internal/metrics/metrics_reload_test.go
-->

---
### Requirement: Latency monitoring guidance

The operations guide SHALL document the `shadowdns_dns_request_duration_seconds` histogram metric for latency monitoring, noting its bucket boundaries span 0.1 ms to 100 ms (refined by the `dns-latency-histogram-buckets` change). The guide SHALL prescribe alerting on p99 latency exceeding a defined threshold.

#### Scenario: Operator sets up latency alerting

- **WHEN** operator configures monitoring for shadowdns
- **THEN** the guide prescribes using `shadowdns_dns_request_duration_seconds` to derive p50/p95/p99 latency, and setting a p99 alert threshold appropriate to the SLA (example threshold: >10ms p99 for authoritative DNS)

<!-- @trace
source: migration-guide-improvements
updated: 2026-06-11
-->

<!-- @trace
source: migration-guide-improvements
updated: 2026-06-11
code:
  - internal/logging/reopen.go
  - internal/server/server.go
  - internal/server/handler.go
  - docs/migration.md
  - README.md
  - cmd/shadowdns/main.go
  - internal/metrics/metrics.go
tests:
  - cmd/shadowdns/querylog_test.go
  - internal/server/handler_ratelimit_test.go
  - internal/logging/reopen_test.go
  - cmd/shadowdns/main_test.go
  - internal/server/handler_querylog_test.go
  - internal/metrics/metrics_test.go
  - cmd/shadowdns/main_reload_test.go
  - cmd/shadowdns/main_ephemeral_test.go
  - internal/metrics/metrics_reload_test.go
-->