## ADDED Requirements

### Requirement: Serve AXFR over TCP for loaded zones

The zone-transfer subsystem SHALL answer AXFR queries (QTYPE=252, QCLASS=IN) received over TCP by streaming the zone's records in the order: SOA first, then all other records, then the same SOA again, per RFC 5936. AXFR queries received over UDP SHALL be refused.

#### Scenario: AXFR request returns full zone stream

- **WHEN** a permitted client sends `AXFR example.com. IN` over TCP
- **THEN** the server streams a response starting with the zone SOA, followed by all zone records, and ending with the zone SOA again

#### Scenario: AXFR over UDP is refused

- **WHEN** a client sends an AXFR query over UDP
- **THEN** the server returns `RCODE=REFUSED`


<!-- @trace
source: shadowdns-foundation
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - testdata/integration/named.conf
  - testdata/integration/master/example.com_view-th.fwd
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/master.zones
  - internal/alias/override.go
  - internal/server/listener.go
  - internal/transfer/notify.go
  - internal/view/matcher.go
  - internal/view/netmatch.go
  - internal/transfer/axfr.go
  - Makefile
  - README.md
  - internal/view/loader.go
  - internal/config/match.go
  - testdata/integration/README.md
  - internal/dnsutil/dnsutil.go
  - internal/zone/parser.go
  - internal/transfer/acl.go
  - testdata/integration/master/example.com_view-other.fwd
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/master/backup.example_view-th.fwd
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/master/backup.example_view-other.fwd
tests:
  - internal/view/testhelper_test.go
  - internal/view/geoip_country_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/zone/zone_test.go
  - internal/transfer/axfr_test.go
  - test/integration/backup_test.go
  - internal/view/netmatch_test.go
  - internal/view/geoip_asn_test.go
  - test/integration/query_test.go
  - internal/config/options_test.go
  - internal/view/loader_test.go
  - internal/zone/parser_test.go
  - internal/zone/classify_test.go
  - internal/config/aliases_test.go
  - internal/alias/rewrite_test.go
  - test/integration/negative_test.go
  - internal/alias/detect_test.go
  - internal/alias/override_test.go
  - internal/server/server_test.go
  - internal/view/matcher_test.go
  - test/integration/axfr_test.go
  - internal/config/zones_test.go
  - test/integration/helpers_test.go
  - internal/config/match_test.go
  - internal/transfer/acl_test.go
  - cmd/shadowdns/main_test.go
  - internal/alias/soa_test.go
  - internal/transfer/notify_test.go
-->

### Requirement: Stream alias-zone AXFR with rewritten records

When a client requests AXFR for a backup zone, the zone-transfer subsystem SHALL produce the stream by reading records from the corresponding root zone and applying the same owner-name and in-bailiwick rewrite rules that the alias-resolver uses for standard queries. TXT/MX/SRV overrides from the backup zone SHALL replace inherited records for their respective (owner, type) pairs in the stream.

#### Scenario: Backup-zone AXFR emits rewritten records

- **WHEN** a slave requests `AXFR backup.com. IN` and `backup.com` is aliased to `root.com`
- **THEN** the stream contains records with owner names under `backup.com.` and any in-bailiwick RDATA values rewritten from `.root.com` to `.backup.com`

#### Scenario: Backup-zone AXFR includes TXT override

- **WHEN** `backup.com` has an override `backup.com. TXT "google-site-verification=..."`
- **THEN** the AXFR stream contains the override TXT record AND does not contain the corresponding root zone TXT record rewritten


<!-- @trace
source: shadowdns-foundation
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - testdata/integration/named.conf
  - testdata/integration/master/example.com_view-th.fwd
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/master.zones
  - internal/alias/override.go
  - internal/server/listener.go
  - internal/transfer/notify.go
  - internal/view/matcher.go
  - internal/view/netmatch.go
  - internal/transfer/axfr.go
  - Makefile
  - README.md
  - internal/view/loader.go
  - internal/config/match.go
  - testdata/integration/README.md
  - internal/dnsutil/dnsutil.go
  - internal/zone/parser.go
  - internal/transfer/acl.go
  - testdata/integration/master/example.com_view-other.fwd
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/master/backup.example_view-th.fwd
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/master/backup.example_view-other.fwd
tests:
  - internal/view/testhelper_test.go
  - internal/view/geoip_country_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/zone/zone_test.go
  - internal/transfer/axfr_test.go
  - test/integration/backup_test.go
  - internal/view/netmatch_test.go
  - internal/view/geoip_asn_test.go
  - test/integration/query_test.go
  - internal/config/options_test.go
  - internal/view/loader_test.go
  - internal/zone/parser_test.go
  - internal/zone/classify_test.go
  - internal/config/aliases_test.go
  - internal/alias/rewrite_test.go
  - test/integration/negative_test.go
  - internal/alias/detect_test.go
  - internal/alias/override_test.go
  - internal/server/server_test.go
  - internal/view/matcher_test.go
  - test/integration/axfr_test.go
  - internal/config/zones_test.go
  - test/integration/helpers_test.go
  - internal/config/match_test.go
  - internal/transfer/acl_test.go
  - cmd/shadowdns/main_test.go
  - internal/alias/soa_test.go
  - internal/transfer/notify_test.go
-->

### Requirement: Enforce allow-transfer ACL

The zone-transfer subsystem SHALL consult the `allow-transfer` ACL declared in `named.conf` options (globally) before serving any AXFR. Only source IP addresses explicitly present in the ACL SHALL be served; all others SHALL receive `RCODE=REFUSED`.

#### Scenario: Permitted IP receives AXFR

- **WHEN** the ACL contains `192.0.2.10;` and a client with that source IP sends AXFR
- **THEN** the server streams the zone transfer

#### Scenario: Non-permitted IP is refused

- **WHEN** a client with a source IP not in the ACL sends AXFR
- **THEN** the server returns `RCODE=REFUSED` AND does not stream any records

#### Scenario: Empty allow-transfer denies all

- **WHEN** the ACL is empty or not declared
- **THEN** every AXFR request returns `RCODE=REFUSED`


<!-- @trace
source: shadowdns-foundation
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - testdata/integration/named.conf
  - testdata/integration/master/example.com_view-th.fwd
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/master.zones
  - internal/alias/override.go
  - internal/server/listener.go
  - internal/transfer/notify.go
  - internal/view/matcher.go
  - internal/view/netmatch.go
  - internal/transfer/axfr.go
  - Makefile
  - README.md
  - internal/view/loader.go
  - internal/config/match.go
  - testdata/integration/README.md
  - internal/dnsutil/dnsutil.go
  - internal/zone/parser.go
  - internal/transfer/acl.go
  - testdata/integration/master/example.com_view-other.fwd
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/master/backup.example_view-th.fwd
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/master/backup.example_view-other.fwd
tests:
  - internal/view/testhelper_test.go
  - internal/view/geoip_country_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/zone/zone_test.go
  - internal/transfer/axfr_test.go
  - test/integration/backup_test.go
  - internal/view/netmatch_test.go
  - internal/view/geoip_asn_test.go
  - test/integration/query_test.go
  - internal/config/options_test.go
  - internal/view/loader_test.go
  - internal/zone/parser_test.go
  - internal/zone/classify_test.go
  - internal/config/aliases_test.go
  - internal/alias/rewrite_test.go
  - test/integration/negative_test.go
  - internal/alias/detect_test.go
  - internal/alias/override_test.go
  - internal/server/server_test.go
  - internal/view/matcher_test.go
  - test/integration/axfr_test.go
  - internal/config/zones_test.go
  - test/integration/helpers_test.go
  - internal/config/match_test.go
  - internal/transfer/acl_test.go
  - cmd/shadowdns/main_test.go
  - internal/alias/soa_test.go
  - internal/transfer/notify_test.go
-->

### Requirement: Send NOTIFY on zone content change

On startup after all zones are loaded, and on every zone reload, the zone-transfer subsystem SHALL send DNS NOTIFY messages to each NS record target of the zone (excluding the zone's own primary, if identifiable via SOA MNAME). NOTIFY SHALL be sent over UDP to port 53; NOTIFY SHALL be retried up to 3 times on failure with exponential backoff (1s, 2s, 4s).

#### Scenario: NOTIFY sent to each NS target

- **WHEN** a zone has NS records `ns1.example.com.` and `ns2.example.com.` and neither equals the SOA MNAME
- **THEN** NOTIFY is sent to the resolved IP of each NS target

#### Scenario: NOTIFY retry on failure

- **WHEN** the first NOTIFY send returns no response within 5 seconds
- **THEN** the server retries after 1 second, then 2 seconds, then 4 seconds; after three failed attempts it logs an error and gives up

#### Scenario: NOTIFY not sent to SOA MNAME

- **WHEN** the zone has NS records including a target that equals the SOA MNAME
- **THEN** NOTIFY is not sent to that target (since it refers to the primary master itself)


<!-- @trace
source: shadowdns-foundation
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - testdata/integration/named.conf
  - testdata/integration/master/example.com_view-th.fwd
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/master.zones
  - internal/alias/override.go
  - internal/server/listener.go
  - internal/transfer/notify.go
  - internal/view/matcher.go
  - internal/view/netmatch.go
  - internal/transfer/axfr.go
  - Makefile
  - README.md
  - internal/view/loader.go
  - internal/config/match.go
  - testdata/integration/README.md
  - internal/dnsutil/dnsutil.go
  - internal/zone/parser.go
  - internal/transfer/acl.go
  - testdata/integration/master/example.com_view-other.fwd
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/master/backup.example_view-th.fwd
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/master/backup.example_view-other.fwd
tests:
  - internal/view/testhelper_test.go
  - internal/view/geoip_country_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/zone/zone_test.go
  - internal/transfer/axfr_test.go
  - test/integration/backup_test.go
  - internal/view/netmatch_test.go
  - internal/view/geoip_asn_test.go
  - test/integration/query_test.go
  - internal/config/options_test.go
  - internal/view/loader_test.go
  - internal/zone/parser_test.go
  - internal/zone/classify_test.go
  - internal/config/aliases_test.go
  - internal/alias/rewrite_test.go
  - test/integration/negative_test.go
  - internal/alias/detect_test.go
  - internal/alias/override_test.go
  - internal/server/server_test.go
  - internal/view/matcher_test.go
  - test/integration/axfr_test.go
  - internal/config/zones_test.go
  - test/integration/helpers_test.go
  - internal/config/match_test.go
  - internal/transfer/acl_test.go
  - cmd/shadowdns/main_test.go
  - internal/alias/soa_test.go
  - internal/transfer/notify_test.go
-->


<!-- @trace
source: notify-toggle
updated: 2026-04-15
code:
  - internal/config/options.go
  - CHANGELOG.md
  - README.md
  - cmd/shadowdns/main.go
  - packaging/named.conf.example
  - .release-please-manifest.json
tests:
  - internal/config/options_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/notify_test.go
-->


<!-- @trace
source: notify-glue-resolution
updated: 2026-05-14
code:
  - CLAUDE.md
  - cmd/shadowdns/pprof.go
  - cmd/shadowdns/main.go
  - internal/prunebackup/diff.go
  - cmd/shadowdns/reload.go
  - internal/view/geoip_country.go
  - scripts/smoke.sh
  - testdata/integration/README.md
  - packaging/aliases.yaml.example
  - internal/config/zones.go
  - internal/server/server.go
  - internal/shadowdnscfg/config.go
  - packaging/shadowdns.yaml.example
  - testdata/integration/aliases.yaml
  - internal/view/loader.go
  - internal/prunebackup/rewrite.go
  - docs/migration.md
  - internal/alias/rewrite.go
  - internal/config/options.go
  - internal/logging/reopen.go
  - packaging/logrotate.shadowdns
  - internal/server/handler.go
  - internal/zone/zone.go
  - go.mod
  - internal/transfer/axfr.go
  - internal/server/fingerprint.go
  - testdata/integration/master/example.com_view-th.fwd
  - packaging/shadowdns.service
  - internal/api/server.go
  - .github/workflows/release-please.yml
  - testdata/integration/shadowdns.yaml
  - .release-please-manifest.json
  - internal/transfer/notify.go
  - internal/alias/override.go
  - internal/zone/parser.go
  - cmd/shadowdns/prune_backup.go
  - docs/benchmark.md
  - internal/server/build.go
  - Makefile
  - internal/prunebackup/prunebackup.go
  - packaging/named.conf.example
  - docs/ephemeral-api.md
  - CHANGELOG.md
  - internal/ephemeral/store.go
  - internal/prunebackup/apply.go
  - scripts/test-deb.sh
  - internal/zone/classify.go
  - internal/server/listenaddr.go
  - internal/prunebackup/doc.go
  - internal/config/aliases.go
  - internal/prunebackup/include.go
  - scripts/gen-container-testdata.go
  - internal/server/listener.go
  - .spectra.yaml
  - README.md
  - testdata/integration/master/example.com_view-other.fwd
  - nfpm.yaml
  - go.sum
  - internal/view/geoip_asn.go
  - internal/logging/logger.go
  - internal/prunebackup/lexer.go
  - internal/dnsutil/dnsutil.go
tests:
  - cmd/shadowdns/prune_backup_test.go
  - internal/alias/rewrite_anywhere_test.go
  - test/integration/stress_ceiling_test.go
  - internal/zone/classify_test.go
  - test/integration/helpers_test.go
  - test/integration/notify_test.go
  - internal/server/server_test.go
  - cmd/shadowdns/main_test.go
  - internal/api/server_test.go
  - cmd/shadowdns/listenon_test.go
  - test/integration/axfr_test.go
  - internal/prunebackup/diff_test.go
  - internal/view/testhelper_test.go
  - internal/zone/zone_test.go
  - internal/ephemeral/store_test.go
  - internal/view/geoip_asn_test.go
  - test/integration/negative_test.go
  - test/integration/wildcard_test.go
  - internal/shadowdnscfg/config_test.go
  - test/integration/compression_budget_test.go
  - test/integration/reload_diff_test.go
  - test/integration/case_preservation_test.go
  - internal/config/zones_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/server/handler_test.go
  - internal/prunebackup/include_test.go
  - internal/alias/rewrite_test.go
  - internal/prunebackup/lexer_test.go
  - internal/logging/reopen_test.go
  - cmd/shadowdns/pprof_test.go
  - test/integration/listenon_test.go
  - test/integration/ephemeral_overrides_cname_test.go
  - internal/zone/parser_test.go
  - test/integration/query_test.go
  - internal/config/aliases_test.go
  - internal/server/fingerprint_test.go
  - internal/server/build_test.go
  - internal/view/loader_test.go
  - internal/transfer/notify_test.go
  - internal/prunebackup/prunebackup_test.go
  - test/integration/prune_backup_test.go
  - internal/alias/override_test.go
  - test/integration/stress_shared_bucket_test.go
  - internal/config/options_test.go
  - internal/transfer/axfr_test.go
  - internal/server/listenaddr_test.go
  - internal/prunebackup/apply_test.go
  - test/integration/alias_rdata_rewrite_test.go
  - test/integration/cname_following_test.go
  - cmd/shadowdns/main_ephemeral_test.go
  - internal/logging/logger_test.go
  - internal/view/geoip_country_test.go
  - test/integration/cname_synthesis_test.go
  - internal/server/handler_ephemeral_test.go
  - internal/prunebackup/rewrite_test.go
-->

### Requirement: Deny IXFR by responding with full AXFR

The zone-transfer subsystem SHALL NOT implement incremental transfer. On receiving an IXFR query (QTYPE=251), the server SHALL fall back to a full AXFR response per RFC 1995 (section 4), which is the protocol-defined fallback when the server cannot supply an incremental delta.

#### Scenario: IXFR query falls back to AXFR

- **WHEN** a slave sends `IXFR example.com. IN` with a SOA serial
- **THEN** the server responds with the same stream format as AXFR (SOA, records, SOA)


<!-- @trace
source: shadowdns-foundation
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - testdata/integration/named.conf
  - testdata/integration/master/example.com_view-th.fwd
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/master.zones
  - internal/alias/override.go
  - internal/server/listener.go
  - internal/transfer/notify.go
  - internal/view/matcher.go
  - internal/view/netmatch.go
  - internal/transfer/axfr.go
  - Makefile
  - README.md
  - internal/view/loader.go
  - internal/config/match.go
  - testdata/integration/README.md
  - internal/dnsutil/dnsutil.go
  - internal/zone/parser.go
  - internal/transfer/acl.go
  - testdata/integration/master/example.com_view-other.fwd
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/master/backup.example_view-th.fwd
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/master/backup.example_view-other.fwd
tests:
  - internal/view/testhelper_test.go
  - internal/view/geoip_country_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/zone/zone_test.go
  - internal/transfer/axfr_test.go
  - test/integration/backup_test.go
  - internal/view/netmatch_test.go
  - internal/view/geoip_asn_test.go
  - test/integration/query_test.go
  - internal/config/options_test.go
  - internal/view/loader_test.go
  - internal/zone/parser_test.go
  - internal/zone/classify_test.go
  - internal/config/aliases_test.go
  - internal/alias/rewrite_test.go
  - test/integration/negative_test.go
  - internal/alias/detect_test.go
  - internal/alias/override_test.go
  - internal/server/server_test.go
  - internal/view/matcher_test.go
  - test/integration/axfr_test.go
  - internal/config/zones_test.go
  - test/integration/helpers_test.go
  - internal/config/match_test.go
  - internal/transfer/acl_test.go
  - cmd/shadowdns/main_test.go
  - internal/alias/soa_test.go
  - internal/transfer/notify_test.go
-->

### Requirement: Refuse unknown or unsupported transfer types

The zone-transfer subsystem SHALL return `RCODE=REFUSED` for any zone-transfer-class query (AXFR or IXFR) that targets a zone not loaded by the server.

#### Scenario: AXFR for unknown zone is refused

- **WHEN** a permitted client requests `AXFR unknown.example. IN` and no zone `unknown.example.` is loaded
- **THEN** the server returns `RCODE=REFUSED`

## Requirements

<!-- @trace
source: shadowdns-foundation
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - testdata/integration/named.conf
  - testdata/integration/master/example.com_view-th.fwd
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/master.zones
  - internal/alias/override.go
  - internal/server/listener.go
  - internal/transfer/notify.go
  - internal/view/matcher.go
  - internal/view/netmatch.go
  - internal/transfer/axfr.go
  - Makefile
  - README.md
  - internal/view/loader.go
  - internal/config/match.go
  - testdata/integration/README.md
  - internal/dnsutil/dnsutil.go
  - internal/zone/parser.go
  - internal/transfer/acl.go
  - testdata/integration/master/example.com_view-other.fwd
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/master/backup.example_view-th.fwd
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/master/backup.example_view-other.fwd
tests:
  - internal/view/testhelper_test.go
  - internal/view/geoip_country_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/zone/zone_test.go
  - internal/transfer/axfr_test.go
  - test/integration/backup_test.go
  - internal/view/netmatch_test.go
  - internal/view/geoip_asn_test.go
  - test/integration/query_test.go
  - internal/config/options_test.go
  - internal/view/loader_test.go
  - internal/zone/parser_test.go
  - internal/zone/classify_test.go
  - internal/config/aliases_test.go
  - internal/alias/rewrite_test.go
  - test/integration/negative_test.go
  - internal/alias/detect_test.go
  - internal/alias/override_test.go
  - internal/server/server_test.go
  - internal/view/matcher_test.go
  - test/integration/axfr_test.go
  - internal/config/zones_test.go
  - test/integration/helpers_test.go
  - internal/config/match_test.go
  - internal/transfer/acl_test.go
  - cmd/shadowdns/main_test.go
  - internal/alias/soa_test.go
  - internal/transfer/notify_test.go
-->

### Requirement: Serve AXFR over TCP for loaded zones

The zone-transfer subsystem SHALL answer AXFR queries (QTYPE=252, QCLASS=IN) received over TCP by streaming the zone's records in the order: SOA first, then all other records, then the same SOA again, per RFC 5936. AXFR queries received over UDP SHALL be refused.

#### Scenario: AXFR request returns full zone stream

- **WHEN** a permitted client sends `AXFR example.com. IN` over TCP
- **THEN** the server streams a response starting with the zone SOA, followed by all zone records, and ending with the zone SOA again

#### Scenario: AXFR over UDP is refused

- **WHEN** a client sends an AXFR query over UDP
- **THEN** the server returns `RCODE=REFUSED`

---
### Requirement: Stream alias-zone AXFR with rewritten records

When a client requests AXFR for a backup zone, the zone-transfer subsystem SHALL produce the stream by reading records from the corresponding root zone and applying the same owner-name and in-bailiwick rewrite rules that the alias-resolver uses for standard queries. TXT/MX/SRV overrides from the backup zone SHALL replace inherited records for their respective (owner, type) pairs in the stream.

#### Scenario: Backup-zone AXFR emits rewritten records

- **WHEN** a slave requests `AXFR backup.com. IN` and `backup.com` is aliased to `root.com`
- **THEN** the stream contains records with owner names under `backup.com.` and any in-bailiwick RDATA values rewritten from `.root.com` to `.backup.com`

#### Scenario: Backup-zone AXFR includes TXT override

- **WHEN** `backup.com` has an override `backup.com. TXT "google-site-verification=..."`
- **THEN** the AXFR stream contains the override TXT record AND does not contain the corresponding root zone TXT record rewritten

---
### Requirement: Enforce allow-transfer ACL

The zone-transfer subsystem SHALL consult the `allow-transfer` ACL declared in `named.conf` options (globally) before serving any AXFR. Only source IP addresses explicitly present in the ACL SHALL be served; all others SHALL receive `RCODE=REFUSED`.

#### Scenario: Permitted IP receives AXFR

- **WHEN** the ACL contains `192.0.2.10;` and a client with that source IP sends AXFR
- **THEN** the server streams the zone transfer

#### Scenario: Non-permitted IP is refused

- **WHEN** a client with a source IP not in the ACL sends AXFR
- **THEN** the server returns `RCODE=REFUSED` AND does not stream any records

#### Scenario: Empty allow-transfer denies all

- **WHEN** the ACL is empty or not declared
- **THEN** every AXFR request returns `RCODE=REFUSED`

---
### Requirement: Send NOTIFY on zone content change

On startup after all zones are loaded, and on every zone reload, the zone-transfer subsystem SHALL send DNS NOTIFY messages to each NS record target of the zone (excluding the zone's own primary, if identifiable via SOA MNAME). NOTIFY SHALL be sent over UDP to port 53; NOTIFY SHALL be retried up to 3 times on failure with exponential backoff (1s, 2s, 4s).

The target IP address for each NOTIFY send SHALL be resolved **exclusively from in-zone glue records** — that is, from the A and AAAA records for the NS target name present in the same `*zone.Zone` instance that declares the NS record. The zone-transfer subsystem SHALL NOT invoke the operating system resolver, SHALL NOT perform recursive DNS queries, and SHALL NOT consult other loaded zones when resolving NS target names.

When an NS target has **multiple** in-zone glue IPs (e.g., one A and one AAAA record, or multiple A records), NOTIFY SHALL be sent to each IP independently; each `(zone, NS-hostname, IP)` tuple SHALL be treated as its own NOTIFY send subject to the retry and backoff policy above.

When an NS target has **no** in-zone glue (the target name has no A or AAAA record within the same zone), the zone-transfer subsystem SHALL skip that target: it SHALL NOT build a NOTIFY message, SHALL NOT spawn a send goroutine, and SHALL NOT fall back to any other resolution mechanism. The skip SHALL be recorded in the logs at debug severity with a `source` field whose value is `"skipped-no-glue"`.

Every NOTIFY log record (whether for an attempt, retry, or final failure) SHALL include a `source` field whose value is `"glue"` when the destination IP originated from an in-zone glue record.

Cross-view deduplication of NOTIFY sends SHALL be keyed by the tuple `(zone-origin, NS-hostname, IP)`. A given tuple SHALL result in at most one NOTIFY send sequence (including retries) per startup or reload event, even when the same zone appears in multiple views.

#### Scenario: NOTIFY sent to each in-zone glue IP of an NS target

- **WHEN** a zone `example.com.` has NS record `ns2.example.com.` with in-zone A record `ns2.example.com. A 10.0.0.2` and `ns2.example.com.` does not equal the SOA MNAME
- **THEN** NOTIFY is sent to `10.0.0.2:53` without invoking the operating system resolver

#### Scenario: NOTIFY sent to every glue IP when multiple exist

- **WHEN** a zone has NS record `ns21.example.com.` with in-zone records `ns21.example.com. A 10.0.0.21` and `ns21.example.com. AAAA 2001:db8::21`
- **THEN** one NOTIFY send sequence targets `10.0.0.21:53` and a separate NOTIFY send sequence targets `[2001:db8::21]:53`

#### Scenario: NS target without in-zone glue is skipped

- **WHEN** a zone `example.com.` has NS record `ns.other.tld.` and no A or AAAA record for `ns.other.tld.` exists within the `example.com.` zone data
- **THEN** no NOTIFY message is built, no goroutine is spawned, and no operating system resolution is attempted for that target; a log record at debug severity is emitted with field `source="skipped-no-glue"` identifying the zone and NS hostname

#### Scenario: NOTIFY retry on failure

- **WHEN** the first NOTIFY send to a resolved glue IP returns no response within 5 seconds
- **THEN** the server retries after 1 second, then 2 seconds, then 4 seconds; after three failed attempts it logs an error with field `source="glue"` and gives up

#### Scenario: NOTIFY not sent to SOA MNAME

- **WHEN** the zone has NS records including a target that equals the SOA MNAME
- **THEN** NOTIFY is not sent to that target, regardless of whether in-zone glue for the MNAME exists

#### Scenario: Cross-view deduplication by zone-host-IP tuple

- **WHEN** the same zone `example.com.` is loaded in two views and its NS record `ns2.example.com.` resolves via in-zone glue to the same IP `10.0.0.2` in both views
- **THEN** exactly one NOTIFY send sequence targeting `10.0.0.2:53` is executed for that zone during startup

---
### Requirement: Deny IXFR by responding with full AXFR

The zone-transfer subsystem SHALL NOT implement incremental transfer. On receiving an IXFR query (QTYPE=251), the server SHALL fall back to a full AXFR response per RFC 1995 (section 4), which is the protocol-defined fallback when the server cannot supply an incremental delta.

#### Scenario: IXFR query falls back to AXFR

- **WHEN** a slave sends `IXFR example.com. IN` with a SOA serial
- **THEN** the server responds with the same stream format as AXFR (SOA, records, SOA)

---
### Requirement: Refuse unknown or unsupported transfer types

The zone-transfer subsystem SHALL return `RCODE=REFUSED` for any zone-transfer-class query (AXFR or IXFR) that targets a zone not loaded by the server.

#### Scenario: AXFR for unknown zone is refused

- **WHEN** a permitted client requests `AXFR unknown.example. IN` and no zone `unknown.example.` is loaded
- **THEN** the server returns `RCODE=REFUSED`