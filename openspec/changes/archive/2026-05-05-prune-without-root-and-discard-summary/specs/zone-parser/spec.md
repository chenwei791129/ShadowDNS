## MODIFIED Requirements

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
