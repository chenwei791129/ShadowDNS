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


<!-- @trace
source: honor-listen-on
updated: 2026-04-15
code:
  - internal/server/server.go
  - internal/server/listenaddr.go
  - internal/server/listener.go
  - cmd/shadowdns/main.go
  - README.md
  - docs/migration.md
tests:
  - internal/server/addrset_test.go
  - internal/server/listenaddr_test.go
  - cmd/shadowdns/listenon_test.go
  - test/integration/listenon_test.go
  - internal/server/bindmany_test.go
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

The dns-server SHALL bind both UDP and TCP listeners for every address in its resolved listen-address set (see "Derive listen address set from named.conf listen-on" below) and serve DNS queries on both transports on every successfully bound address. The TCP listener SHALL remain required even when zone transfer is disabled, because TCP is the RFC 7766 fallback for responses larger than the UDP payload limit.

#### Scenario: UDP query receives response

- **WHEN** a client sends a valid DNS query over UDP on port 53 to any address the server has successfully bound
- **THEN** the server responds over UDP within the same 5-tuple

#### Scenario: TCP query receives response

- **WHEN** a client sends a valid DNS query over TCP on port 53 to any address the server has successfully bound
- **THEN** the server accepts the connection, reads the 2-byte length prefix, and writes a length-prefixed response

#### Scenario: Response exceeding UDP limit sets TC flag

- **WHEN** a response over UDP would exceed 512 bytes (or the negotiated EDNS0 UDP size) and cannot be truncated to fit
- **THEN** the server sets the TC (truncated) flag in the UDP response header so the client falls back to TCP

---
### Requirement: Derive listen address set from named.conf listen-on

The dns-server SHALL derive its listen-address set at startup (and at every SIGHUP reload that reparses named.conf) using this precedence:

1. If the `-listen` CLI flag has a non-empty host component (e.g. `127.0.0.1:5353`, `10.0.0.1:53`), the listen-address set SHALL contain exactly the single address passed via `-listen`, and `options.listen-on` from named.conf SHALL be ignored. This preserves override semantics for tests and special deployments.
2. Otherwise (the host component is empty, e.g. `:53`, `:5353`, `:0`), if `options.listen-on` in named.conf is non-empty, the listen-address set SHALL be the IPv4 addresses resolved from the `listen-on` token list, each combined with the port component of `-listen` (default 53).
3. Otherwise (empty host AND `listen-on` absent), the listen-address set SHALL be resolved as if `listen-on { any; };` were specified, with the port component from `-listen`.

In all cases the port component from `-listen` (default 53) SHALL be applied consistently: when `-listen` is `:5353`, every resolved IPv4 address SHALL use port 5353.

Token resolution rules for `listen-on`:

- `any` SHALL expand to every IPv4 address reported by `net.InterfaceAddrs()` for interfaces that are up, excluding IPv6 addresses and excluding the link-local range `169.254.0.0/16`. Loopback addresses (`127.0.0.0/8`, including aliases like `127.0.0.53`) SHALL be included.
- `none` SHALL resolve to an empty set; when the resulting listen-address set is empty and `none` was the explicit cause, the server SHALL exit with a fatal error explaining that no IPv4 listeners would be started.
- Literal IPv4 addresses SHALL be used as-is.
- Unsupported tokens (exclusion syntax `!addr`, ACL names, `port N`, `interface` keyword) SHALL be logged at WARN level including the offending token, and the token SHALL be skipped. Parsing MUST NOT fail because of unsupported tokens.

SIGHUP reload SHALL NOT rebind listeners even if the resolved listen-address set would change; operators needing a binding change MUST restart the process. The dns-server SHALL log an INFO message at reload time stating that listen-address changes require restart if the resolved set differs from the currently bound set.

#### Scenario: listen-on { any; } binds every IPv4 interface address

- **WHEN** named.conf sets `listen-on { any; };` AND the host has IPv4 addresses `10.0.0.1`, `127.0.0.1`, and `127.0.0.53`
- **THEN** the server attempts to bind UDP and TCP on `10.0.0.1:53`, `127.0.0.1:53`, and `127.0.0.53:53`

#### Scenario: listen-on with explicit addresses binds only those addresses

- **WHEN** named.conf sets `listen-on { 10.0.0.1; 192.168.1.1; };`
- **THEN** the server attempts to bind UDP and TCP on exactly `10.0.0.1:53` and `192.168.1.1:53` AND does not attempt any other address

#### Scenario: -listen override bypasses listen-on

- **WHEN** the process is started with `-listen 127.0.0.1:5353` AND named.conf sets `listen-on { any; };`
- **THEN** the server binds UDP and TCP only on `127.0.0.1:5353` AND does not enumerate interface addresses

#### Scenario: -listen with empty host does not bypass listen-on

- **WHEN** the process is started without `-listen` (or with any `:PORT` form that has no host, such as `:53` or `:0`) AND named.conf sets `listen-on { 10.0.0.1; };`
- **THEN** the server binds on `10.0.0.1:<port>` AND not on the wildcard `0.0.0.0:<port>`

#### Scenario: Port from -listen is applied to listen-on addresses

- **WHEN** the process is started with `-listen :5353` AND named.conf sets `listen-on { 10.0.0.1; };`
- **THEN** the server binds on `10.0.0.1:5353`

#### Scenario: Unsupported listen-on token is skipped with a warning

- **WHEN** named.conf sets `listen-on { !10.0.0.1; any; };`
- **THEN** the server logs a WARN message naming the unsupported token `!10.0.0.1` AND proceeds to resolve `any`

#### Scenario: listen-on { none; } on IPv4 causes fatal error

- **WHEN** named.conf sets `listen-on { none; };` AND `-listen` is at its default value
- **THEN** the server exits with a fatal error indicating no IPv4 listeners would be started

#### Scenario: SIGHUP reload does not rebind listeners

- **WHEN** the server is running with listeners bound for `listen-on { 10.0.0.1; };` AND named.conf is edited to `listen-on { 10.0.0.2; };` AND SIGHUP is sent
- **THEN** the server continues serving on `10.0.0.1:53` only AND logs an INFO message stating that listen-address changes require restart


<!-- @trace
source: honor-listen-on
updated: 2026-04-15
code:
  - internal/server/server.go
  - internal/server/listenaddr.go
  - internal/server/listener.go
  - cmd/shadowdns/main.go
  - README.md
  - docs/migration.md
tests:
  - internal/server/addrset_test.go
  - internal/server/listenaddr_test.go
  - cmd/shadowdns/listenon_test.go
  - test/integration/listenon_test.go
  - internal/server/bindmany_test.go
-->

---
### Requirement: Tolerate per-address bind failures

The dns-server SHALL attempt to bind each address in the resolved listen-address set independently. For each address, UDP and TCP SHALL be bound as an atomic pair: if either the UDP or the TCP bind for a given address fails, the already-bound half of that pair SHALL be closed and the address SHALL be counted as failed. Address-level bind failures SHALL be logged at WARN level with the address and the underlying error, and the server SHALL continue attempting the remaining addresses. Successful bind attempts SHALL be logged at INFO level with the address.

The server SHALL return a fatal error and fail to start only when every address in the resolved listen-address set has failed to bind. The fatal error SHALL include the count of attempted addresses.

When the WARN is caused by `EADDRINUSE` on a loopback address in `127.0.0.0/8` (typical of systemd-resolved's stub listener occupying `127.0.0.53` or `127.0.0.54`), the WARN SHALL include a hint stating the likely cause and that `DNSStubListener=no` in `resolved.conf` removes the conflict.

#### Scenario: One address fails, others succeed

- **WHEN** the resolved listen-address set is `{10.0.0.1:53, 127.0.0.53:53}` AND `127.0.0.53:53` is already in use by another process
- **THEN** the server logs WARN for `127.0.0.53:53` with the systemd-resolved hint AND logs INFO for `10.0.0.1:53` AND starts successfully serving queries on `10.0.0.1:53`

#### Scenario: TCP bind fails after UDP succeeds for the same address

- **WHEN** the resolved listen-address set contains `10.0.0.1:53` AND the UDP bind on `10.0.0.1:53` succeeds AND the TCP bind on `10.0.0.1:53` subsequently fails
- **THEN** the server closes the already-open UDP socket on `10.0.0.1:53` AND logs WARN for `10.0.0.1:53` AND treats the address as failed

#### Scenario: All addresses fail to bind

- **WHEN** every address in the resolved listen-address set fails to bind
- **THEN** the server returns a fatal error including the number of attempted addresses AND the process exits with a non-zero status


<!-- @trace
source: honor-listen-on
updated: 2026-04-15
code:
  - internal/server/server.go
  - internal/server/listenaddr.go
  - internal/server/listener.go
  - cmd/shadowdns/main.go
  - README.md
  - docs/migration.md
tests:
  - internal/server/addrset_test.go
  - internal/server/listenaddr_test.go
  - cmd/shadowdns/listenon_test.go
  - test/integration/listenon_test.go
  - internal/server/bindmany_test.go
-->

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

---
### Requirement: Synthesize CNAME response when qtype does not match but CNAME exists at the queried name

When the dns-server looks up records for a queried name and the requested qtype is not CNAME, but a CNAME record exists at that name, the dns-server SHALL return the CNAME record in the answer section with `RCODE=NOERROR` and `AA=1`, per RFC 1034 §3.6.2.

When the CNAME target is within the same zone (in-bailiwick — the target FQDN equals the zone origin or has the zone origin as a suffix), the dns-server SHALL restart the query at the CNAME target per RFC 1034 §3.6.2: look up records for `(target, original qtype)` in the same zone and append the results to the answer section after the CNAME record. This in-zone CNAME following is NOT recursion; it uses only local zone data.

When the CNAME target results in another CNAME (CNAME chain), the dns-server SHALL continue following the chain as long as each intermediate target remains in-bailiwick. The dns-server SHALL stop following when:
1. A non-CNAME record is found at the current target (success — append to answer).
2. The current target is out-of-bailiwick (stop — return collected records so far).
3. No record of any type exists at the current target (stop — return collected CNAME chain).
4. The chain depth reaches 8 (stop — return collected CNAME chain to prevent infinite loops from circular zone configurations).

When the CNAME target is out-of-bailiwick (not within the same zone), the dns-server SHALL return only the CNAME record without following, as resolving external names requires recursion which the server does not perform.

This behavior SHALL apply to both root zone queries and backup (alias) zone queries. For backup zone queries, the CNAME record's owner name in the response SHALL use the backup-namespace qname (not the rewritten root-namespace name). In-zone CNAME following for backup zone queries SHALL operate on the root zone's data (since that is where the records originate), and all returned records SHALL have their owner names and in-bailiwick RDATA rewritten to the backup namespace.

This behavior SHALL also apply when the initial CNAME is found via wildcard matching. After synthesizing the wildcard CNAME with the original qname as owner, the dns-server SHALL follow the CNAME target using the same in-zone rules described above.

When qtype is explicitly CNAME, the existing exact-match lookup behavior SHALL continue to apply unchanged — no CNAME following is performed.

When both a CNAME record and other record types coexist at the same name (a configuration error per RFC 1034 §3.6.2, but possible in zone files), the CNAME SHALL take precedence for non-CNAME queries: the server SHALL return the CNAME rather than NODATA.

#### Scenario: A query at a CNAME name with in-zone target returns CNAME plus target records

- **WHEN** a client queries `alias.root.com. A` AND the zone contains `alias.root.com. CNAME target.root.com.` AND `target.root.com. A 1.2.3.4`
- **THEN** the response has `RCODE=NOERROR`, `AA=1`, and the answer section contains both `alias.root.com. CNAME target.root.com.` and `target.root.com. A 1.2.3.4` in that order

#### Scenario: A query at a CNAME name with out-of-bailiwick target returns only the CNAME

- **WHEN** a client queries `alias.root.com. A` AND the zone contains `alias.root.com. CNAME target.other.com.` AND `other.com.` is not a loaded zone
- **THEN** the response has `RCODE=NOERROR`, `AA=1`, and the answer section contains only `alias.root.com. CNAME target.other.com.`

#### Scenario: CNAME chain within the same zone is fully followed

- **WHEN** a client queries `a.root.com. A` AND the zone contains `a.root.com. CNAME b.root.com.` AND `b.root.com. CNAME c.root.com.` AND `c.root.com. A 5.6.7.8`
- **THEN** the response answer section contains `a.root.com. CNAME b.root.com.`, `b.root.com. CNAME c.root.com.`, and `c.root.com. A 5.6.7.8` in that order

#### Scenario: CNAME chain stops at out-of-bailiwick target

- **WHEN** a client queries `a.root.com. A` AND the zone contains `a.root.com. CNAME b.root.com.` AND `b.root.com. CNAME external.other.com.`
- **THEN** the response answer section contains `a.root.com. CNAME b.root.com.` and `b.root.com. CNAME external.other.com.` (no A record, as the external target cannot be resolved locally)

#### Scenario: CNAME chain is truncated at depth 8

- **WHEN** a client queries `c1.root.com. A` AND the zone contains a circular CNAME chain `c1 → c2 → c3 → ... → c8 → c9` (9 CNAMEs)
- **THEN** the response answer section contains the first 8 CNAME records and stops (no A record)

#### Scenario: AAAA query at a CNAME name with in-zone target returns CNAME plus AAAA

- **WHEN** a client queries `alias.root.com. AAAA` AND the zone contains `alias.root.com. CNAME target.root.com.` AND `target.root.com. AAAA 2001:db8::1`
- **THEN** the response answer section contains `alias.root.com. CNAME target.root.com.` and `target.root.com. AAAA 2001:db8::1`

#### Scenario: In-zone CNAME target has no records of requested type returns CNAME chain only

- **WHEN** a client queries `alias.root.com. AAAA` AND the zone contains `alias.root.com. CNAME target.root.com.` AND `target.root.com.` has an A record but no AAAA record
- **THEN** the response has `RCODE=NOERROR`, and the answer section contains only `alias.root.com. CNAME target.root.com.`

#### Scenario: Explicit CNAME query does not follow the target

- **WHEN** a client queries `alias.root.com. CNAME` AND the zone contains `alias.root.com. CNAME target.root.com.` AND `target.root.com. A 1.2.3.4`
- **THEN** the response answer section contains only `alias.root.com. CNAME target.root.com.` (no A record appended)

#### Scenario: Backup zone CNAME with in-zone target returns rewritten CNAME plus target records

- **WHEN** `backup.com.` is a backup of `root.com.` AND the root zone contains `sub.root.com. CNAME target.root.com.` AND `target.root.com. A 1.2.3.4` AND a client queries `sub.backup.com. A`
- **THEN** the response answer section contains `sub.backup.com. CNAME target.backup.com.` and `target.backup.com. A 1.2.3.4` (owner names and in-bailiwick CNAME RDATA rewritten to backup namespace)

#### Scenario: Backup zone CNAME with out-of-bailiwick target returns only CNAME

- **WHEN** `backup.com.` is a backup of `root.com.` AND the root zone contains `sub.root.com. CNAME target.other.com.` AND a client queries `sub.backup.com. A`
- **THEN** the response answer section contains only `sub.backup.com. CNAME target.other.com.` (owner is rewritten to backup namespace, out-of-bailiwick target preserved)

#### Scenario: Wildcard CNAME with in-zone target is followed

- **WHEN** the zone contains `*.root.com. CNAME service.root.com.` AND `service.root.com. A 10.0.0.1` AND a client queries `foo.root.com. A`
- **THEN** the response answer section contains `foo.root.com. CNAME service.root.com.` and `service.root.com. A 10.0.0.1`

#### Scenario: Name with no records and no CNAME returns NXDOMAIN or NODATA as before

- **WHEN** a client queries `missing.root.com. A` AND no records of any type exist at `missing.root.com.`
- **THEN** the response has `RCODE=NXDOMAIN` with the zone SOA in the authority section (unchanged behavior)

#### Scenario: Name with A record but no AAAA and no CNAME returns NODATA as before

- **WHEN** a client queries `www.root.com. AAAA` AND `www.root.com.` has an A record but no AAAA record and no CNAME record
- **THEN** the response has `RCODE=NOERROR`, empty answer section, and the zone SOA in the authority section (unchanged behavior)


<!-- @trace
source: in-zone-cname-following
updated: 2026-04-16
code:
  - testdata/integration/master/example.com_view-th.fwd
  - internal/server/handler.go
  - internal/alias/override.go
  - testdata/integration/master/example.com_view-other.fwd
tests:
  - internal/alias/override_test.go
  - test/integration/cname_following_test.go
  - internal/server/server_test.go
  - test/integration/cname_synthesis_test.go
  - test/integration/wildcard_test.go
-->

---
### Requirement: Match wildcard records per RFC 4592 when exact lookup fails

When a query name falls within a loaded zone and exact lookup produces no records, the dns-server SHALL attempt wildcard matching per RFC 4592. The wildcard matching algorithm SHALL:

1. Starting from the query name, strip the leftmost label to produce a parent name.
2. Check whether a wildcard owner `*.<parent>` exists in the zone. If it does, use those records as the match.
3. If no wildcard owner exists at this level, check whether `<parent>` itself exists in the zone's Records (as an empty non-terminal). If it does, stop — the wildcard MUST NOT match (RFC 4592 §2.2.1 ENT blocking rule).
4. If `<parent>` does not exist, strip the next leftmost label and repeat from step 2.
5. Stop when parent equals the zone origin. If no wildcard is found, fall through to NXDOMAIN or NODATA as before.

When a wildcard match is found, the dns-server SHALL synthesize the response with the original query name as the owner name in the answer section (not the `*` label), per RFC 4592 §2.2.

This behavior SHALL apply to both root zone queries and backup (alias) zone queries. For backup zone queries, the wildcard lookup SHALL operate on the root zone's records (after qname rewrite to root namespace), and the synthesized response SHALL use the backup-namespace qname as the owner name.

#### Scenario: Single-level wildcard matches subdomain query

- **WHEN** the zone contains `*.example.com. A 1.2.3.4` AND a client queries `foo.example.com. A`
- **THEN** the response has `RCODE=NOERROR`, `AA=1`, and the answer section contains `foo.example.com. A 1.2.3.4`

#### Scenario: Multi-level subdomain matches wildcard at closest encloser

- **WHEN** the zone contains `*.example.com. A 1.2.3.4` AND no records exist at `bar.example.com.` AND a client queries `foo.bar.example.com. A`
- **THEN** the response has `RCODE=NOERROR`, `AA=1`, and the answer section contains `foo.bar.example.com. A 1.2.3.4`

#### Scenario: Empty non-terminal blocks wildcard matching

- **WHEN** the zone contains `*.example.com. A 1.2.3.4` AND `sub.example.com. TXT "exists"` AND a client queries `other.sub.example.com. A`
- **THEN** the response has `RCODE=NXDOMAIN` because `sub.example.com.` is an ENT that blocks the wildcard

#### Scenario: More-specific wildcard takes precedence

- **WHEN** the zone contains `*.example.com. A 1.1.1.1` AND `*.sub.example.com. A 2.2.2.2` AND a client queries `foo.sub.example.com. A`
- **THEN** the response answer section contains `foo.sub.example.com. A 2.2.2.2` (the more-specific wildcard wins)

#### Scenario: Exact record takes precedence over wildcard

- **WHEN** the zone contains `*.example.com. A 1.1.1.1` AND `www.example.com. A 3.3.3.3` AND a client queries `www.example.com. A`
- **THEN** the response answer section contains `www.example.com. A 3.3.3.3` (exact match, wildcard not consulted)

#### Scenario: Wildcard CNAME is returned for non-CNAME query type

- **WHEN** the zone contains `*.example.com. CNAME target.other.com.` AND a client queries `foo.example.com. A` AND CNAME synthesis is active
- **THEN** the response answer section contains `foo.example.com. CNAME target.other.com.`

#### Scenario: Backup zone wildcard uses root zone wildcard with owner rewrite

- **WHEN** `backup.com.` is a backup of `root.com.` AND the root zone contains `*.root.com. A 1.2.3.4` AND a client queries `foo.backup.com. A`
- **THEN** the response answer section contains `foo.backup.com. A 1.2.3.4`

#### Scenario: No wildcard found falls through to NXDOMAIN

- **WHEN** the zone contains no wildcard records AND a client queries `missing.example.com. A` AND no records exist at that name
- **THEN** the response has `RCODE=NXDOMAIN` with the zone SOA in the authority section (unchanged behavior)

#### Scenario: Wildcard match with no records of requested type returns NODATA

- **WHEN** the zone contains `*.example.com. A 1.2.3.4` AND a client queries `foo.example.com. AAAA` AND no CNAME exists at the wildcard
- **THEN** the response has `RCODE=NOERROR`, empty answer section, and the zone SOA in the authority section

<!-- @trace
source: wildcard-support
updated: 2026-04-16
code:
  - internal/server/handler.go
  - README.md
  - internal/zone/zone.go
  - internal/alias/override.go
  - testdata/integration/master/example.com_view-other.fwd
  - testdata/integration/master/example.com_view-th.fwd
tests:
  - internal/alias/override_test.go
  - test/integration/wildcard_test.go
  - test/integration/cname_synthesis_test.go
  - internal/zone/parser_test.go
  - test/integration/negative_test.go
  - internal/zone/zone_test.go
  - internal/server/server_test.go
-->