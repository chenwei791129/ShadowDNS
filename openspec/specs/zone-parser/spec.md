## Requirements

<!-- @trace
source: shadowdns-foundation
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - testdata/integration/named.conf
  - testdata/integration/db.example.com-th
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/named.conf.local
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
  - testdata/integration/db.example.com-other
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/db.backup.example-th
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/db.backup.example-other
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

<!-- @trace
source: shadowdns-foundation
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - testdata/integration/named.conf
  - testdata/integration/db.example.com-th
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/named.conf.local
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
  - testdata/integration/db.example.com-other
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/db.backup.example-th
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/db.backup.example-other
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

<!-- @trace
source: shadowdns-foundation
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - testdata/integration/named.conf
  - testdata/integration/db.example.com-th
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/named.conf.local
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
  - testdata/integration/db.example.com-other
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/db.backup.example-th
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/db.backup.example-other
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

---
### Requirement: Classify zones as root or backup override at load time

When loading all zone files, the zone-parser SHALL consult the alias map produced by the config-loader. Zones whose origin appears as a backup in the alias map SHALL be classified as backup-override zones and only records of type TXT, MX, or SRV SHALL be retained; records of other types in a backup-override zone SHALL be discarded.

For each discarded record in a backup-override zone, the zone-parser SHALL emit a per-record log entry at DEBUG level. The per-record entry is intended for operators who explicitly enable DEBUG-level logging during investigation; it MUST NOT appear at INFO or higher levels. This change supersedes the previous behaviour where non-SOA, non-apex-NS drops were emitted at WARN level.

After finishing the classification of a single backup-override zone, the zone-parser SHALL emit at most one INFO-level summary log entry per zone. The summary SHALL be emitted whenever the zone produced at least one discarded record (regardless of which RR types were dropped), and SHALL be omitted only when zero records were discarded. When emitted, the entry SHALL include:

1. The zone origin in canonical FQDN form (with trailing dot).
2. A by-type drop histogram structured as a deterministic map from RR-type label to count, where:
   - Non-SOA, non-apex-NS RR types use the dnsutil RR type label (e.g. `A`, `AAAA`, `CNAME`, `PTR`, `NS`).
   - Apex SOA drops use the literal label `SOA`.
   - Apex NS drops (NS records whose canonical owner equals the canonical zone origin) use the literal label `apex_NS` to disambiguate from sub-delegation NS drops.
   - The map SHALL be emitted in a structured form that produces deterministic key ordering when serialized by the configured zap encoder (alphabetic by label).

The previous summary fields `soa_dropped`, `apex_ns_dropped`, and `other_dropped` SHALL NOT appear in the new summary; the histogram replaces them in full.

Zones whose origin does not appear as a backup SHALL be classified as root zones and loaded in full.

#### Scenario: Root zone retains all record types

- **WHEN** the zone origin `root.com.` is not listed as a backup in the alias map
- **THEN** all records in the zone file are retained

#### Scenario: Backup override zone retains only TXT/MX/SRV

- **WHEN** the zone origin `backup.com.` is listed as a backup of `root.com`, and its file contains A, CNAME, TXT, and MX records
- **THEN** only the TXT and MX records are retained AND a DEBUG-level log entry is emitted for each discarded A and CNAME record with its owner name AND no WARN-level entry is emitted for any of those discards

#### Scenario: Backup override zone without its own file is allowed

- **WHEN** `aliases.yaml` declares `backup.com` as an alias but no zone file for `backup.com` exists
- **THEN** loading succeeds AND `backup.com` has an empty override set

#### Scenario: Apex SOA drop is logged at DEBUG level

- **WHEN** a backup-override zone with origin `backup.com.` contains an SOA record at the apex
- **THEN** the SOA record SHALL be discarded AND the per-record log entry SHALL be emitted at DEBUG level AND the per-zone INFO summary SHALL include `SOA: 1` in the histogram

#### Scenario: Apex NS drop is logged at DEBUG level

- **WHEN** a backup-override zone with origin `backup.com.` contains an NS record whose canonical owner is `backup.com.`
- **THEN** the NS record SHALL be discarded AND the per-record log entry SHALL be emitted at DEBUG level AND the per-zone INFO summary SHALL include `apex_NS: 1` in the histogram

#### Scenario: Sub-delegation NS drop is also DEBUG and bucketed under NS

- **WHEN** a backup-override zone with origin `backup.com.` contains an NS record whose owner is `child.backup.com.` (below the apex)
- **THEN** the NS record SHALL be discarded AND the per-record log entry SHALL be emitted at DEBUG level (no longer WARN) AND the per-zone INFO summary SHALL include this drop under the `NS` histogram bucket (distinct from `apex_NS`)

#### Scenario: Per-zone INFO summary is emitted whenever any drop occurred

- **WHEN** the zone-parser finishes classifying records for backup-override zone `backup.com.` whose file produced 1 SOA drop, 4 apex NS drops, 17 A-record drops, and 3 sub-delegation NS drops
- **THEN** exactly one INFO log entry SHALL be emitted naming `backup.com.` with histogram `{A: 17, NS: 3, SOA: 1, apex_NS: 4}` (alphabetic key order)

#### Scenario: Backup zone with only RFC-mandated drops still emits a summary

- **WHEN** the zone-parser finishes classifying records for backup-override zone `backup.com.` whose only dropped records are 1 SOA and 2 apex NS
- **THEN** exactly one INFO log entry SHALL be emitted naming `backup.com.` with histogram `{SOA: 1, apex_NS: 2}` (this differs from the previous spec, where this case suppressed the summary)

#### Scenario: Backup zone with zero drops emits no summary

- **WHEN** the zone-parser finishes classifying records for backup-override zone `backup.com.` whose file contains only TXT and MX records (no records of any other type, no apex SOA, no apex NS)
- **THEN** no INFO summary log entry SHALL be emitted for this zone

##### Example: drop-count summary fields

| Field | Type | Description |
|---|---|---|
| zone | string | Zone origin in canonical FQDN form (trailing dot) |
| dropped | structured map | RR-type label → count of records discarded for this zone, with deterministic alphabetic key order; type labels include standard RR-type names plus the literal `apex_NS` to disambiguate apex-NS drops from sub-delegation NS drops |

<!-- @trace
source: shadowdns-foundation
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - testdata/integration/named.conf
  - testdata/integration/db.example.com-th
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/named.conf.local
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
  - testdata/integration/db.example.com-other
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/db.backup.example-th
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/db.backup.example-other
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
source: quiet-expected-backup-drop-warnings
updated: 2026-05-04
code:
  - internal/zone/classify.go
  - cmd/shadowdns/main.go
tests:
  - cmd/shadowdns/main_test.go
  - internal/server/server_test.go
  - internal/zone/classify_test.go
-->

<!-- @trace
source: prune-without-root-and-discard-summary
updated: 2026-05-05
code:
  - internal/prunebackup/prunebackup.go
  - internal/prunebackup/diff.go
  - internal/shadowdnscfg/config.go
  - testdata/integration/shadowdns.yaml
  - testdata/integration/db.example.com-th
  - cmd/shadowdns/prune_backup.go
  - packaging/shadowdns.yaml.example
  - testdata/integration/db.example.com-other
  - internal/zone/classify.go
tests:
  - cmd/shadowdns/main_ephemeral_test.go
  - internal/prunebackup/diff_test.go
  - internal/prunebackup/prunebackup_test.go
  - test/integration/query_test.go
  - test/integration/case_preservation_test.go
  - internal/shadowdnscfg/config_test.go
  - cmd/shadowdns/prune_backup_test.go
  - internal/zone/classify_test.go
-->

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
  - testdata/integration/db.include-test.example
  - scripts/smoke.sh
  - testdata/integration/named.conf.local
  - README.md
  - testdata/integration/db.backup.example.overrides
  - testdata/integration/cnames/db.example.com.cname
  - testdata/integration/db.backup.example-other
  - internal/zone/parser.go
tests:
  - internal/zone/parser_test.go
  - test/integration/backup_test.go
  - test/integration/query_test.go
  - test/integration/helpers_test.go
-->

---
### Requirement: Parse and store wildcard owner names

The zone-parser SHALL correctly parse zone file entries with the `*` wildcard label (e.g., `*.example.com. A 1.2.3.4`) and store them in the Zone.Records map under the key `*.example.com.` (lowercased, FQDN with trailing dot). The `*` label SHALL be preserved as a literal `*` character in the map key, not expanded or interpreted during parsing.

#### Scenario: Wildcard A record is parsed and stored

- **WHEN** a zone file contains `* 300 IN A 1.2.3.4` with `$ORIGIN example.com.`
- **THEN** the parsed Zone.Records map contains an entry at key `*.example.com.` with one A record having RDATA `1.2.3.4`

#### Scenario: Wildcard CNAME record is parsed and stored

- **WHEN** a zone file contains `*.sub 300 IN CNAME target.other.com.` with `$ORIGIN example.com.`
- **THEN** the parsed Zone.Records map contains an entry at key `*.sub.example.com.` with one CNAME record targeting `target.other.com.`

#### Scenario: Multiple wildcard records at the same owner are stored together

- **WHEN** a zone file contains `* 300 IN A 1.2.3.4` and `* 300 IN A 5.6.7.8` with `$ORIGIN example.com.`
- **THEN** the parsed Zone.Records map contains an entry at key `*.example.com.` with two A records

<!-- @trace
source: wildcard-support
updated: 2026-04-16
code:
  - internal/server/handler.go
  - README.md
  - internal/zone/zone.go
  - internal/alias/override.go
  - testdata/integration/db.example.com-other
  - testdata/integration/db.example.com-th
tests:
  - internal/alias/override_test.go
  - test/integration/wildcard_test.go
  - test/integration/cname_synthesis_test.go
  - internal/zone/parser_test.go
  - test/integration/negative_test.go
  - internal/zone/zone_test.go
  - internal/server/server_test.go
-->

---
### Requirement: Index parsed records by owner and qtype

The zone-parser SHALL store parsed resource records in an owner-indexed map whose values are further indexed by RR type code (qtype), such that a `(owner, qtype)` pair resolves to its matching records via two map lookups and without per-query filtering. The outer key SHALL be the canonical owner name (lowercased, trailing-dot terminated, as produced by existing parsing rules). The inner key SHALL be the RR type code as defined by `miekg/dns.Type*` constants.

The zone-parser SHALL NOT store byte-identical duplicate records within a single `(owner, qtype)` RRset. When a record is inserted whose owner, type, and RDATA match a record already stored at that `(owner, qtype)` (TTL excluded from the comparison, as determined by `miekg/dns.IsDuplicate`), the parser SHALL keep the first-stored record (including its TTL) and discard the later duplicate. This invariant SHALL hold regardless of whether the duplicate originates from inline records, a `$INCLUDE`-expanded fragment, or any combination thereof, and SHALL be enforced by the record-insertion primitive so that every load path is covered. Records that differ in RDATA (distinct records) SHALL all be retained.

#### Scenario: Records at the same owner with different qtypes are stored separately

- **WHEN** a zone file contains `www.example.com. IN A 192.0.2.1` and `www.example.com. IN AAAA 2001:db8::1` and `www.example.com. IN TXT "v=spf1 -all"`
- **THEN** `Zone.Records["www.example.com."][TypeA]` contains exactly one A record, `Zone.Records["www.example.com."][TypeAAAA]` contains exactly one AAAA record, and `Zone.Records["www.example.com."][TypeTXT]` contains exactly one TXT record
- **AND** no single slice at `Zone.Records["www.example.com."]` mixes RRs of different qtypes

#### Scenario: Multiple distinct records of the same qtype at one owner are preserved in insertion order

- **WHEN** a zone file contains `a.example.com. IN A 192.0.2.1` followed by `a.example.com. IN A 192.0.2.2` followed by `a.example.com. IN A 192.0.2.3`
- **THEN** `Zone.Records["a.example.com."][TypeA]` is a slice of length 3 whose elements appear in the order 192.0.2.1, 192.0.2.2, 192.0.2.3

#### Scenario: Byte-identical duplicate of the same qtype at one owner is stored once

- **WHEN** a zone file contains `a.example.com. IN A 192.0.2.1` followed by a second `a.example.com. IN A 192.0.2.1`
- **THEN** `Zone.Records["a.example.com."][TypeA]` is a slice of length 1 containing 192.0.2.1

#### Scenario: Duplicate CNAME declared inline and via $INCLUDE is collapsed

- **WHEN** a zone file declares `host.example.com. IN CNAME target.example.com.` inline and also `$INCLUDE`s a fragment that declares the identical `host.example.com. IN CNAME target.example.com.`
- **THEN** `Zone.Records["host.example.com."][TypeCNAME]` is a slice of length 1
- **AND** a query that follows the chain through `host.example.com.` emits the CNAME exactly once

#### Scenario: TTL is excluded from duplicate identity

- **WHEN** a zone file contains `a.example.com. 300 IN A 192.0.2.1` followed by `a.example.com. 60 IN A 192.0.2.1`
- **THEN** `Zone.Records["a.example.com."][TypeA]` is a slice of length 1
- **AND** the retained record carries the first occurrence's TTL of 300

#### Scenario: Owner with no records for a given qtype yields no inner entry

- **WHEN** a zone file contains only `www.example.com. IN A 192.0.2.1`
- **THEN** `Zone.Records["www.example.com."][TypeA]` is a non-empty slice
- **AND** `Zone.Records["www.example.com."][TypeAAAA]` is the zero value (nil slice) and the inner map has no entry for TypeAAAA

#### Scenario: Non-existent owner yields no outer entry

- **WHEN** a zone file contains records for `www.example.com.` only
- **THEN** `Zone.Records["nonexistent.example.com."]` is the zero value (nil inner map)


<!-- @trace
source: dedup-rrset-on-load
updated: 2026-06-14
code:
  - internal/zone/parser.go
  - test.yaml
  - docs/migration.md
  - docs/migration.zh.md
  - internal/zone/zone.go
tests:
  - internal/zone/parser_test.go
  - internal/zone/zone_test.go
-->

---
### Requirement: Lookup returns stored records as a shared-backing reference

`Zone.Lookup(owner, qtype)` and `Zone.LookupWildcard(qname, qtype)` SHALL return the slice stored at `Zone.Records[owner][qtype]` as a direct reference whose underlying array is shared with the stored record list. The caller SHALL NOT mutate the returned slice (no element assignment, no `append` that shares capacity, no sort). When the owner or qtype has no records, the return value SHALL be a nil or zero-length slice; callers SHALL check `len()` rather than relying on `!= nil`.

#### Scenario: Lookup result shares backing array with stored records

- **WHEN** `Zone.Records["a.example.com."][TypeA]` contains two A records
- **THEN** the slice returned by `Lookup("a.example.com.", TypeA)` has the same underlying array address as the stored slice
- **AND** iterating the return value yields the same records in the same order as the stored slice

#### Scenario: Lookup for missing qtype returns empty slice without error

- **WHEN** owner `a.example.com.` exists with records only at TypeA
- **THEN** `Lookup("a.example.com.", TypeAAAA)` returns a slice with `len() == 0`
- **AND** no panic or error occurs

<!-- @trace
source: zone-records-qtype-index
updated: 2026-04-20
code:
  - internal/server/handler.go
  - internal/zone/parser.go
  - internal/alias/override.go
  - internal/transfer/notify.go
  - internal/zone/classify.go
  - internal/zone/zone.go
  - internal/transfer/axfr.go
tests:
  - internal/alias/override_test.go
  - internal/server/server_test.go
  - internal/zone/parser_test.go
  - internal/zone/zone_test.go
-->

---
### Requirement: Preserve zone-file case in stored RRs while indexing on lowercase

The zone-parser SHALL store each parsed resource record in memory with its owner name field (`Header().Name`) and any name-bearing RDATA field (CNAME target, NS, MX exchange, PTR, SRV target, SOA MNAME, SOA RNAME) byte-for-byte as written in the zone file. The internal lookup index keyed on owner name SHALL use a lowercase-folded form of the name solely as the index key, without modifying the stored RR. Subsequent lookups SHALL fold the query name to the same lowercase form before comparing against index keys, satisfying RFC 4343 case-insensitive matching while keeping stored data case-preserving for response emission.

#### Scenario: Mixed-case owner in zone file is stored as written

- **WHEN** a zone file contains `Service.Root.Com. IN A 1.2.3.4`
- **THEN** the in-memory zone has at least one RR whose `Header().Name` equals `Service.Root.Com.` byte-for-byte

#### Scenario: Lookup with lowercase query finds the mixed-case stored record

- **WHEN** a zone file contains `Service.Root.Com. IN A 1.2.3.4` and a lookup is performed with key `service.root.com.`
- **THEN** the lookup returns the stored RR (case-insensitive index hit) whose `Header().Name` remains `Service.Root.Com.`

#### Scenario: Lookup with mixed-case query finds the same record

- **WHEN** a zone file contains `Service.Root.Com. IN A 1.2.3.4` and a lookup is performed with key `SERVICE.root.COM.`
- **THEN** the lookup returns the stored RR with `Header().Name` = `Service.Root.Com.`

#### Scenario: Mixed-case CNAME target is preserved

- **WHEN** a zone file contains `alias.root.com. IN CNAME Target.Root.Com.`
- **THEN** the stored CNAME RDATA `Target` field equals `Target.Root.Com.` byte-for-byte

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
### Requirement: Log resource-record deduplication at load

When the zone-parser discards a duplicate record (per the deduplication invariant above), it SHALL record the event for operability without flooding logs at production scale, where a single zone-view can contain thousands of duplicates. Per-duplicate detail SHALL be emitted at DEBUG level only, and the parser SHALL guard the DEBUG call so that a disabled DEBUG level incurs no per-record formatting cost. For each parsed zone that discarded at least one duplicate, the parser SHALL emit exactly one aggregated WARN summary that includes the zone origin, the total number of duplicates discarded, and a by-RR-type histogram of the discarded records. A zone that discarded no duplicates SHALL NOT emit a summary.

#### Scenario: Each discarded duplicate is logged at DEBUG

- **WHEN** a zone load discards a duplicate `host.example.com. IN CNAME target.example.com.`
- **AND** the logger has DEBUG level enabled
- **THEN** a DEBUG entry is emitted carrying the zone origin, the owner `host.example.com.`, and the type `CNAME`

#### Scenario: Per-record DEBUG is skipped when DEBUG is disabled

- **WHEN** a zone load discards one or more duplicates
- **AND** the logger has DEBUG level disabled
- **THEN** no per-duplicate DEBUG entry is emitted
- **AND** the aggregated summary is still emitted

#### Scenario: Per-zone WARN summary is emitted whenever any duplicate was discarded

- **WHEN** a zone load discards 3 duplicate A records and 1 duplicate CNAME record at various owners
- **THEN** exactly one WARN entry is emitted for that zone
- **AND** it reports the zone origin, a total of 4, and a histogram of `{A: 3, CNAME: 1}`

#### Scenario: Zone with zero duplicates emits no summary

- **WHEN** a zone load discards no duplicate records
- **THEN** no deduplication summary entry is emitted for that zone

<!-- @trace
source: dedup-rrset-on-load
updated: 2026-06-14
code:
  - internal/zone/parser.go
  - test.yaml
  - docs/migration.md
  - docs/migration.zh.md
  - internal/zone/zone.go
tests:
  - internal/zone/parser_test.go
  - internal/zone/zone_test.go
-->