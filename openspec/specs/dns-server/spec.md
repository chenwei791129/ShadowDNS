## ADDED Requirements

### Requirement: Listen for DNS queries on UDP and TCP port 53

The dns-server SHALL bind both UDP and TCP listeners on the configured address (default `0.0.0.0:53`) and serve DNS queries on both transports. The TCP listener SHALL remain required even when zone transfer is disabled, because TCP is the RFC 7766 fallback for responses larger than the UDP payload limit.

#### Scenario: UDP query receives response

- **WHEN** a client sends a valid DNS query over UDP on port 53
- **THEN** the server responds over UDP within the same 5-tuple

#### Scenario: TCP query receives response

- **WHEN** a client sends a valid DNS query over TCP on port 53
- **THEN** the server accepts the connection, reads the 2-byte length prefix, and writes a length-prefixed response

#### Scenario: UDP response size SHALL NOT exceed the advertised EDNS0 buffer

- **WHEN** a client sends a UDP query with an EDNS0 OPT record advertising a buffer size of N bytes
- **THEN** the server's UDP response SHALL have a wire size (as produced by `dns.Msg.Pack()`) less than or equal to N bytes
- **AND** when the untruncated response would exceed N bytes, the server SHALL drop trailing Answer-section RRs and set the TC (truncated) flag until the packed response fits within N bytes

##### Example: Client advertises 4096, answer set would serialize to 6000 bytes

- **GIVEN** a DNS query with EDNS0 UDPSize=4096 asking for TXT at an FQDN with enough RRs to serialize to 6000 bytes after compression
- **WHEN** the server builds the reply
- **THEN** the server SHALL pack the reply, observe 6000 > 4096, drop trailing Answer RRs one at a time and re-pack until the packed size ≤ 4096 bytes, and set TC=1 in the final packet

#### Scenario: UDP response without EDNS0 falls back to 512-byte budget

- **WHEN** a client sends a UDP query with no EDNS0 OPT record
- **THEN** the server's UDP response SHALL have a packed wire size ≤ 512 bytes (RFC 1035 §2.3.4) and SHALL set TC=1 when RRs are dropped to meet that limit


<!-- @trace
source: fix-oversized-udp-responses
updated: 2026-04-25
code:
  - internal/server/handler.go
tests:
  - test/integration/stress_shared_bucket_test.go
  - internal/server/handler_test.go
  - test/integration/compression_budget_test.go
-->

### Requirement: Successful answer responses SHALL use DNS name compression

The dns-server SHALL produce successful authoritative answer responses with DNS name compression enabled per RFC 1035 §4.1.4. This applies to both UDP and TCP transports. `dns.Msg.Compress` SHALL be set to `true` before the message is packed or written to the transport.

#### Scenario: Reply with multiple RRs sharing an owner name uses compression pointers

- **GIVEN** a query answered with two or more RRs at the same owner name (for example ephemeral TXT RRset at `_acme-challenge.<zone>`)
- **WHEN** the server serializes the reply
- **THEN** the second and subsequent occurrences of the owner name in the wire format SHALL be encoded as 2-byte compression pointers, not full labels

##### Example: 48 TXT RRs at `_acme-challenge.example.com.` with 43-byte values

- **GIVEN** 48 TXT RRs sharing owner name `_acme-challenge.example.com.`, each carrying a 43-byte base64url challenge value, TTL 0
- **WHEN** the server packs an authoritative reply
- **THEN** the packed wire size SHALL be under 3000 bytes (compressed), not around 4000 bytes (uncompressed), enabling fit within a 4096-byte EDNS0 UDP buffer without truncation


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


<!-- @trace
source: ephemeral-txt-api
updated: 2026-04-22
code:
  - docs/ephemeral-api.md
  - go.sum
  - .release-please-manifest.json
  - cmd/shadowdns/main.go
  - internal/transfer/notify.go
  - internal/config/zones.go
  - Makefile
  - scripts/smoke.sh
  - internal/ephemeral/store.go
  - go.mod
  - docs/benchmark.md
  - scripts/gen-container-testdata.go
  - testdata/integration/master/example.com_view-other.fwd
  - internal/server/server.go
  - internal/server/listener.go
  - cmd/shadowdns/pprof.go
  - internal/view/loader.go
  - internal/shadowdnscfg/config.go
  - internal/zone/parser.go
  - internal/server/handler.go
  - internal/alias/override.go
  - .github/workflows/release-please.yml
  - CLAUDE.md
  - internal/server/listenaddr.go
  - internal/zone/classify.go
  - CHANGELOG.md
  - testdata/integration/master/example.com_view-th.fwd
  - cmd/shadowdns/reload.go
  - internal/transfer/axfr.go
  - internal/zone/zone.go
  - internal/config/options.go
  - packaging/shadowdns.service
  - internal/api/server.go
  - packaging/shadowdns.yaml.example
  - packaging/aliases.yaml.example
  - packaging/named.conf.example
  - internal/server/build.go
  - internal/config/aliases.go
  - scripts/test-deb.sh
  - nfpm.yaml
  - internal/server/fingerprint.go
  - internal/logging/logger.go
  - docs/migration.md
  - README.md
tests:
  - cmd/shadowdns/main_ephemeral_test.go
  - test/integration/notify_test.go
  - internal/server/server_test.go
  - test/integration/negative_test.go
  - internal/transfer/axfr_test.go
  - internal/ephemeral/store_test.go
  - internal/zone/classify_test.go
  - internal/zone/parser_test.go
  - internal/config/aliases_test.go
  - cmd/shadowdns/listenon_test.go
  - cmd/shadowdns/pprof_test.go
  - cmd/shadowdns/main_test.go
  - internal/api/server_test.go
  - internal/config/zones_test.go
  - internal/server/fingerprint_test.go
  - test/integration/axfr_test.go
  - internal/logging/logger_test.go
  - test/integration/helpers_test.go
  - internal/view/loader_test.go
  - test/integration/reload_diff_test.go
  - test/integration/cname_following_test.go
  - internal/shadowdnscfg/config_test.go
  - internal/alias/override_test.go
  - internal/server/handler_ephemeral_test.go
  - internal/zone/zone_test.go
  - internal/transfer/notify_test.go
  - internal/server/listenaddr_test.go
  - internal/server/build_test.go
  - internal/config/options_test.go
  - test/integration/listenon_test.go
  - test/integration/wildcard_test.go
  - test/integration/cname_synthesis_test.go
-->


<!-- @trace
source: ephemeral-txt-overrides-cname
updated: 2026-04-22
code:
  - internal/config/aliases.go
  - README.md
  - scripts/test-deb.sh
  - internal/shadowdnscfg/config.go
  - testdata/integration/shadowdns.yaml
  - CHANGELOG.md
  - docs/ephemeral-api.md
  - testdata/integration/master/example.com_view-th.fwd
  - scripts/gen-container-testdata.go
  - internal/server/build.go
  - internal/alias/override.go
  - .release-please-manifest.json
  - internal/server/handler.go
  - packaging/shadowdns.yaml.example
  - scripts/smoke.sh
  - docs/benchmark.md
  - testdata/integration/aliases.yaml
  - testdata/integration/master/example.com_view-other.fwd
  - testdata/integration/README.md
tests:
  - internal/server/handler_ephemeral_test.go
  - test/integration/cname_following_test.go
  - internal/shadowdnscfg/config_test.go
  - internal/alias/override_test.go
  - test/integration/helpers_test.go
  - test/integration/axfr_test.go
  - test/integration/listenon_test.go
  - internal/config/aliases_test.go
  - test/integration/ephemeral_overrides_cname_test.go
  - test/integration/reload_diff_test.go
  - cmd/shadowdns/main_ephemeral_test.go
-->


<!-- @trace
source: ephemeral-fixed-response-ttl
updated: 2026-04-24
code:
  - internal/ephemeral/store.go
  - internal/server/handler.go
tests:
  - test/integration/ephemeral_overrides_cname_test.go
  - internal/server/handler_ephemeral_test.go
  - internal/ephemeral/store_test.go
-->


<!-- @trace
source: fix-oversized-udp-responses
updated: 2026-04-25
code:
  - internal/server/handler.go
tests:
  - test/integration/stress_shared_bucket_test.go
  - internal/server/handler_test.go
  - test/integration/compression_budget_test.go
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

#### Scenario: UDP response size SHALL NOT exceed the advertised EDNS0 buffer

- **WHEN** a client sends a UDP query with an EDNS0 OPT record advertising a buffer size of N bytes
- **THEN** the server's UDP response SHALL have a wire size (as produced by `dns.Msg.Pack()`) less than or equal to N bytes
- **AND** when the untruncated response would exceed N bytes, the server SHALL drop trailing Answer-section RRs and set the TC (truncated) flag until the packed response fits within N bytes

##### Example: Client advertises 4096, answer set would serialize to 6000 bytes

- **GIVEN** a DNS query with EDNS0 UDPSize=4096 asking for TXT at an FQDN with enough RRs to serialize to 6000 bytes after compression
- **WHEN** the server builds the reply
- **THEN** the server SHALL pack the reply, observe 6000 > 4096, drop trailing Answer RRs one at a time and re-pack until the packed size ≤ 4096 bytes, and set TC=1 in the final packet

#### Scenario: UDP response without EDNS0 falls back to 512-byte budget

- **WHEN** a client sends a UDP query with no EDNS0 OPT record
- **THEN** the server's UDP response SHALL have a packed wire size ≤ 512 bytes (RFC 1035 §2.3.4) and SHALL set TC=1 when RRs are dropped to meet that limit

---
### Requirement: Derive listen address set from named.conf listen-on

The dns-server SHALL derive its listen-address set at startup (and at every SIGHUP reload that reparses named.conf) using this precedence:

1. If the `--listen` CLI flag has a non-empty host component (e.g. `127.0.0.1:5353`, `10.0.0.1:53`, `[::1]:5353`), the listen-address set SHALL contain exactly the single address passed via `--listen`, and both `options.listen-on` and `options.listen-on-v6` from named.conf SHALL be ignored. The host MAY be an IPv4 literal or an IPv6 literal in bracket form. This preserves override semantics for tests and special deployments.
2. Otherwise (the host component is empty, e.g. `:53`, `:5353`, `:0`), the listen-address set SHALL be the union of the IPv4 addresses resolved from the `listen-on` token list and the IPv6 addresses resolved from the `listen-on-v6` token list, each address combined with the port component of `--listen` (default 53). The IPv4 addresses SHALL appear before the IPv6 addresses, each family preserving first-appearance order. IPv6 addresses SHALL be emitted in bracket form `[addr]:port`.
3. Within the IPv4 family of case 2: if `options.listen-on` is absent, the IPv4 set SHALL be resolved as if `listen-on { any; };` were specified. If `options.listen-on-v6` is absent, the IPv6 set SHALL be empty (IPv6 is opt-in; absence SHALL NOT imply `any`). This preserves IPv4-only behavior for deployments that do not declare `listen-on-v6`.

In all cases the port component from `--listen` (default 53) SHALL be applied consistently across both families: when `--listen` is `:5353`, every resolved IPv4 and IPv6 address SHALL use port 5353.

Token resolution rules for `listen-on` (IPv4 family):

- `any` SHALL expand to every IPv4 address reported by `net.InterfaceAddrs()` for interfaces that are up, excluding IPv6 addresses and excluding the link-local range `169.254.0.0/16`. Loopback addresses (`127.0.0.0/8`, including aliases like `127.0.0.53`) SHALL be included.
- `none` SHALL resolve to an empty set.
- Literal IPv4 addresses SHALL be used as-is.
- Unsupported tokens (IPv6 literals, exclusion syntax `!addr`, ACL names, `port N`, `interface` keyword) SHALL be logged at WARN level including the offending token, and the token SHALL be skipped. Parsing MUST NOT fail because of unsupported tokens.

Token resolution rules for `listen-on-v6` (IPv6 family):

- `any` SHALL expand to every IPv6 address reported by `net.InterfaceAddrs()` for interfaces that are up, excluding IPv4 addresses and excluding the link-local range `fe80::/10`. The loopback address `::1` SHALL be included.
- `none` SHALL resolve to an empty set.
- Literal IPv6 addresses SHALL be used as-is and emitted in bracket form.
- Unsupported tokens (IPv4 literals, exclusion syntax `!addr`, ACL names, `port N`, `interface` keyword) SHALL be logged at WARN level including the offending token, and the token SHALL be skipped. Parsing MUST NOT fail because of unsupported tokens.

When the resulting listen-address set (after unioning both families) is empty, the server SHALL exit with a fatal error explaining that no listeners would be started. When the empty result is caused by an explicit `none` in a family that was the only declared family, the fatal error SHALL distinguish that explicit cause from the "all tokens unsupported" cause for that family.

SIGHUP reload SHALL NOT rebind listeners even if the resolved listen-address set (IPv4 or IPv6) would change; operators needing a binding change MUST restart the process. The dns-server SHALL log an INFO message at reload time stating that listen-address changes require restart if the resolved set differs from the currently bound set; this comparison SHALL cover both IPv4 and IPv6 addresses.

#### Scenario: listen-on { any; } binds every IPv4 interface address

- **WHEN** named.conf sets `listen-on { any; };` AND the host has IPv4 addresses `10.0.0.1`, `127.0.0.1`, and `127.0.0.53`
- **THEN** the server attempts to bind UDP and TCP on `10.0.0.1:53`, `127.0.0.1:53`, and `127.0.0.53:53`

#### Scenario: listen-on with explicit addresses binds only those addresses

- **WHEN** named.conf sets `listen-on { 10.0.0.1; 192.168.1.1; };`
- **THEN** the server attempts to bind UDP and TCP on exactly `10.0.0.1:53` and `192.168.1.1:53` AND does not attempt any other address

#### Scenario: --listen override bypasses listen-on

- **WHEN** the process is started with `--listen 127.0.0.1:5353` AND named.conf sets `listen-on { any; };`
- **THEN** the server binds UDP and TCP only on `127.0.0.1:5353` AND does not enumerate interface addresses

#### Scenario: --listen with empty host does not bypass listen-on

- **WHEN** the process is started without `--listen` (or with any `:PORT` form that has no host, such as `:53` or `:0`) AND named.conf sets `listen-on { 10.0.0.1; };`
- **THEN** the server binds on `10.0.0.1:<port>` AND not on the wildcard `0.0.0.0:<port>`

#### Scenario: Port from --listen is applied to listen-on addresses

- **WHEN** the process is started with `--listen :5353` AND named.conf sets `listen-on { 10.0.0.1; };`
- **THEN** the server binds on `10.0.0.1:5353`

#### Scenario: Unsupported listen-on token is skipped with a warning

- **WHEN** named.conf sets `listen-on { !10.0.0.1; any; };`
- **THEN** the server logs a WARN message naming the unsupported token `!10.0.0.1` AND proceeds to resolve `any`

#### Scenario: listen-on { none; } with no other family causes fatal error

- **WHEN** named.conf sets `listen-on { none; };` AND `listen-on-v6` is absent AND `--listen` is at its default value
- **THEN** the server exits with a fatal error indicating no listeners would be started

#### Scenario: listen-on-v6 absent yields IPv4-only behavior

- **WHEN** named.conf sets `listen-on { 10.0.0.1; };` AND `listen-on-v6` is absent AND `--listen` is `:53`
- **THEN** the resolved listen-address set is exactly `{10.0.0.1:53}` AND no IPv6 listener is attempted

#### Scenario: listen-on-v6 { none; } yields IPv4-only behavior

- **WHEN** named.conf sets `listen-on { 10.0.0.1; };` AND `listen-on-v6 { none; };` AND `--listen` is `:53`
- **THEN** the resolved listen-address set is exactly `{10.0.0.1:53}` AND the server starts successfully

#### Scenario: listen-on-v6 with explicit addresses binds those IPv6 addresses

- **WHEN** named.conf sets `listen-on-v6 { 2001:db8::1; };` AND `listen-on { 10.0.0.1; };` AND `--listen` is `:53`
- **THEN** the resolved listen-address set is `{10.0.0.1:53, [2001:db8::1]:53}` with the IPv4 address ordered before the IPv6 address

#### Scenario: listen-on-v6 { any; } enumerates IPv6 interfaces excluding link-local

- **WHEN** named.conf sets `listen-on-v6 { any; };` AND the host has IPv6 addresses `2001:db8::1`, `fe80::1`, and `::1`
- **THEN** the server attempts to bind UDP and TCP on `[2001:db8::1]:53` and `[::1]:53` AND does not attempt `[fe80::1]:53`

##### Example: any expansion mixing both families

- **GIVEN** interface addresses: `10.0.0.1`, `169.254.1.1`, `2001:db8::1`, `fe80::1`, `::1`
- **WHEN** named.conf sets `listen-on { any; };` AND `listen-on-v6 { any; };` AND `--listen` is `:53`
- **THEN** the resolved set is `{10.0.0.1:53, [2001:db8::1]:53, [::1]:53}` (IPv4 link-local `169.254.1.1` and IPv6 link-local `fe80::1` are excluded; both loopbacks retained; IPv4 ordered first)

#### Scenario: --listen with IPv6 literal host binds only that address

- **WHEN** the process is started with `--listen [::1]:5353` AND named.conf sets `listen-on { any; };` AND `listen-on-v6 { any; };`
- **THEN** the server binds UDP and TCP only on `[::1]:5353` AND does not enumerate interface addresses

#### Scenario: IPv4 literal in listen-on-v6 is skipped with a warning

- **WHEN** named.conf sets `listen-on-v6 { 10.0.0.1; 2001:db8::1; };`
- **THEN** the server logs a WARN message naming the unsupported token `10.0.0.1` AND resolves the IPv6 set to `{[2001:db8::1]:53}`

#### Scenario: SIGHUP reload does not rebind listeners

- **WHEN** the server is running with listeners bound for `listen-on { 10.0.0.1; };` AND named.conf is edited to `listen-on { 10.0.0.2; };` AND SIGHUP is sent
- **THEN** the server continues serving on `10.0.0.1:53` only AND logs an INFO message stating that listen-address changes require restart

#### Scenario: SIGHUP reload drift detection covers IPv6

- **WHEN** the server is running with listeners bound for `listen-on-v6 { 2001:db8::1; };` AND named.conf is edited to `listen-on-v6 { 2001:db8::2; };` AND SIGHUP is sent
- **THEN** the server continues serving on `[2001:db8::1]:53` only AND logs an INFO message stating that listen-address changes require restart


<!-- @trace
source: add-ipv6-listener
updated: 2026-06-09
code:
  - README.md
  - internal/server/listener.go
  - scripts/smoke.sh
  - internal/server/listenaddr.go
  - docs/migration.md
  - cmd/shadowdns/main.go
tests:
  - internal/server/bindmany_test.go
  - cmd/shadowdns/listenon_test.go
  - internal/server/listenaddr_test.go
  - test/integration/listenon_test.go
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

When both a CNAME record and other record types coexist at the same name (a configuration that violates RFC 1034 §3.6.2 but is permitted by some authoritative providers including Cloudflare, and is possible in zone files), the dns-server SHALL silently accept the zone — no load-time error, rejection, or warning is emitted. At query time, the dns-server SHALL apply exact-match-first resolution: if the zone contains a record matching the queried `(name, qtype)` exactly (with `qtype != CNAME`), the dns-server SHALL return that exact-match record set and SHALL NOT emit the coexisting CNAME or follow its target. CNAME synthesis SHALL trigger only when no exact-match record exists at the queried name for the queried qtype. This applies uniformly at any owner name including the zone apex.

**Exception — ephemeral TXT overlay**: when `qtype == TXT` AND an ephemeral record store is attached AND the store contains at least one live (unexpired) TXT entry at the queried name, the ephemeral TXT overlay defined in the "Listen for DNS queries on UDP and TCP port 53" Requirement SHALL take precedence over this CNAME synthesis behavior for that specific response. The CNAME SHALL NOT be emitted and the CNAME target SHALL NOT be followed. This exception is intentionally scoped to TXT qtype and live ephemeral entries; all other qtypes and the absence of a live ephemeral entry cause the standard CNAME synthesis behavior to apply unchanged.

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
- **THEN** the response answer section contains `foo.root.com. CNAME service.root.com.` (wildcard synthesized with the query owner) and `service.root.com. A 10.0.0.1`

#### Scenario: Ephemeral TXT overlay takes precedence over CNAME synthesis for TXT queries

- **WHEN** a client queries `_acme-challenge.foo.root.com. TXT` AND the zone contains `_acme-challenge.foo.root.com. CNAME acme-dns.external.net.` AND the ephemeral store holds a live TXT entry for `_acme-challenge.foo.root.com.`
- **THEN** the response answer section contains only the ephemeral TXT RR(s); the CNAME is not emitted and the CNAME target is not followed

#### Scenario: Static zone record at the same owner as a CNAME wins over CNAME synthesis (Cloudflare-style coexistence)

- **WHEN** the zone apex `root.com.` contains `root.com. CNAME target.root.com.` AND `root.com. TXT "v=spf1 -all"` AND `root.com. A 192.0.2.10` AND `target.root.com. A 192.0.2.99` (a Cloudflare-style coexistence configuration that BIND9 would reject at zone load) AND the zone is loaded successfully without error or warning
- **THEN** a TXT query at `root.com.` returns only the static TXT record `"v=spf1 -all"` (the apex CNAME is not emitted and `target.root.com.` is not followed) AND a CNAME query at `root.com.` returns only the apex CNAME `root.com. CNAME target.root.com.` AND an A query at `root.com.` returns only the static apex A `root.com. A 192.0.2.10` (the apex CNAME is not followed because the apex has its own A record; CNAME flattening is not performed)

##### Example: Cloudflare-style apex coexistence resolution table

| Query type at apex | Records at apex | Expected answer | Notes |
| ------------------ | --------------- | --------------- | ----- |
| TXT | CNAME + TXT + A | the static TXT record(s) | exact-match wins over CNAME synthesis |
| CNAME | CNAME + TXT + A | the apex CNAME record | explicit CNAME query, no following |
| A | CNAME + TXT + A | the static apex A record | exact-match wins; CNAME not followed |
| AAAA | CNAME + TXT + A (no AAAA) | apex CNAME + target's AAAA chain | no exact AAAA → CNAME synthesis triggers |


<!-- @trace
source: apex-cname-txt-coexist
updated: 2026-05-05
code:
  - testdata/integration/master/example.com_view-other.fwd
  - testdata/integration/master/example.com_view-th.fwd
tests:
  - test/integration/query_test.go
-->

---
### Requirement: Match wildcard records per RFC 4592 when exact lookup fails

When a query name falls within a loaded zone and exact lookup produces no records, the dns-server SHALL attempt wildcard matching per RFC 4592. For the purposes of this requirement, "exact lookup" SHALL include the ephemeral TXT record store: when the query type is TXT and the ephemeral store holds one or more unexpired entries under the exact query name, those entries SHALL be treated as an exact match and SHALL be returned in preference to any wildcard synthesis. The wildcard matching algorithm SHALL:

1. Starting from the query name, strip the leftmost label to produce a parent name.
2. Check whether a wildcard owner `*.<parent>` exists in the zone. If it does, use those records as the match.
3. If no wildcard owner exists at this level, check whether `<parent>` itself exists in the zone's Records (as an empty non-terminal). If it does, stop — the wildcard MUST NOT match (RFC 4592 §2.2.1 ENT blocking rule).
4. If `<parent>` does not exist, strip the next leftmost label and repeat from step 2.
5. Stop when parent equals the zone origin. If no wildcard is found, fall through to NXDOMAIN or NODATA as before.

When a wildcard match is found, the dns-server SHALL synthesize the response with the original query name as the owner name in the answer section (not the `*` label), per RFC 4592 §2.2.

This behavior SHALL apply to both root zone queries and backup (alias) zone queries. For backup zone queries, the wildcard lookup SHALL operate on the root zone's records (after qname rewrite to root namespace), and the synthesized response SHALL use the backup-namespace qname as the owner name. The ephemeral exact-match check for backup zone queries SHALL use the backup-namespace qname (the name the client actually sent) because API callers PUT entries under the backup-namespace qname.

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

#### Scenario: Ephemeral TXT at exact qname takes precedence over zone wildcard TXT

- **WHEN** the zone contains `*.example.com. TXT "wild-value"` AND the ephemeral store holds a TXT entry with value `ephemeral-value` under `foo.example.com.` AND a client queries `foo.example.com. TXT`
- **THEN** the response answer section contains exactly one TXT record, `foo.example.com. TXT "ephemeral-value"`, and SHALL NOT contain `"wild-value"`

#### Scenario: Ephemeral TXT at exact qname takes precedence over zone wildcard CNAME

- **WHEN** the zone contains `*.example.com. CNAME target.other.com.` AND the ephemeral store holds a TXT entry with value `token` under `_acme-challenge.foo.example.com.` AND a client queries `_acme-challenge.foo.example.com. TXT`
- **THEN** the response answer section contains exactly one TXT record, `_acme-challenge.foo.example.com. TXT "token"`, and SHALL NOT contain a synthesized CNAME to `target.other.com.`

#### Scenario: Ephemeral TXT at exact backup-zone qname takes precedence over backup-derived wildcard

- **WHEN** `backup.com.` is a backup of `root.com.` AND the root zone contains `*.root.com. CNAME target.other.com.` AND the ephemeral store holds a TXT entry with value `backup-token` under `_acme-challenge.foo.backup.com.` AND a client queries `_acme-challenge.foo.backup.com. TXT`
- **THEN** the response answer section contains exactly one TXT record, `_acme-challenge.foo.backup.com. TXT "backup-token"`, and SHALL NOT contain a synthesized CNAME

#### Scenario: Zone wildcard still applies when ephemeral store has no exact match

- **WHEN** the zone contains `*.example.com. A 1.2.3.4` AND the ephemeral store holds no entry under `foo.example.com.` AND a client queries `foo.example.com. A`
- **THEN** the response answer section contains `foo.example.com. A 1.2.3.4` (wildcard unchanged when no exact ephemeral match exists)

#### Scenario: Ephemeral TXT does not suppress wildcard for non-TXT query types

- **WHEN** the zone contains `*.example.com. A 1.2.3.4` AND the ephemeral store holds a TXT entry under `foo.example.com.` AND a client queries `foo.example.com. A`
- **THEN** the response answer section contains `foo.example.com. A 1.2.3.4` (ephemeral TXT does not block wildcard for A queries)

<!-- @trace
source: exact-match-wins-over-wildcard
updated: 2026-04-22
code:
  - internal/api/server.go
  - internal/ephemeral/store.go
  - internal/alias/override.go
  - internal/server/handler.go
  - scripts/smoke.sh
  - docs/ephemeral-api.md
tests:
  - internal/server/server_test.go
  - cmd/shadowdns/main_ephemeral_test.go
  - internal/server/handler_ephemeral_test.go
  - internal/alias/override_test.go
  - internal/api/server_test.go
  - internal/ephemeral/store_test.go
-->

---
### Requirement: Successful answer responses SHALL use DNS name compression

The dns-server SHALL produce successful authoritative answer responses with DNS name compression enabled per RFC 1035 §4.1.4. This applies to both UDP and TCP transports. `dns.Msg.Compress` SHALL be set to `true` before the message is packed or written to the transport.

#### Scenario: Reply with multiple RRs sharing an owner name uses compression pointers

- **GIVEN** a query answered with two or more RRs at the same owner name (for example ephemeral TXT RRset at `_acme-challenge.<zone>`)
- **WHEN** the server serializes the reply
- **THEN** the second and subsequent occurrences of the owner name in the wire format SHALL be encoded as 2-byte compression pointers, not full labels

##### Example: 48 TXT RRs at `_acme-challenge.example.com.` with 43-byte values

- **GIVEN** 48 TXT RRs sharing owner name `_acme-challenge.example.com.`, each carrying a 43-byte base64url challenge value, TTL 0
- **WHEN** the server packs an authoritative reply
- **THEN** the packed wire size SHALL be under 3000 bytes (compressed), not around 4000 bytes (uncompressed), enabling fit within a 4096-byte EDNS0 UDP buffer without truncation

---
### Requirement: Preserve query case in the response Question section

The dns-server SHALL copy the Question section of the request into the response byte-for-byte, including the case of every label in the QNAME. The server SHALL NOT alter the case of QNAME bytes between request reception and response emission. This requirement enforces compatibility with DNS-0x20 case-randomization clients (Google Public DNS, Unbound `use-caps-for-id`, dnsmasq ≥2.91rc4) that drop responses whose Question section does not match the case of their query verbatim.

#### Scenario: Lowercase query echoed in lowercase

- **WHEN** a client sends a query for `www.example.com.`
- **THEN** the response's Question section contains `www.example.com.` byte-for-byte

#### Scenario: Mixed-case query echoed in mixed case

- **WHEN** a client sends a query for `WwW.eXaMpLe.CoM.`
- **THEN** the response's Question section contains `WwW.eXaMpLe.CoM.` byte-for-byte

#### Scenario: Uppercase query echoed in uppercase

- **WHEN** a client sends a query for `WWW.EXAMPLE.COM.`
- **THEN** the response's Question section contains `WWW.EXAMPLE.COM.` byte-for-byte


<!-- @trace
source: preserve-dns-name-case-in-responses
updated: 2026-04-29
code:
  - internal/transfer/axfr.go
  - internal/server/server.go
  - cmd/shadowdns/main.go
  - internal/zone/zone.go
  - internal/alias/rewrite.go
  - internal/ephemeral/store.go
  - internal/api/server.go
  - internal/server/build.go
  - internal/config/aliases.go
  - internal/shadowdnscfg/config.go
  - internal/zone/parser.go
  - internal/dnsutil/dnsutil.go
  - CHANGELOG.md
  - internal/alias/override.go
  - internal/server/handler.go
tests:
  - internal/zone/parser_test.go
  - cmd/shadowdns/main_test.go
  - internal/transfer/axfr_test.go
  - test/integration/case_preservation_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/zone/zone_test.go
  - internal/server/build_test.go
  - test/integration/reload_diff_test.go
  - internal/alias/rewrite_test.go
  - test/integration/listenon_test.go
  - internal/config/aliases_test.go
  - internal/server/handler_test.go
  - internal/shadowdnscfg/config_test.go
  - internal/alias/override_test.go
  - test/integration/axfr_test.go
  - test/integration/helpers_test.go
  - internal/server/server_test.go
  - internal/alias/rewrite_anywhere_test.go
-->

---
### Requirement: Responses echo an EDNS0 OPT record when the query carries one

When a query contains an EDNS0 OPT record, every query response path (successful answers, negative answers such as NXDOMAIN/NODATA, and error rcodes such as FORMERR, NOTIMP, REFUSED, SERVFAIL — including the panic-recovery SERVFAIL and error responses to refused transfer requests) SHALL include an OPT record in the Additional section with EDNS version 0 and a sender UDP payload size of 1232 bytes. When the query carries no OPT record, the response SHALL NOT contain an OPT record. OPT echo SHALL behave identically over UDP and TCP; the UDP truncation budget remains UDP-only and TCP responses are never truncated (current behavior, unchanged). AXFR/IXFR data-stream responses produced by the dedicated transfer subsystem are out of scope and keep their current behavior.

#### Scenario: EDNS query receives OPT echo

- **WHEN** a client sends a query with an EDNS0 OPT record advertising any buffer size
- **THEN** the response SHALL contain exactly one OPT record with version 0 and UDP payload size 1232

#### Scenario: Non-EDNS query receives no OPT

- **WHEN** a client sends a query without an EDNS0 OPT record
- **THEN** the response SHALL NOT contain an OPT record

#### Scenario: Error responses also carry OPT

- **WHEN** a client sends an EDNS0 query that results in a non-success rcode (e.g., REFUSED for CHAOS class)
- **THEN** the response SHALL still contain an OPT record with version 0

#### Scenario: Negative responses carry OPT

- **WHEN** a client sends an EDNS0 query for a non-existent name in a served zone (NXDOMAIN) or an existing name with no records of the queried type (NODATA)
- **THEN** the response SHALL contain an OPT record with version 0 alongside the SOA in the Authority section

#### Scenario: OPT echo over TCP

- **WHEN** a client sends the same EDNS0 query over TCP instead of UDP
- **THEN** the response SHALL contain the same OPT record (version 0, UDP payload size 1232) as the UDP response, and the response SHALL NOT be truncated regardless of its size


<!-- @trace
source: add-dns-cookies
updated: 2026-06-08
code:
  - internal/cookie/cookie.go
  - README.md
  - internal/server/server.go
  - go.sum
  - go.mod
  - internal/server/handler.go
  - NOTES.md
tests:
  - internal/server/handler_bench_test.go
  - internal/cookie/cookie_test.go
  - internal/server/handler_cookie_test.go
  - internal/server/handler_opt_test.go
  - internal/server/handler_test.go
-->

---
### Requirement: Unsupported EDNS version receives BADVERS

When a query carries an EDNS0 OPT record with a version greater than 0, the dns-server SHALL respond with the BADVERS extended rcode per RFC 6891 Section 6.1.3. The BADVERS response SHALL echo the question section, SHALL carry an OPT record with version 0, and SHALL NOT answer the question. The version check SHALL take precedence over all COOKIE option processing: a BADVERS response SHALL NOT contain a COOKIE option regardless of what the query carried.

#### Scenario: EDNS version 1 query

- **WHEN** a client sends a query with an EDNS0 OPT record of version 1
- **THEN** the response SHALL have the BADVERS extended rcode encoded in the OPT record, the OPT version field SHALL be 0, the question section SHALL be echoed, and the Answer section SHALL be empty

#### Scenario: BADVERS takes precedence over COOKIE processing

- **WHEN** a client sends a query with an EDNS0 OPT record of version 1 that also contains a malformed 7-byte COOKIE option
- **THEN** the response SHALL be BADVERS (not FORMERR) and SHALL NOT contain a COOKIE option


<!-- @trace
source: add-dns-cookies
updated: 2026-06-08
code:
  - internal/cookie/cookie.go
  - README.md
  - internal/server/server.go
  - go.sum
  - go.mod
  - internal/server/handler.go
  - NOTES.md
tests:
  - internal/server/handler_bench_test.go
  - internal/cookie/cookie_test.go
  - internal/server/handler_cookie_test.go
  - internal/server/handler_opt_test.go
  - internal/server/handler_test.go
-->

---
### Requirement: OPT record persists through UDP truncation and counts toward the size budget

The OPT record SHALL be included in the packed wire size measured against the UDP payload budget, and UDP truncation SHALL only drop Answer-section RRs — the OPT record SHALL never be removed to satisfy the budget. The truncated response SHALL set TC=1 and still carry the OPT record.

#### Scenario: Truncated EDNS response keeps its OPT record

- **WHEN** an EDNS0 UDP query produces a response whose packed size exceeds the advertised buffer size
- **THEN** the server SHALL drop trailing Answer RRs until the packed size including the OPT record fits the budget, SHALL set TC=1, and the final response SHALL still contain the OPT record

<!-- @trace
source: add-dns-cookies
updated: 2026-06-08
-->


<!-- @trace
source: add-dns-cookies
updated: 2026-06-08
code:
  - internal/cookie/cookie.go
  - README.md
  - internal/server/server.go
  - go.sum
  - go.mod
  - internal/server/handler.go
  - NOTES.md
tests:
  - internal/server/handler_bench_test.go
  - internal/cookie/cookie_test.go
  - internal/server/handler_cookie_test.go
  - internal/server/handler_opt_test.go
  - internal/server/handler_test.go
-->

---
### Requirement: Preserve owner-name case in answer, authority, and additional sections

The dns-server SHALL emit owner names in the Answer, Authority, and Additional sections using the case of the data source: zone-file case for records served from a root zone, alias-rewrite output case for records served from a backup zone (which combines query-case prefix and alias-config-case suffix per the alias-resolver capability), and zone-file case for SOA / NS records in the Authority section. The server SHALL NOT lowercase owner names during response assembly.

#### Scenario: Root-zone owner case preserved from zone file

- **WHEN** a root zone file contains `Service.Root.Com. IN A 1.2.3.4` and a client queries `service.root.com. A`
- **THEN** the response Answer section owner name is `Service.Root.Com.` (zone-file case)

#### Scenario: Wildcard-synthesized owner uses query case

- **WHEN** a root zone has `*.root.com. A 1.2.3.4` and a client queries `WWW.Root.Com. A`
- **THEN** the synthesized response Answer owner name is `WWW.Root.Com.` (query case, not lowercase)

#### Scenario: SOA in NXDOMAIN authority preserves case

- **WHEN** a query for `nonexistent.root.com.` returns NXDOMAIN and the SOA record in the zone file has owner `Root.Com.`
- **THEN** the Authority section SOA record has owner `Root.Com.`

<!-- @trace
source: preserve-dns-name-case-in-responses
updated: 2026-04-29
code:
  - internal/transfer/axfr.go
  - internal/server/server.go
  - cmd/shadowdns/main.go
  - internal/zone/zone.go
  - internal/alias/rewrite.go
  - internal/ephemeral/store.go
  - internal/api/server.go
  - internal/server/build.go
  - internal/config/aliases.go
  - internal/shadowdnscfg/config.go
  - internal/zone/parser.go
  - internal/dnsutil/dnsutil.go
  - CHANGELOG.md
  - internal/alias/override.go
  - internal/server/handler.go
tests:
  - internal/zone/parser_test.go
  - cmd/shadowdns/main_test.go
  - internal/transfer/axfr_test.go
  - test/integration/case_preservation_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/zone/zone_test.go
  - internal/server/build_test.go
  - test/integration/reload_diff_test.go
  - internal/alias/rewrite_test.go
  - test/integration/listenon_test.go
  - internal/config/aliases_test.go
  - internal/server/handler_test.go
  - internal/shadowdnscfg/config_test.go
  - internal/alias/override_test.go
  - test/integration/axfr_test.go
  - test/integration/helpers_test.go
  - internal/server/server_test.go
  - internal/alias/rewrite_anywhere_test.go
-->