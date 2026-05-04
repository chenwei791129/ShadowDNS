## MODIFIED Requirements

### Requirement: Classify zones as root or backup override at load time

When loading all zone files, the zone-parser SHALL consult the alias map produced by the config-loader. Zones whose origin appears as a backup in the alias map SHALL be classified as backup-override zones and only records of type TXT, MX, or SRV SHALL be retained; records of other types in a backup-override zone SHALL be discarded.

For each discarded record in a backup-override zone, the zone-parser SHALL emit a per-record log entry whose level is determined as follows:

1. If the record's type is SOA, the log level SHALL be DEBUG.
2. Otherwise, if the record's type is NS AND the canonical owner equals the canonical zone origin (apex NS), the log level SHALL be DEBUG.
3. Otherwise, the log level SHALL be WARN.

The DEBUG-level cases cover record types whose presence in the zone file is required for RFC 1035 zone-file validity (apex SOA and apex NS) and whose runtime drop is therefore an expected, non-actionable event for every backup-override zone load. The WARN-level case preserves operator visibility for record types that indicate either residual records the operator should clean up (typically via the prune-backup CLI) or unexpected zone-file contents.

After finishing the classification of a single backup-override zone, the zone-parser SHALL emit one INFO log entry summarizing the zone's drop counts if and only if at least one WARN-level drop occurred (i.e. the count of dropped records in any other type category is greater than zero). A backup-override zone whose only drops are SOA records or apex NS records SHALL NOT emit a summary entry, because those drops are RFC 1035 mandated and operationally non-actionable. When emitted, the entry SHALL include at least: the zone origin, the count of dropped SOA records, the count of dropped apex NS records, and the count of dropped records in any other type category.

Zones whose origin does not appear as a backup SHALL be classified as root zones and loaded in full.

#### Scenario: Root zone retains all record types

- **WHEN** the zone origin `root.com.` is not listed as a backup in the alias map
- **THEN** all records in the zone file are retained

#### Scenario: Backup override zone retains only TXT/MX/SRV

- **WHEN** the zone origin `backup.com.` is listed as a backup of `root.com`, and its file contains A, CNAME, TXT, and MX records
- **THEN** only the TXT and MX records are retained AND a WARN-level log entry is emitted for each discarded A and CNAME record with its owner name

#### Scenario: Backup override zone without its own file is allowed

- **WHEN** `aliases.yaml` declares `backup.com` as an alias but no zone file for `backup.com` exists
- **THEN** loading succeeds AND `backup.com` has an empty override set

#### Scenario: Apex SOA drop is logged at DEBUG level

- **WHEN** a backup-override zone with origin `backup.com.` contains an SOA record at the apex
- **THEN** the SOA record SHALL be discarded AND the per-record log entry SHALL be emitted at DEBUG level (not WARN) AND the per-zone INFO summary SHALL include `soa_dropped: 1`

#### Scenario: Apex NS drop is logged at DEBUG level

- **WHEN** a backup-override zone with origin `backup.com.` contains an NS record whose canonical owner is `backup.com.`
- **THEN** the NS record SHALL be discarded AND the per-record log entry SHALL be emitted at DEBUG level (not WARN) AND the per-zone INFO summary SHALL include `apex_ns_dropped: 1`

#### Scenario: Sub-delegation NS drop remains WARN

- **WHEN** a backup-override zone with origin `backup.com.` contains an NS record whose owner is `child.backup.com.` (below the apex)
- **THEN** the NS record SHALL be discarded AND the per-record log entry SHALL be emitted at WARN level AND the per-zone INFO summary SHALL include this drop in `other_dropped`

#### Scenario: Per-zone INFO summary is emitted exactly once per backup zone with WARN-level drops

- **WHEN** the zone-parser finishes classifying records for backup-override zone `backup.com.` whose file produced 1 SOA drop, 4 apex NS drops, 17 A-record drops, and 3 sub-delegation NS drops
- **THEN** exactly one INFO log entry SHALL be emitted naming `backup.com.` with `soa_dropped: 1`, `apex_ns_dropped: 4`, AND `other_dropped: 20` (the union of A-record drops and sub-delegation NS drops)

#### Scenario: Backup zone with only RFC-mandated drops emits no summary

- **WHEN** the zone-parser finishes classifying records for backup-override zone `backup.com.` whose only dropped records are 1 SOA and 2 apex NS (no other types dropped)
- **THEN** no INFO summary log entry SHALL be emitted for this zone, because every drop was RFC 1035 mandated and carries no actionable signal

##### Example: drop-count summary fields

| Field | Type | Description |
|---|---|---|
| zone | string | Zone origin in canonical FQDN form (trailing dot) |
| soa_dropped | int | Count of SOA records discarded for this zone |
| apex_ns_dropped | int | Count of NS records discarded whose canonical owner equals the canonical zone origin |
| other_dropped | int | Count of all other record types discarded (the union of records that produced WARN-level entries) |
