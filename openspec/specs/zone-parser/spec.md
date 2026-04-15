## ADDED Requirements

### Requirement: Parse RFC 1035 master zone files

The zone-parser SHALL accept text-format zone files conforming to RFC 1035, including the `$TTL` and `$ORIGIN` directives, the `@` shorthand for the current origin, multi-line records enclosed in `(` ... `)`, and `;` line comments. It SHALL NOT rely on a specific filename extension to identify zone files; the file path is supplied by the config-loader.

#### Scenario: Standard SOA with multi-line body

- **WHEN** a file contains `@ IN SOA ns1.root.com. root.ns1.root.com. (4230120512 ;serial\n300 ;refresh\n120 ;retry\n86400 ;expire\n3600 ) ;minimum`
- **THEN** the parser produces an SOA record with serial=4230120512, refresh=300, retry=120, expire=86400, minimum=3600 and MNAME=`ns1.root.com.`, RNAME=`root.ns1.root.com.`

#### Scenario: Records using `@` for origin

- **WHEN** the zone origin is `root.com.` and a line reads `@ IN NS ns1.root.com.`
- **THEN** the parser produces an NS record with owner name `root.com.`

#### Scenario: Comment-only and blank lines are skipped

- **WHEN** the file contains `;`-prefixed lines or blank lines
- **THEN** the parser ignores them and does not emit records

#### Scenario: Commented-out record is not emitted

- **WHEN** a line reads `;@ IN A 1.2.3.4`
- **THEN** the parser produces no record for that line


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

### Requirement: Build an in-memory zone structure

The zone-parser SHALL build an in-memory representation for each zone containing: (a) the zone origin, (b) the SOA record, (c) an index of records keyed by the fully-qualified owner name, and (d) the TTL applied to each record.

#### Scenario: Records are indexed by owner name for O(1) lookup

- **WHEN** a zone file defines records `www A 1.2.3.4`, `mail A 5.6.7.8`, `@ A 9.10.11.12` under origin `root.com.`
- **THEN** the resulting zone exposes lookups by keys `www.root.com.`, `mail.root.com.`, `root.com.`

#### Scenario: Default TTL applied from $TTL directive

- **WHEN** the file begins with `$TTL 300` and a record omits an explicit TTL
- **THEN** the parsed record carries TTL 300

#### Scenario: Per-record TTL overrides $TTL

- **WHEN** a record declares its own TTL (e.g., `www 600 IN A 1.2.3.4`) under `$TTL 300`
- **THEN** the parsed record carries TTL 600


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

### Requirement: Classify zones as root or backup override at load time

When loading all zone files, the zone-parser SHALL consult the alias map produced by the config-loader. Zones whose origin appears as a backup in the alias map SHALL be classified as backup-override zones and only records of type TXT, MX, or SRV SHALL be retained; records of other types in a backup-override zone SHALL be discarded with a warning. Zones whose origin does not appear as a backup SHALL be classified as root zones and loaded in full.

#### Scenario: Root zone retains all record types

- **WHEN** the zone origin `root.com.` is not listed as a backup in the alias map
- **THEN** all records in the zone file are retained

#### Scenario: Backup override zone retains only TXT/MX/SRV

- **WHEN** the zone origin `backup.com.` is listed as a backup of `root.com`, and its file contains A, CNAME, TXT, and MX records
- **THEN** only the TXT and MX records are retained AND a warning is logged for each discarded A and CNAME record with its owner name

#### Scenario: Backup override zone without its own file is allowed

- **WHEN** `aliases.yaml` declares `backup.com` as an alias but no zone file for `backup.com` exists
- **THEN** loading succeeds AND `backup.com` has an empty override set


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

### Requirement: Fail loudly on malformed zone data

The zone-parser SHALL return a fatal error that names the file path and line number when it encounters syntax that does not conform to RFC 1035, an unknown RR type, or a record whose owner name is outside the zone origin.

#### Scenario: Out-of-zone owner name is rejected

- **WHEN** a file with origin `root.com.` contains `example.org. IN A 1.2.3.4`
- **THEN** the parser returns a fatal error citing the file and line

#### Scenario: Unknown RR type is rejected

- **WHEN** a file contains an RR type that is not recognized by the miekg/dns library
- **THEN** the parser returns a fatal error citing the file and line

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

### Requirement: Parse RFC 1035 master zone files

The zone-parser SHALL accept text-format zone files conforming to RFC 1035, including the `$TTL` and `$ORIGIN` directives, the `@` shorthand for the current origin, multi-line records enclosed in `(` ... `)`, and `;` line comments. It SHALL NOT rely on a specific filename extension to identify zone files; the file path is supplied by the config-loader.

#### Scenario: Standard SOA with multi-line body

- **WHEN** a file contains `@ IN SOA ns1.root.com. root.ns1.root.com. (4230120512 ;serial\n300 ;refresh\n120 ;retry\n86400 ;expire\n3600 ) ;minimum`
- **THEN** the parser produces an SOA record with serial=4230120512, refresh=300, retry=120, expire=86400, minimum=3600 and MNAME=`ns1.root.com.`, RNAME=`root.ns1.root.com.`

#### Scenario: Records using `@` for origin

- **WHEN** the zone origin is `root.com.` and a line reads `@ IN NS ns1.root.com.`
- **THEN** the parser produces an NS record with owner name `root.com.`

#### Scenario: Comment-only and blank lines are skipped

- **WHEN** the file contains `;`-prefixed lines or blank lines
- **THEN** the parser ignores them and does not emit records

#### Scenario: Commented-out record is not emitted

- **WHEN** a line reads `;@ IN A 1.2.3.4`
- **THEN** the parser produces no record for that line

---
### Requirement: Build an in-memory zone structure

The zone-parser SHALL build an in-memory representation for each zone containing: (a) the zone origin, (b) the SOA record, (c) an index of records keyed by the fully-qualified owner name, and (d) the TTL applied to each record.

#### Scenario: Records are indexed by owner name for O(1) lookup

- **WHEN** a zone file defines records `www A 1.2.3.4`, `mail A 5.6.7.8`, `@ A 9.10.11.12` under origin `root.com.`
- **THEN** the resulting zone exposes lookups by keys `www.root.com.`, `mail.root.com.`, `root.com.`

#### Scenario: Default TTL applied from $TTL directive

- **WHEN** the file begins with `$TTL 300` and a record omits an explicit TTL
- **THEN** the parsed record carries TTL 300

#### Scenario: Per-record TTL overrides $TTL

- **WHEN** a record declares its own TTL (e.g., `www 600 IN A 1.2.3.4`) under `$TTL 300`
- **THEN** the parsed record carries TTL 600

---
### Requirement: Classify zones as root or backup override at load time

When loading all zone files, the zone-parser SHALL consult the alias map produced by the config-loader. Zones whose origin appears as a backup in the alias map SHALL be classified as backup-override zones and only records of type TXT, MX, or SRV SHALL be retained; records of other types in a backup-override zone SHALL be discarded with a warning. Zones whose origin does not appear as a backup SHALL be classified as root zones and loaded in full.

#### Scenario: Root zone retains all record types

- **WHEN** the zone origin `root.com.` is not listed as a backup in the alias map
- **THEN** all records in the zone file are retained

#### Scenario: Backup override zone retains only TXT/MX/SRV

- **WHEN** the zone origin `backup.com.` is listed as a backup of `root.com`, and its file contains A, CNAME, TXT, and MX records
- **THEN** only the TXT and MX records are retained AND a warning is logged for each discarded A and CNAME record with its owner name

#### Scenario: Backup override zone without its own file is allowed

- **WHEN** `aliases.yaml` declares `backup.com` as an alias but no zone file for `backup.com` exists
- **THEN** loading succeeds AND `backup.com` has an empty override set

---
### Requirement: Fail loudly on malformed zone data

The zone-parser SHALL return a fatal error that names the file path and line number when it encounters syntax that does not conform to RFC 1035, an unknown RR type, or a record whose owner name is outside the zone origin.

#### Scenario: Out-of-zone owner name is rejected

- **WHEN** a file with origin `root.com.` contains `example.org. IN A 1.2.3.4`
- **THEN** the parser returns a fatal error citing the file and line

#### Scenario: Unknown RR type is rejected

- **WHEN** a file contains an RR type that is not recognized by the miekg/dns library
- **THEN** the parser returns a fatal error citing the file and line

---
### Requirement: Accept BIND-compatible `$INCLUDE` directive with quoted file path

The zone-parser SHALL accept the `$INCLUDE` directive in both its RFC 1035 bare form (`$INCLUDE /path/to/file`) and its BIND-compatible quoted form (`$INCLUDE "/path/to/file"`). The directive name is case-insensitive: `$INCLUDE` and `$include` MUST be treated identically. When an optional origin argument follows the file path, it MUST be preserved unchanged. When a zone file uses the quoted form, the parser SHALL produce the same set of records as when the same content is written with the bare form.

The parser MUST NOT alter quoted strings that appear outside a line-leading `$INCLUDE` directive. In particular, quoted rdata such as TXT record strings (e.g., `@ IN TXT "v=spf1 -all"`) SHALL remain byte-for-byte unchanged in their effect on parsing.

When the quoted form is malformed (e.g., an opening `"` without a matching closing `"` on the same line), the parser SHALL NOT silently correct it; the original malformed content MUST be passed through so that the underlying zone-file parser can report the syntax error with the original line number.

#### Scenario: `$include` with quoted path loads the included fragment

- **WHEN** a zone file for origin `example.com.` contains `$include "testdata/fragments/example.com_cname"` on its own line, and the referenced fragment declares `alias IN CNAME target.example.com.`
- **THEN** the parser loads the zone successfully AND the record for `alias.example.com.` is present in the resulting zone

#### Scenario: `$INCLUDE` uppercase with quoted path loads the included fragment

- **WHEN** a zone file for origin `example.com.` contains `$INCLUDE "testdata/fragments/example.com_cname"` on its own line
- **THEN** the parser loads the zone successfully with the same records as the lowercase variant

#### Scenario: Bare `$include path` continues to work

- **WHEN** a zone file contains `$include testdata/fragments/example.com_cname` without quotes
- **THEN** the parser loads the zone successfully, matching pre-existing behavior

#### Scenario: TXT record with quoted string is unaffected

- **WHEN** a zone file contains `@ IN TXT "v=spf1 -all"` as a record line (not a directive)
- **THEN** the parser produces a TXT record whose text value is `v=spf1 -all`, identical to parsing without the BIND-compatibility layer

#### Scenario: Trailing comment after quoted include is tolerated

- **WHEN** a zone file contains `$include "testdata/fragments/example.com_cname" ; generated by zone-tool` on a single line
- **THEN** the parser loads the included fragment AND the trailing comment is ignored, as it would be after a bare-form `$include`

#### Scenario: Error line number reflects the original file

- **WHEN** a zone file contains malformed content on line N, where line N is after one or more quoted `$include` directives
- **THEN** any parse error reported by the parser cites line N of the original file, not a shifted line number

<!-- @trace
source: bind-include-quoted-path
updated: 2026-04-15
code:
  - testdata/integration/master/example.com_include.fwd
  - scripts/smoke.sh
  - testdata/integration/master.zones
  - README.md
  - testdata/integration/master/backup.example_overrides
  - testdata/integration/master/cnames/example.com_cname
  - testdata/integration/master/backup.example_view-other.fwd
  - internal/zone/parser.go
tests:
  - internal/zone/parser_test.go
  - test/integration/backup_test.go
  - test/integration/query_test.go
  - test/integration/helpers_test.go
-->