## ADDED Requirements

### Requirement: Detect whether a query target is a backup zone

Given a fully-qualified query name, the alias-resolver SHALL identify the longest-suffix zone among the loaded zones (root zones and backup-override zones combined) and decide whether that zone is a backup zone by consulting the alias map.

#### Scenario: Query under backup zone is identified

- **WHEN** zones loaded are `root.com` (root) and `backup.com` (backup of `root.com`) AND the query name is `www.backup.com.`
- **THEN** the resolver reports matched zone `backup.com` AND classifies it as backup with root target `root.com`

#### Scenario: Query under root zone is identified

- **WHEN** the query name is `www.root.com.`
- **THEN** the resolver reports matched zone `root.com` AND classifies it as root

#### Scenario: Query matching no loaded zone returns no-match

- **WHEN** the query name is `www.unknown.com.` and no loaded zone is a suffix of it
- **THEN** the resolver reports no match AND the caller is responsible for producing REFUSED or delegating


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

### Requirement: Rewrite query name from backup to root before lookup

When the matched zone is a backup zone, the alias-resolver SHALL compute the rewritten query name by stripping the backup zone suffix from the original query name and appending the root zone suffix in its place. The comparison SHALL be case-insensitive on DNS labels per RFC 1035.

#### Scenario: Rewrite subdomain query

- **WHEN** backup zone is `backup.com.`, root zone is `root.com.`, and the query is `www.backup.com.`
- **THEN** the rewritten lookup name is `www.root.com.`

#### Scenario: Rewrite apex query

- **WHEN** the query is `backup.com.` (the zone apex)
- **THEN** the rewritten lookup name is `root.com.`

#### Scenario: Case-insensitive rewrite

- **WHEN** the query is `WWW.Backup.Com.`
- **THEN** the rewritten lookup name is `www.root.com.`


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

### Requirement: Rewrite owner names in the answer to the original backup zone

For each resource record returned from the root zone during a backup-zone query, the alias-resolver SHALL rewrite the record's owner name so that the response carries the original backup domain. The rule: if the record owner equals the root zone origin, replace it with the backup zone origin; if the owner has the root zone as a suffix, replace that suffix with the backup zone origin.

#### Scenario: Owner name at apex is rewritten

- **WHEN** a record returned from root has owner `root.com.` and the query was for `backup.com.`
- **THEN** the response record has owner `backup.com.`

#### Scenario: Owner name below apex is rewritten

- **WHEN** a record returned from root has owner `www.root.com.` and the query was under `backup.com.`
- **THEN** the response record has owner `www.backup.com.`


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

### Requirement: Apply in-bailiwick rewrite to record values

The alias-resolver SHALL rewrite DNS name values inside RDATA of record types `CNAME` (Target), `NS` (Ns), `MX` (Mx), `PTR` (Ptr), `SRV` (Target), and `SOA` (Ns, Mbox) only when the value equals the root zone origin or has the root zone origin as a suffix. Values that do not point into the root zone SHALL be preserved byte-for-byte. Record types `A`, `AAAA`, and `TXT` SHALL NOT have their RDATA modified.

#### Scenario: CNAME pointing within root zone is rewritten

- **WHEN** the root zone record is `blog.root.com. CNAME service.root.com.` and the query is under `backup.com.`
- **THEN** the response record is `blog.backup.com. CNAME service.backup.com.`

#### Scenario: CNAME pointing to a third party is preserved

- **WHEN** the root zone record is `app.root.com. CNAME abc.us-east-1.elb.amazonaws.com.` and the query is under `backup.com.`
- **THEN** the response record is `app.backup.com. CNAME abc.us-east-1.elb.amazonaws.com.`

#### Scenario: NS value within root zone is rewritten

- **WHEN** the root zone record is `root.com. NS ns1.root.com.` and the query is under `backup.com.`
- **THEN** the response record is `backup.com. NS ns1.backup.com.`

#### Scenario: NS value to external nameserver is preserved

- **WHEN** the root zone record is `root.com. NS ns1.externaldns.net.`
- **THEN** the response record value is `ns1.externaldns.net.` unchanged

#### Scenario: SOA MNAME and RNAME within root zone are rewritten

- **WHEN** the root zone SOA is `root.com. SOA ns1.root.com. root.ns1.root.com. (...)` and the query is for `backup.com. SOA`
- **THEN** the response is `backup.com. SOA ns1.backup.com. root.ns1.backup.com. (...)` with all numeric fields preserved byte-for-byte

#### Scenario: A and AAAA RDATA are never rewritten

- **WHEN** the root zone record is `ns1.root.com. A 1.2.3.4` and the query is `ns1.backup.com. A`
- **THEN** the response record is `ns1.backup.com. A 1.2.3.4`

#### Scenario: TXT RDATA is never rewritten

- **WHEN** the root zone record is `root.com. TXT "v=spf1 include:_spf.root.com ~all"` and the query is for `backup.com. TXT` with no override present
- **THEN** the response record is `backup.com. TXT "v=spf1 include:_spf.root.com ~all"` with the TXT string unchanged


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

### Requirement: Merge backup overrides for TXT, MX, and SRV

For a backup-zone query, the alias-resolver SHALL first consult the backup zone's override record set (loaded by zone-parser). If an override exists for the queried (owner, type) pair where type is TXT, MX, or SRV, the override records SHALL be returned in place of the root zone records for that (owner, type) pair. For all other record types and for (owner, type) pairs without an override, the alias-resolver SHALL return the rewritten root zone records.

#### Scenario: Override replaces inherited TXT

- **WHEN** `backup.com` override set contains `backup.com. TXT "google-site-verification=abc"` and root zone has `root.com. TXT "v=spf1 ..."`
- **THEN** querying `backup.com. TXT` returns only the override `"google-site-verification=abc"`

#### Scenario: Inherited MX when no override exists

- **WHEN** `backup.com` has no MX override and root has `root.com. MX 10 mx1.root.com.`
- **THEN** querying `backup.com. MX` returns `backup.com. MX 10 mx1.backup.com.` (inherited with in-bailiwick rewrite applied to the MX target)

#### Scenario: SRV override at same owner is returned

- **WHEN** `backup.com` override set contains `_sip._tcp.backup.com. SRV 10 5 5060 sip.example.com.`
- **THEN** querying `_sip._tcp.backup.com. SRV` returns that override record


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

### Requirement: Inherit SOA fields from root zone when serving backup SOA

When serving a query for the backup zone's SOA record, the alias-resolver SHALL construct the SOA response by copying the root zone's SOA serial, refresh, retry, expire, and minimum fields verbatim and applying the owner-name and in-bailiwick rewrite rules to the record owner, MNAME, and RNAME only.

#### Scenario: Serial is inherited verbatim

- **WHEN** root zone SOA has serial `4230120512` and the query is `backup.com. SOA`
- **THEN** the response SOA serial is `4230120512`

#### Scenario: Numeric timers are inherited verbatim

- **WHEN** root zone SOA has refresh=300, retry=120, expire=86400, minimum=3600
- **THEN** the response SOA has refresh=300, retry=120, expire=86400, minimum=3600

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

### Requirement: Detect whether a query target is a backup zone

Given a fully-qualified query name, the alias-resolver SHALL identify the longest-suffix zone among the loaded zones (root zones and backup-override zones combined) and decide whether that zone is a backup zone by consulting the alias map.

#### Scenario: Query under backup zone is identified

- **WHEN** zones loaded are `root.com` (root) and `backup.com` (backup of `root.com`) AND the query name is `www.backup.com.`
- **THEN** the resolver reports matched zone `backup.com` AND classifies it as backup with root target `root.com`

#### Scenario: Query under root zone is identified

- **WHEN** the query name is `www.root.com.`
- **THEN** the resolver reports matched zone `root.com` AND classifies it as root

#### Scenario: Query matching no loaded zone returns no-match

- **WHEN** the query name is `www.unknown.com.` and no loaded zone is a suffix of it
- **THEN** the resolver reports no match AND the caller is responsible for producing REFUSED or delegating

---
### Requirement: Rewrite query name from backup to root before lookup

When the matched zone is a backup zone, the alias-resolver SHALL compute the rewritten query name by stripping the backup zone suffix from the original query name and appending the root zone suffix in its place. The comparison SHALL be case-insensitive on DNS labels per RFC 1035.

#### Scenario: Rewrite subdomain query

- **WHEN** backup zone is `backup.com.`, root zone is `root.com.`, and the query is `www.backup.com.`
- **THEN** the rewritten lookup name is `www.root.com.`

#### Scenario: Rewrite apex query

- **WHEN** the query is `backup.com.` (the zone apex)
- **THEN** the rewritten lookup name is `root.com.`

#### Scenario: Case-insensitive rewrite

- **WHEN** the query is `WWW.Backup.Com.`
- **THEN** the rewritten lookup name is `www.root.com.`

---
### Requirement: Rewrite owner names in the answer to the original backup zone

For each resource record returned from the root zone during a backup-zone query, the alias-resolver SHALL rewrite the record's owner name so that the response carries the original backup domain. The rule: if the record owner equals the root zone origin, replace it with the backup zone origin; if the owner has the root zone as a suffix, replace that suffix with the backup zone origin.

#### Scenario: Owner name at apex is rewritten

- **WHEN** a record returned from root has owner `root.com.` and the query was for `backup.com.`
- **THEN** the response record has owner `backup.com.`

#### Scenario: Owner name below apex is rewritten

- **WHEN** a record returned from root has owner `www.root.com.` and the query was under `backup.com.`
- **THEN** the response record has owner `www.backup.com.`

---
### Requirement: Apply in-bailiwick rewrite to record values

The alias-resolver SHALL rewrite DNS name values inside RDATA of record types `CNAME` (Target), `NS` (Ns), `MX` (Mx), `PTR` (Ptr), `SRV` (Target), and `SOA` (Ns, Mbox) only when the value equals the root zone origin or has the root zone origin as a suffix. Values that do not point into the root zone SHALL be preserved byte-for-byte. Record types `A`, `AAAA`, and `TXT` SHALL NOT have their RDATA modified.

#### Scenario: CNAME pointing within root zone is rewritten

- **WHEN** the root zone record is `blog.root.com. CNAME service.root.com.` and the query is under `backup.com.`
- **THEN** the response record is `blog.backup.com. CNAME service.backup.com.`

#### Scenario: CNAME pointing to a third party is preserved

- **WHEN** the root zone record is `app.root.com. CNAME abc.us-east-1.elb.amazonaws.com.` and the query is under `backup.com.`
- **THEN** the response record is `app.backup.com. CNAME abc.us-east-1.elb.amazonaws.com.`

#### Scenario: NS value within root zone is rewritten

- **WHEN** the root zone record is `root.com. NS ns1.root.com.` and the query is under `backup.com.`
- **THEN** the response record is `backup.com. NS ns1.backup.com.`

#### Scenario: NS value to external nameserver is preserved

- **WHEN** the root zone record is `root.com. NS ns1.externaldns.net.`
- **THEN** the response record value is `ns1.externaldns.net.` unchanged

#### Scenario: SOA MNAME and RNAME within root zone are rewritten

- **WHEN** the root zone SOA is `root.com. SOA ns1.root.com. root.ns1.root.com. (...)` and the query is for `backup.com. SOA`
- **THEN** the response is `backup.com. SOA ns1.backup.com. root.ns1.backup.com. (...)` with all numeric fields preserved byte-for-byte

#### Scenario: A and AAAA RDATA are never rewritten

- **WHEN** the root zone record is `ns1.root.com. A 1.2.3.4` and the query is `ns1.backup.com. A`
- **THEN** the response record is `ns1.backup.com. A 1.2.3.4`

#### Scenario: TXT RDATA is never rewritten

- **WHEN** the root zone record is `root.com. TXT "v=spf1 include:_spf.root.com ~all"` and the query is for `backup.com. TXT` with no override present
- **THEN** the response record is `backup.com. TXT "v=spf1 include:_spf.root.com ~all"` with the TXT string unchanged

---
### Requirement: Merge backup overrides for TXT, MX, and SRV

For a backup-zone query, the alias-resolver SHALL first consult the backup zone's override record set (loaded by zone-parser). If an override exists for the queried (owner, type) pair where type is TXT, MX, or SRV, the override records SHALL be returned in place of the root zone records for that (owner, type) pair. For all other record types and for (owner, type) pairs without an override, the alias-resolver SHALL return the rewritten root zone records.

#### Scenario: Override replaces inherited TXT

- **WHEN** `backup.com` override set contains `backup.com. TXT "google-site-verification=abc"` and root zone has `root.com. TXT "v=spf1 ..."`
- **THEN** querying `backup.com. TXT` returns only the override `"google-site-verification=abc"`

#### Scenario: Inherited MX when no override exists

- **WHEN** `backup.com` has no MX override and root has `root.com. MX 10 mx1.root.com.`
- **THEN** querying `backup.com. MX` returns `backup.com. MX 10 mx1.backup.com.` (inherited with in-bailiwick rewrite applied to the MX target)

#### Scenario: SRV override at same owner is returned

- **WHEN** `backup.com` override set contains `_sip._tcp.backup.com. SRV 10 5 5060 sip.example.com.`
- **THEN** querying `_sip._tcp.backup.com. SRV` returns that override record

---
### Requirement: Inherit SOA fields from root zone when serving backup SOA

When serving a query for the backup zone's SOA record, the alias-resolver SHALL construct the SOA response by copying the root zone's SOA serial, refresh, retry, expire, and minimum fields verbatim and applying the owner-name and in-bailiwick rewrite rules to the record owner, MNAME, and RNAME only.

#### Scenario: Serial is inherited verbatim

- **WHEN** root zone SOA has serial `4230120512` and the query is `backup.com. SOA`
- **THEN** the response SOA serial is `4230120512`

#### Scenario: Numeric timers are inherited verbatim

- **WHEN** root zone SOA has refresh=300, retry=120, expire=86400, minimum=3600
- **THEN** the response SOA has refresh=300, retry=120, expire=86400, minimum=3600

---
### Requirement: Follow in-zone CNAME targets during backup zone resolution

When the alias-resolver resolves a backup-zone query and the root zone lookup yields a CNAME record whose target is within the root zone (in-bailiwick), the alias-resolver SHALL continue looking up `(target, original qtype)` in the root zone and collect the full CNAME chain plus final records. All collected records SHALL be rewritten to the backup namespace using the existing owner-name and in-bailiwick RDATA rewrite rules before returning.

The CNAME following SHALL operate entirely within the root zone's data, using exact lookup first and falling back to wildcard matching at each step. The alias-resolver SHALL stop following when:
1. A non-CNAME record of the requested qtype is found at the current target.
2. The current target is out-of-bailiwick (not within the root zone).
3. No record of any type exists at the current target.
4. The chain depth reaches 8.

#### Scenario: Backup zone query follows in-zone CNAME and returns rewritten records

- **WHEN** `backup.com.` is a backup of `root.com.` AND the root zone contains `app.root.com. CNAME service.root.com.` AND `service.root.com. A 10.0.0.1` AND a client queries `app.backup.com. A`
- **THEN** the alias-resolver returns `app.backup.com. CNAME service.backup.com.` and `service.backup.com. A 10.0.0.1`

#### Scenario: Backup zone CNAME chain is followed within root zone

- **WHEN** `backup.com.` is a backup of `root.com.` AND the root zone contains `a.root.com. CNAME b.root.com.` AND `b.root.com. CNAME c.root.com.` AND `c.root.com. A 9.8.7.6` AND a client queries `a.backup.com. A`
- **THEN** the alias-resolver returns `a.backup.com. CNAME b.backup.com.`, `b.backup.com. CNAME c.backup.com.`, and `c.backup.com. A 9.8.7.6`

#### Scenario: Backup zone CNAME with out-of-bailiwick target stops at the CNAME

- **WHEN** `backup.com.` is a backup of `root.com.` AND the root zone contains `app.root.com. CNAME cdn.external.com.` AND a client queries `app.backup.com. A`
- **THEN** the alias-resolver returns only `app.backup.com. CNAME cdn.external.com.` (target is not rewritten because it is out-of-bailiwick)

#### Scenario: Backup zone wildcard CNAME with in-zone target is followed

- **WHEN** `backup.com.` is a backup of `root.com.` AND the root zone contains `*.root.com. CNAME service.root.com.` AND `service.root.com. A 10.0.0.1` AND a client queries `any.backup.com. A`
- **THEN** the alias-resolver returns `any.backup.com. CNAME service.backup.com.` and `service.backup.com. A 10.0.0.1`

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