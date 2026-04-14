## ADDED Requirements

### Requirement: Listen for DNS queries on UDP and TCP port 53

The dns-server SHALL bind both UDP and TCP listeners on the configured address (default `0.0.0.0:53`) and serve DNS queries on both transports. The TCP listener SHALL remain required even when zone transfer is disabled, because TCP is the RFC 7766 fallback for responses larger than the UDP payload limit.

#### Scenario: UDP query receives response

- **WHEN** a client sends a valid DNS query over UDP on port 53
- **THEN** the server responds over UDP within the same 5-tuple

#### Scenario: TCP query receives response

- **WHEN** a client sends a valid DNS query over TCP on port 53
- **THEN** the server accepts the connection, reads the 2-byte length prefix, and writes a length-prefixed response

#### Scenario: Response exceeding UDP limit sets TC flag

- **WHEN** a response over UDP would exceed 512 bytes (or the negotiated EDNS0 UDP size) and cannot be truncated to fit
- **THEN** the server sets the TC (truncated) flag in the UDP response header so the client falls back to TCP


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

### Requirement: Operate in authoritative-only mode

The dns-server SHALL set `AA` (authoritative answer) flag in responses for queries matching a loaded zone and SHALL NOT perform recursion regardless of the query's RD (recursion desired) flag. The RA (recursion available) flag SHALL be set to 0.

#### Scenario: AA flag is set on authoritative answer

- **WHEN** the server answers a query for a name within a loaded zone
- **THEN** the response header has `AA=1`

#### Scenario: RA flag is always 0

- **WHEN** any response is produced
- **THEN** the response header has `RA=0`

#### Scenario: Recursion-desired query is not recursed

- **WHEN** a query arrives with `RD=1` for a name outside all loaded zones
- **THEN** the server responds REFUSED or the appropriate non-recursive error AND does not initiate any outbound DNS query


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

### Requirement: Answer queries using view, alias, and zone data

For every query, the dns-server SHALL (a) determine the view via the view-matcher using the client source IP, (b) identify the matched zone and any alias mapping via the alias-resolver, (c) look up records in the selected view's zone data, (d) apply in-bailiwick rewrite rules for backup zones, and (e) produce a response.

#### Scenario: Same query produces different answers per view

- **WHEN** two clients in different countries resolve to different views AND each view's zone data for `example.com A` differs
- **THEN** each client receives the answer from its respective view

#### Scenario: Backup-zone query uses alias-resolver

- **WHEN** a client queries `www.backup.com A` where `backup.com` is a backup of `root.com`
- **THEN** the server returns an A record with owner `www.backup.com.` whose RDATA comes from `www.root.com.` in the selected view


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

### Requirement: Produce SOA in authority section for NXDOMAIN and NODATA

When the query target falls within a loaded zone but the queried name does not exist (NXDOMAIN) or the queried name exists but has no records of the requested type (NODATA), the dns-server SHALL include the zone's SOA record in the authority section of the response. The TTL of the SOA record in the authority section SHALL be the minimum of the zone's SOA TTL and the zone's SOA minimum field, enabling correct negative caching per RFC 2308.

#### Scenario: NXDOMAIN includes SOA

- **WHEN** a query for `nonexistent.root.com. A` is received and no matching name exists in the zone
- **THEN** the response has `RCODE=NXDOMAIN`, empty answer section, and an SOA record in the authority section

#### Scenario: NODATA includes SOA

- **WHEN** `www.root.com. AAAA` is queried, `www.root.com.` has an A record but no AAAA record
- **THEN** the response has `RCODE=NOERROR`, empty answer section, and an SOA record in the authority section

#### Scenario: Backup zone NXDOMAIN includes rewritten SOA

- **WHEN** a query for `nonexistent.backup.com. A` is received
- **THEN** the response authority section contains a SOA record owned by `backup.com.` with MNAME/RNAME rewritten by in-bailiwick rules


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

### Requirement: Serve the zone SOA on explicit SOA query

When a client explicitly queries a zone's apex SOA record, the dns-server SHALL return the SOA in the answer section with `RCODE=NOERROR` and `AA=1`.

#### Scenario: Explicit SOA query on root zone

- **WHEN** a client queries `root.com. SOA`
- **THEN** the response answer section contains the zone SOA record

#### Scenario: Explicit SOA query on backup zone

- **WHEN** a client queries `backup.com. SOA`
- **THEN** the response answer section contains an SOA whose serial is inherited from root and whose owner/MNAME/RNAME are rewritten


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

### Requirement: Hide server identity

The dns-server SHALL NOT reveal its software name or host identity in responses. Queries for `version.bind. CHAOS TXT` SHALL return REFUSED or an empty TXT response; queries for `hostname.bind. CHAOS TXT` and `id.server. CHAOS TXT` SHALL behave identically.

#### Scenario: version.bind query is refused

- **WHEN** a client queries `version.bind. CH TXT`
- **THEN** the response has `RCODE=REFUSED` (or empty TXT with RCODE=NOERROR) AND contains no ShadowDNS version string

#### Scenario: hostname.bind query is refused

- **WHEN** a client queries `hostname.bind. CH TXT`
- **THEN** the response does not contain the host's hostname


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

### Requirement: Return minimal responses by default

The dns-server SHALL operate in minimal-responses mode: additional-section glue records SHALL NOT be added automatically for NS or MX answers unless required for correctness (e.g., glue for in-bailiwick NS targets when serving a referral). The authority section SHALL be populated only for NXDOMAIN/NODATA (SOA) and delegations (NS).

#### Scenario: Plain A query has empty authority and additional sections

- **WHEN** a query for `www.root.com. A` is successfully answered
- **THEN** the response answer section contains the A record AND the authority and additional sections are empty


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

### Requirement: Handle malformed or unsupported queries without crashing

The dns-server SHALL return `RCODE=FORMERR` for queries that cannot be parsed, `RCODE=NOTIMP` for unsupported opcodes (e.g., UPDATE), and `RCODE=REFUSED` for queries outside any loaded zone. It SHALL NOT panic or terminate the process on any malformed input.

#### Scenario: Unparseable query returns FORMERR

- **WHEN** a UDP packet is received that is not a valid DNS message
- **THEN** the server returns a DNS response with `RCODE=FORMERR` if the header is parseable, or drops the packet silently if it is not

#### Scenario: UPDATE opcode returns NOTIMP

- **WHEN** a client sends a DNS UPDATE (opcode 5) message
- **THEN** the server returns `RCODE=NOTIMP`

#### Scenario: Out-of-zone query returns REFUSED

- **WHEN** a client queries a name outside every loaded zone
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

### Requirement: Listen for DNS queries on UDP and TCP port 53

The dns-server SHALL bind both UDP and TCP listeners on the configured address (default `0.0.0.0:53`) and serve DNS queries on both transports. The TCP listener SHALL remain required even when zone transfer is disabled, because TCP is the RFC 7766 fallback for responses larger than the UDP payload limit.

#### Scenario: UDP query receives response

- **WHEN** a client sends a valid DNS query over UDP on port 53
- **THEN** the server responds over UDP within the same 5-tuple

#### Scenario: TCP query receives response

- **WHEN** a client sends a valid DNS query over TCP on port 53
- **THEN** the server accepts the connection, reads the 2-byte length prefix, and writes a length-prefixed response

#### Scenario: Response exceeding UDP limit sets TC flag

- **WHEN** a response over UDP would exceed 512 bytes (or the negotiated EDNS0 UDP size) and cannot be truncated to fit
- **THEN** the server sets the TC (truncated) flag in the UDP response header so the client falls back to TCP

---
### Requirement: Operate in authoritative-only mode

The dns-server SHALL set `AA` (authoritative answer) flag in responses for queries matching a loaded zone and SHALL NOT perform recursion regardless of the query's RD (recursion desired) flag. The RA (recursion available) flag SHALL be set to 0.

#### Scenario: AA flag is set on authoritative answer

- **WHEN** the server answers a query for a name within a loaded zone
- **THEN** the response header has `AA=1`

#### Scenario: RA flag is always 0

- **WHEN** any response is produced
- **THEN** the response header has `RA=0`

#### Scenario: Recursion-desired query is not recursed

- **WHEN** a query arrives with `RD=1` for a name outside all loaded zones
- **THEN** the server responds REFUSED or the appropriate non-recursive error AND does not initiate any outbound DNS query

---
### Requirement: Answer queries using view, alias, and zone data

For every query, the dns-server SHALL (a) determine the view via the view-matcher using the client source IP, (b) identify the matched zone and any alias mapping via the alias-resolver, (c) look up records in the selected view's zone data, (d) apply in-bailiwick rewrite rules for backup zones, and (e) produce a response.

#### Scenario: Same query produces different answers per view

- **WHEN** two clients in different countries resolve to different views AND each view's zone data for `example.com A` differs
- **THEN** each client receives the answer from its respective view

#### Scenario: Backup-zone query uses alias-resolver

- **WHEN** a client queries `www.backup.com A` where `backup.com` is a backup of `root.com`
- **THEN** the server returns an A record with owner `www.backup.com.` whose RDATA comes from `www.root.com.` in the selected view

---
### Requirement: Produce SOA in authority section for NXDOMAIN and NODATA

When the query target falls within a loaded zone but the queried name does not exist (NXDOMAIN) or the queried name exists but has no records of the requested type (NODATA), the dns-server SHALL include the zone's SOA record in the authority section of the response. The TTL of the SOA record in the authority section SHALL be the minimum of the zone's SOA TTL and the zone's SOA minimum field, enabling correct negative caching per RFC 2308.

#### Scenario: NXDOMAIN includes SOA

- **WHEN** a query for `nonexistent.root.com. A` is received and no matching name exists in the zone
- **THEN** the response has `RCODE=NXDOMAIN`, empty answer section, and an SOA record in the authority section

#### Scenario: NODATA includes SOA

- **WHEN** `www.root.com. AAAA` is queried, `www.root.com.` has an A record but no AAAA record
- **THEN** the response has `RCODE=NOERROR`, empty answer section, and an SOA record in the authority section

#### Scenario: Backup zone NXDOMAIN includes rewritten SOA

- **WHEN** a query for `nonexistent.backup.com. A` is received
- **THEN** the response authority section contains a SOA record owned by `backup.com.` with MNAME/RNAME rewritten by in-bailiwick rules

---
### Requirement: Serve the zone SOA on explicit SOA query

When a client explicitly queries a zone's apex SOA record, the dns-server SHALL return the SOA in the answer section with `RCODE=NOERROR` and `AA=1`.

#### Scenario: Explicit SOA query on root zone

- **WHEN** a client queries `root.com. SOA`
- **THEN** the response answer section contains the zone SOA record

#### Scenario: Explicit SOA query on backup zone

- **WHEN** a client queries `backup.com. SOA`
- **THEN** the response answer section contains an SOA whose serial is inherited from root and whose owner/MNAME/RNAME are rewritten

---
### Requirement: Hide server identity

The dns-server SHALL NOT reveal its software name or host identity in responses. Queries for `version.bind. CHAOS TXT` SHALL return REFUSED or an empty TXT response; queries for `hostname.bind. CHAOS TXT` and `id.server. CHAOS TXT` SHALL behave identically.

#### Scenario: version.bind query is refused

- **WHEN** a client queries `version.bind. CH TXT`
- **THEN** the response has `RCODE=REFUSED` (or empty TXT with RCODE=NOERROR) AND contains no ShadowDNS version string

#### Scenario: hostname.bind query is refused

- **WHEN** a client queries `hostname.bind. CH TXT`
- **THEN** the response does not contain the host's hostname

---
### Requirement: Return minimal responses by default

The dns-server SHALL operate in minimal-responses mode: additional-section glue records SHALL NOT be added automatically for NS or MX answers unless required for correctness (e.g., glue for in-bailiwick NS targets when serving a referral). The authority section SHALL be populated only for NXDOMAIN/NODATA (SOA) and delegations (NS).

#### Scenario: Plain A query has empty authority and additional sections

- **WHEN** a query for `www.root.com. A` is successfully answered
- **THEN** the response answer section contains the A record AND the authority and additional sections are empty

---
### Requirement: Handle malformed or unsupported queries without crashing

The dns-server SHALL return `RCODE=FORMERR` for queries that cannot be parsed, `RCODE=NOTIMP` for unsupported opcodes (e.g., UPDATE), and `RCODE=REFUSED` for queries outside any loaded zone. It SHALL NOT panic or terminate the process on any malformed input.

#### Scenario: Unparseable query returns FORMERR

- **WHEN** a UDP packet is received that is not a valid DNS message
- **THEN** the server returns a DNS response with `RCODE=FORMERR` if the header is parseable, or drops the packet silently if it is not

#### Scenario: UPDATE opcode returns NOTIMP

- **WHEN** a client sends a DNS UPDATE (opcode 5) message
- **THEN** the server returns `RCODE=NOTIMP`

#### Scenario: Out-of-zone query returns REFUSED

- **WHEN** a client queries a name outside every loaded zone
- **THEN** the server returns `RCODE=REFUSED`