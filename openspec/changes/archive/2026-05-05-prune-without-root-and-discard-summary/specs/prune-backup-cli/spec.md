## MODIFIED Requirements

### Requirement: Iterate backup zones per view

For each view declared in `named.conf`, the sub-command SHALL enumerate zones whose origin appears as a backup in the alias map, pair each with its aliased root zone in the same view, and process each `(view, backup zone)` pair independently.

When a view declares a backup zone but does NOT declare the aliased root zone in the same view, the sub-command SHALL NOT skip that pair. Instead, it SHALL run the pair in **root-less mode**: the sub-command SHALL plan deletions for backup-zone records whose RR type is NOT in the overridable-type set (i.e. NOT TXT, MX, or SRV), without consulting any root zone. Records whose RR type IS in the overridable-type set SHALL be retained in root-less mode, because the decision to delete those types depends on byte-equality against the root RRSet which is unavailable. The sub-command SHALL emit one INFO log entry per `(view, backup)` pair entering root-less mode, identifying the view, the backup origin, the missing root origin, and the fact that overridable-type records are retained without root comparison. The sub-command SHALL NOT abort processing of other pairs.

#### Scenario: multi-view backup pairing

- **WHEN** `named.conf` declares views `view-th` and `view-other`, both with zone origin `backup.example` aliased to `example.com`
- **THEN** the sub-command SHALL process `(view-th, backup.example)` and `(view-other, backup.example)` as separate units, each compared against that view's `example.com` zone file

#### Scenario: zone origin that is not a backup is skipped

- **WHEN** a view declares a zone origin not present as a key in the alias map
- **THEN** that zone SHALL NOT be processed AND its file SHALL NOT be read

#### Scenario: backup without a matching root in the same view runs in root-less mode

- **WHEN** a view declares a backup zone `backup.example` aliased to `example.com` AND `example.com` is NOT declared in the same view
- **THEN** the sub-command SHALL emit one INFO log entry naming the `(view, backup, missing-root)` triple AND SHALL plan deletions for every record in the backup zone whose RR type is not in `{TXT, MX, SRV}` AND SHALL retain every record whose RR type IS in `{TXT, MX, SRV}` without comparing them to any root zone AND SHALL continue processing other pairs

##### Example: root-less mode deletes CNAME and A but retains TXT/MX/SRV

- **GIVEN** view `view-th` declares zone `backup.example.com` aliased to `root.example.com` AND `root.example.com` is NOT declared in `view-th` AND the backup zone file contains 2854 CNAME records, 32 A records, 1 TXT record, and 1 MX record (excluding apex SOA/NS which are governed by zone-parser, not by this CLI)
- **WHEN** `shadowdns prune-backup --apply` runs
- **THEN** the planned deletion set for this pair SHALL contain the 2854 CNAME records and 32 A records AND SHALL NOT contain the TXT or MX records AND the sub-command SHALL emit one INFO log entry naming `view-th`, `backup.example.com.`, and `root.example.com.` (the missing root)

#### Scenario: backup without a matching root does not abort other pairs

- **WHEN** view `view-th` declares backup `b1.example` aliased to a missing root `r1.example`, AND the same view declares backup `b2.example` aliased to a present root `r2.example`
- **THEN** the sub-command SHALL plan deletions for `(view-th, b1.example)` in root-less mode AND SHALL plan deletions for `(view-th, b2.example)` using the normal root-comparison path AND both pairs SHALL appear in the dry-run output
