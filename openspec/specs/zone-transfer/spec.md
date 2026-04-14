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

#### Scenario: NOTIFY sent to each NS target

- **WHEN** a zone has NS records `ns1.example.com.` and `ns2.example.com.` and neither equals the SOA MNAME
- **THEN** NOTIFY is sent to the resolved IP of each NS target

#### Scenario: NOTIFY retry on failure

- **WHEN** the first NOTIFY send returns no response within 5 seconds
- **THEN** the server retries after 1 second, then 2 seconds, then 4 seconds; after three failed attempts it logs an error and gives up

#### Scenario: NOTIFY not sent to SOA MNAME

- **WHEN** the zone has NS records including a target that equals the SOA MNAME
- **THEN** NOTIFY is not sent to that target (since it refers to the primary master itself)

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