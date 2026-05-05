## ADDED Requirements

### Requirement: Provide a prune-backup sub-command

The `shadowdns` binary SHALL expose a `prune-backup` sub-command that runs an offline scan of backup zone files, compares each backup zone's records against its aliased root zone, and reports or removes redundant records. The sub-command SHALL NOT open any network sockets, SHALL NOT send SIGHUP, and SHALL NOT mutate the running server's state.

#### Scenario: sub-command is registered

- **WHEN** an operator runs `shadowdns prune-backup --help`
- **THEN** the help output SHALL list `--named-conf`, `--config`, `--apply` flags, AND the invocation SHALL exit with status 0 without attempting to bind any UDP/TCP listener

#### Scenario: required flags are enforced

- **WHEN** an operator runs `shadowdns prune-backup` without `--named-conf` or without `--config`
- **THEN** the sub-command SHALL exit with a non-zero status AND SHALL print a message naming the missing required flag


<!-- @trace
source: prune-redundant-backup-records
updated: 2026-05-04
code:
  - internal/transfer/axfr.go
  - internal/ephemeral/store.go
  - internal/prunebackup/include.go
  - internal/prunebackup/lexer.go
  - cmd/shadowdns/prune_backup.go
  - testdata/integration/shadowdns.yaml
  - internal/server/build.go
  - packaging/shadowdns.yaml.example
  - internal/server/server.go
  - internal/alias/rewrite.go
  - internal/server/handler.go
  - internal/prunebackup/rewrite.go
  - .release-please-manifest.json
  - internal/prunebackup/apply.go
  - internal/prunebackup/doc.go
  - internal/prunebackup/diff.go
  - CHANGELOG.md
  - README.md
  - internal/api/server.go
  - internal/zone/zone.go
  - cmd/shadowdns/main.go
  - internal/dnsutil/dnsutil.go
  - internal/shadowdnscfg/config.go
  - internal/alias/override.go
  - internal/zone/parser.go
  - internal/config/aliases.go
  - internal/prunebackup/prunebackup.go
tests:
  - internal/prunebackup/diff_test.go
  - test/integration/stress_ceiling_test.go
  - internal/zone/zone_test.go
  - test/integration/stress_shared_bucket_test.go
  - internal/config/aliases_test.go
  - internal/zone/parser_test.go
  - internal/alias/rewrite_anywhere_test.go
  - internal/prunebackup/apply_test.go
  - internal/server/handler_ephemeral_test.go
  - internal/ephemeral/store_test.go
  - test/integration/reload_diff_test.go
  - internal/prunebackup/prunebackup_test.go
  - internal/prunebackup/include_test.go
  - internal/server/build_test.go
  - internal/server/handler_test.go
  - internal/prunebackup/rewrite_test.go
  - internal/server/server_test.go
  - internal/alias/rewrite_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/shadowdnscfg/config_test.go
  - test/integration/case_preservation_test.go
  - test/integration/alias_rdata_rewrite_test.go
  - internal/alias/override_test.go
  - test/integration/compression_budget_test.go
  - test/integration/ephemeral_overrides_cname_test.go
  - test/integration/listenon_test.go
  - test/integration/helpers_test.go
  - test/integration/prune_backup_test.go
  - internal/prunebackup/lexer_test.go
  - cmd/shadowdns/prune_backup_test.go
  - internal/transfer/axfr_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/axfr_test.go
-->

### Requirement: Load config and named.conf identically to the server

The sub-command SHALL load `named.conf` via the existing config-loader path and the unified YAML config via `internal/shadowdnscfg`, using the same validation rules as the server startup path. Any load or validation failure SHALL abort the sub-command with a non-zero exit status before any zone file is read or modified.

#### Scenario: named.conf load failure aborts prune

- **WHEN** `--named-conf` points to a file that fails to parse
- **THEN** the sub-command SHALL exit with a non-zero status AND no backup zone file SHALL be read

#### Scenario: unified config validation failure aborts prune

- **WHEN** the unified YAML config contains an invalid `aliases` entry (e.g., self-alias)
- **THEN** the sub-command SHALL exit with a non-zero status AND no backup zone file SHALL be read


<!-- @trace
source: prune-redundant-backup-records
updated: 2026-05-04
code:
  - internal/transfer/axfr.go
  - internal/ephemeral/store.go
  - internal/prunebackup/include.go
  - internal/prunebackup/lexer.go
  - cmd/shadowdns/prune_backup.go
  - testdata/integration/shadowdns.yaml
  - internal/server/build.go
  - packaging/shadowdns.yaml.example
  - internal/server/server.go
  - internal/alias/rewrite.go
  - internal/server/handler.go
  - internal/prunebackup/rewrite.go
  - .release-please-manifest.json
  - internal/prunebackup/apply.go
  - internal/prunebackup/doc.go
  - internal/prunebackup/diff.go
  - CHANGELOG.md
  - README.md
  - internal/api/server.go
  - internal/zone/zone.go
  - cmd/shadowdns/main.go
  - internal/dnsutil/dnsutil.go
  - internal/shadowdnscfg/config.go
  - internal/alias/override.go
  - internal/zone/parser.go
  - internal/config/aliases.go
  - internal/prunebackup/prunebackup.go
tests:
  - internal/prunebackup/diff_test.go
  - test/integration/stress_ceiling_test.go
  - internal/zone/zone_test.go
  - test/integration/stress_shared_bucket_test.go
  - internal/config/aliases_test.go
  - internal/zone/parser_test.go
  - internal/alias/rewrite_anywhere_test.go
  - internal/prunebackup/apply_test.go
  - internal/server/handler_ephemeral_test.go
  - internal/ephemeral/store_test.go
  - test/integration/reload_diff_test.go
  - internal/prunebackup/prunebackup_test.go
  - internal/prunebackup/include_test.go
  - internal/server/build_test.go
  - internal/server/handler_test.go
  - internal/prunebackup/rewrite_test.go
  - internal/server/server_test.go
  - internal/alias/rewrite_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/shadowdnscfg/config_test.go
  - test/integration/case_preservation_test.go
  - test/integration/alias_rdata_rewrite_test.go
  - internal/alias/override_test.go
  - test/integration/compression_budget_test.go
  - test/integration/ephemeral_overrides_cname_test.go
  - test/integration/listenon_test.go
  - test/integration/helpers_test.go
  - test/integration/prune_backup_test.go
  - internal/prunebackup/lexer_test.go
  - cmd/shadowdns/prune_backup_test.go
  - internal/transfer/axfr_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/axfr_test.go
-->

### Requirement: Iterate backup zones per view

For each view declared in `named.conf`, the sub-command SHALL enumerate zones whose origin appears as a backup in the alias map, pair each with its aliased root zone in the same view, and process each `(view, backup zone)` pair independently.

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


<!-- @trace
source: prune-redundant-backup-records
updated: 2026-05-04
code:
  - internal/transfer/axfr.go
  - internal/ephemeral/store.go
  - internal/prunebackup/include.go
  - internal/prunebackup/lexer.go
  - cmd/shadowdns/prune_backup.go
  - testdata/integration/shadowdns.yaml
  - internal/server/build.go
  - packaging/shadowdns.yaml.example
  - internal/server/server.go
  - internal/alias/rewrite.go
  - internal/server/handler.go
  - internal/prunebackup/rewrite.go
  - .release-please-manifest.json
  - internal/prunebackup/apply.go
  - internal/prunebackup/doc.go
  - internal/prunebackup/diff.go
  - CHANGELOG.md
  - README.md
  - internal/api/server.go
  - internal/zone/zone.go
  - cmd/shadowdns/main.go
  - internal/dnsutil/dnsutil.go
  - internal/shadowdnscfg/config.go
  - internal/alias/override.go
  - internal/zone/parser.go
  - internal/config/aliases.go
  - internal/prunebackup/prunebackup.go
tests:
  - internal/prunebackup/diff_test.go
  - test/integration/stress_ceiling_test.go
  - internal/zone/zone_test.go
  - test/integration/stress_shared_bucket_test.go
  - internal/config/aliases_test.go
  - internal/zone/parser_test.go
  - internal/alias/rewrite_anywhere_test.go
  - internal/prunebackup/apply_test.go
  - internal/server/handler_ephemeral_test.go
  - internal/ephemeral/store_test.go
  - test/integration/reload_diff_test.go
  - internal/prunebackup/prunebackup_test.go
  - internal/prunebackup/include_test.go
  - internal/server/build_test.go
  - internal/server/handler_test.go
  - internal/prunebackup/rewrite_test.go
  - internal/server/server_test.go
  - internal/alias/rewrite_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/shadowdnscfg/config_test.go
  - test/integration/case_preservation_test.go
  - test/integration/alias_rdata_rewrite_test.go
  - internal/alias/override_test.go
  - test/integration/compression_budget_test.go
  - test/integration/ephemeral_overrides_cname_test.go
  - test/integration/listenon_test.go
  - test/integration/helpers_test.go
  - test/integration/prune_backup_test.go
  - internal/prunebackup/lexer_test.go
  - cmd/shadowdns/prune_backup_test.go
  - internal/transfer/axfr_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/axfr_test.go
-->


<!-- @trace
source: prune-without-root-and-discard-summary
updated: 2026-05-05
code:
  - internal/prunebackup/prunebackup.go
  - internal/prunebackup/diff.go
  - internal/shadowdnscfg/config.go
  - testdata/integration/shadowdns.yaml
  - testdata/integration/master/example.com_view-th.fwd
  - cmd/shadowdns/prune_backup.go
  - packaging/shadowdns.yaml.example
  - testdata/integration/master/example.com_view-other.fwd
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

### Requirement: Merge main file and $include files before diffing

For each backup zone, the sub-command SHALL recursively expand `$include` directives, producing a single merged list of RRs where each RR is annotated with its source file path and 1-based line range. `$include` path resolution SHALL follow the `directory` option from `named.conf`, matching the loader's current behavior.

#### Scenario: included fragment contributes to the merged set

- **WHEN** a backup main file contains `$include "backup.example_overrides"` AND that fragment contributes a `_sip._tcp IN SRV 10 5 5060 sip-backup.example.net.` record
- **THEN** the merged RR list SHALL contain that SRV record with source annotation pointing at `backup.example_overrides` with the correct line range

#### Scenario: relative include path uses named.conf directory

- **WHEN** `named.conf` declares `directory "/var/lib/shadowdns/zones"` AND a zone file contains `$include "overrides.fwd"`
- **THEN** the include target SHALL be resolved as `/var/lib/shadowdns/zones/overrides.fwd`


<!-- @trace
source: prune-redundant-backup-records
updated: 2026-05-04
code:
  - internal/transfer/axfr.go
  - internal/ephemeral/store.go
  - internal/prunebackup/include.go
  - internal/prunebackup/lexer.go
  - cmd/shadowdns/prune_backup.go
  - testdata/integration/shadowdns.yaml
  - internal/server/build.go
  - packaging/shadowdns.yaml.example
  - internal/server/server.go
  - internal/alias/rewrite.go
  - internal/server/handler.go
  - internal/prunebackup/rewrite.go
  - .release-please-manifest.json
  - internal/prunebackup/apply.go
  - internal/prunebackup/doc.go
  - internal/prunebackup/diff.go
  - CHANGELOG.md
  - README.md
  - internal/api/server.go
  - internal/zone/zone.go
  - cmd/shadowdns/main.go
  - internal/dnsutil/dnsutil.go
  - internal/shadowdnscfg/config.go
  - internal/alias/override.go
  - internal/zone/parser.go
  - internal/config/aliases.go
  - internal/prunebackup/prunebackup.go
tests:
  - internal/prunebackup/diff_test.go
  - test/integration/stress_ceiling_test.go
  - internal/zone/zone_test.go
  - test/integration/stress_shared_bucket_test.go
  - internal/config/aliases_test.go
  - internal/zone/parser_test.go
  - internal/alias/rewrite_anywhere_test.go
  - internal/prunebackup/apply_test.go
  - internal/server/handler_ephemeral_test.go
  - internal/ephemeral/store_test.go
  - test/integration/reload_diff_test.go
  - internal/prunebackup/prunebackup_test.go
  - internal/prunebackup/include_test.go
  - internal/server/build_test.go
  - internal/server/handler_test.go
  - internal/prunebackup/rewrite_test.go
  - internal/server/server_test.go
  - internal/alias/rewrite_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/shadowdnscfg/config_test.go
  - test/integration/case_preservation_test.go
  - test/integration/alias_rdata_rewrite_test.go
  - internal/alias/override_test.go
  - test/integration/compression_budget_test.go
  - test/integration/ephemeral_overrides_cname_test.go
  - test/integration/listenon_test.go
  - test/integration/helpers_test.go
  - test/integration/prune_backup_test.go
  - internal/prunebackup/lexer_test.go
  - cmd/shadowdns/prune_backup_test.go
  - internal/transfer/axfr_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/axfr_test.go
-->

### Requirement: Determine deletion candidates using RRSet-level rules

For every `(owner, type)` RRSet present in the merged backup zone, the sub-command SHALL classify it as follows:

1. If `type` is SOA, the RRSet SHALL be retained.
2. If `type` is NS AND `owner` equals the backup zone origin, the RRSet SHALL be retained.
3. Otherwise, if `type` is not in the overridable set {TXT, MX, SRV}, the entire RRSet SHALL be marked for deletion.
4. Otherwise (overridable type), the RRSet SHALL be marked for deletion only when the backup RRSet equals the root RRSet at the same `(rewritten-owner, type)` as sets of `(class, rdata)` tuples, ignoring TTL and ignoring order. Any difference — including one RR more, one RR fewer, or any differing rdata — SHALL cause the entire RRSet to be retained.

#### Scenario: A record at a non-apex owner is deleted

- **WHEN** the backup zone contains `www IN A 192.0.2.10` AND the type A is not in the overridable set
- **THEN** the `www A` RRSet SHALL be marked for deletion regardless of whether the root zone has the same record

#### Scenario: apex NS is retained

- **WHEN** the backup zone origin is `backup.example.` AND the file contains `@ IN NS ns1.backup.example.`
- **THEN** the `@ NS` RRSet SHALL be retained

#### Scenario: identical MX RRSet is deleted

- **WHEN** the backup RRSet at `mail.backup.example.` is `{MX 10 a.example.net., MX 20 b.example.net., MX 30 c.example.net.}` AND the root RRSet at `mail.example.com.` is the same three MX records (in any order, with any TTL)
- **THEN** the backup `mail MX` RRSet SHALL be marked for deletion

##### Example: RRSet comparison outcomes

| Backup RRSet at (owner, type) | Root RRSet at (rewritten owner, type) | Outcome |
|---|---|---|
| {MX 10 a, MX 20 b, MX 30 c} | {MX 10 a, MX 20 b, MX 30 c} | Delete entire RRSet |
| {MX 10 a, MX 20 b} | {MX 10 a, MX 20 b, MX 30 c} | Retain entire RRSet (backup shadows) |
| {MX 10 a, MX 20 b} | {MX 10 a, MX 20 b} | Delete entire RRSet |
| {MX 10 a} | {MX 10 z} | Retain entire RRSet (rdata differs) |
| {TXT "v=spf1 a -all"} | {} (no TXT at root) | Retain entire RRSet |
| {A 192.0.2.10} | any | Delete entire RRSet (type not overridable) |

#### Scenario: sub-delegation NS below apex is deleted

- **WHEN** the backup zone origin is `backup.example.` AND the file contains `child IN NS ns.other.` (owner is `child.backup.example.`, not the apex)
- **THEN** the `child NS` RRSet SHALL be marked for deletion because NS below apex is not in the overridable set and is not apex-NS


<!-- @trace
source: prune-redundant-backup-records
updated: 2026-05-04
code:
  - internal/transfer/axfr.go
  - internal/ephemeral/store.go
  - internal/prunebackup/include.go
  - internal/prunebackup/lexer.go
  - cmd/shadowdns/prune_backup.go
  - testdata/integration/shadowdns.yaml
  - internal/server/build.go
  - packaging/shadowdns.yaml.example
  - internal/server/server.go
  - internal/alias/rewrite.go
  - internal/server/handler.go
  - internal/prunebackup/rewrite.go
  - .release-please-manifest.json
  - internal/prunebackup/apply.go
  - internal/prunebackup/doc.go
  - internal/prunebackup/diff.go
  - CHANGELOG.md
  - README.md
  - internal/api/server.go
  - internal/zone/zone.go
  - cmd/shadowdns/main.go
  - internal/dnsutil/dnsutil.go
  - internal/shadowdnscfg/config.go
  - internal/alias/override.go
  - internal/zone/parser.go
  - internal/config/aliases.go
  - internal/prunebackup/prunebackup.go
tests:
  - internal/prunebackup/diff_test.go
  - test/integration/stress_ceiling_test.go
  - internal/zone/zone_test.go
  - test/integration/stress_shared_bucket_test.go
  - internal/config/aliases_test.go
  - internal/zone/parser_test.go
  - internal/alias/rewrite_anywhere_test.go
  - internal/prunebackup/apply_test.go
  - internal/server/handler_ephemeral_test.go
  - internal/ephemeral/store_test.go
  - test/integration/reload_diff_test.go
  - internal/prunebackup/prunebackup_test.go
  - internal/prunebackup/include_test.go
  - internal/server/build_test.go
  - internal/server/handler_test.go
  - internal/prunebackup/rewrite_test.go
  - internal/server/server_test.go
  - internal/alias/rewrite_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/shadowdnscfg/config_test.go
  - test/integration/case_preservation_test.go
  - test/integration/alias_rdata_rewrite_test.go
  - internal/alias/override_test.go
  - test/integration/compression_budget_test.go
  - test/integration/ephemeral_overrides_cname_test.go
  - test/integration/listenon_test.go
  - test/integration/helpers_test.go
  - test/integration/prune_backup_test.go
  - internal/prunebackup/lexer_test.go
  - cmd/shadowdns/prune_backup_test.go
  - internal/transfer/axfr_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/axfr_test.go
-->

### Requirement: Delete records by line range while preserving formatting

When writing is requested, the sub-command SHALL perform line-based deletion: for every RR marked for deletion, the corresponding line range in its source file SHALL be removed. Retained RR lines SHALL be written back byte-identical to their original form, including any trailing `;` comment. Directive lines (`$TTL`, `$ORIGIN`, `$INCLUDE`, `$GENERATE`) SHALL be retained regardless of deletion decisions on surrounding records. Lines consisting only of a `;` comment and blank lines SHALL be removed even when no RR deletion occurs on that line.

#### Scenario: relative owner name is preserved

- **WHEN** a retained RR line in the original file is `www   IN A   192.0.2.1 ; primary frontend`
- **THEN** after prune, the same line SHALL appear byte-identical in the output file — owner SHALL NOT be expanded to `www.backup.example.` and the trailing comment SHALL be preserved

#### Scenario: $include directive is retained even if its target becomes empty

- **WHEN** the main file contains `$include "backup.example_overrides"` AND after prune the `backup.example_overrides` file contains no remaining RRs
- **THEN** the `$include` line in the main file SHALL be retained

#### Scenario: multi-line RR enclosed in parentheses is treated as a single range

- **WHEN** a retained SOA spans multiple physical lines via `( ... )` parenthesization
- **THEN** all lines of the SOA block SHALL be retained as a contiguous range

#### Scenario: blank lines and stand-alone comments are removed

- **WHEN** the original file contains blank lines and lines whose only non-whitespace content begins with `;`
- **THEN** after prune, those lines SHALL NOT appear in the output file


<!-- @trace
source: prune-redundant-backup-records
updated: 2026-05-04
code:
  - internal/transfer/axfr.go
  - internal/ephemeral/store.go
  - internal/prunebackup/include.go
  - internal/prunebackup/lexer.go
  - cmd/shadowdns/prune_backup.go
  - testdata/integration/shadowdns.yaml
  - internal/server/build.go
  - packaging/shadowdns.yaml.example
  - internal/server/server.go
  - internal/alias/rewrite.go
  - internal/server/handler.go
  - internal/prunebackup/rewrite.go
  - .release-please-manifest.json
  - internal/prunebackup/apply.go
  - internal/prunebackup/doc.go
  - internal/prunebackup/diff.go
  - CHANGELOG.md
  - README.md
  - internal/api/server.go
  - internal/zone/zone.go
  - cmd/shadowdns/main.go
  - internal/dnsutil/dnsutil.go
  - internal/shadowdnscfg/config.go
  - internal/alias/override.go
  - internal/zone/parser.go
  - internal/config/aliases.go
  - internal/prunebackup/prunebackup.go
tests:
  - internal/prunebackup/diff_test.go
  - test/integration/stress_ceiling_test.go
  - internal/zone/zone_test.go
  - test/integration/stress_shared_bucket_test.go
  - internal/config/aliases_test.go
  - internal/zone/parser_test.go
  - internal/alias/rewrite_anywhere_test.go
  - internal/prunebackup/apply_test.go
  - internal/server/handler_ephemeral_test.go
  - internal/ephemeral/store_test.go
  - test/integration/reload_diff_test.go
  - internal/prunebackup/prunebackup_test.go
  - internal/prunebackup/include_test.go
  - internal/server/build_test.go
  - internal/server/handler_test.go
  - internal/prunebackup/rewrite_test.go
  - internal/server/server_test.go
  - internal/alias/rewrite_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/shadowdnscfg/config_test.go
  - test/integration/case_preservation_test.go
  - test/integration/alias_rdata_rewrite_test.go
  - internal/alias/override_test.go
  - test/integration/compression_budget_test.go
  - test/integration/ephemeral_overrides_cname_test.go
  - test/integration/listenon_test.go
  - test/integration/helpers_test.go
  - test/integration/prune_backup_test.go
  - internal/prunebackup/lexer_test.go
  - cmd/shadowdns/prune_backup_test.go
  - internal/transfer/axfr_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/axfr_test.go
-->

### Requirement: Default to dry-run; require --apply to modify files

The sub-command SHALL default to dry-run mode. In dry-run mode it SHALL print, for each RR marked for deletion, the source file path, 1-based line range, owner, type, and rdata, and SHALL NOT modify any file. When `--apply` is supplied, the sub-command SHALL perform the writes described in the next requirement.

#### Scenario: dry-run prints planned deletions without writing

- **WHEN** the sub-command is run without `--apply` AND deletion candidates exist
- **THEN** the output SHALL list each candidate with `file:line-range owner type rdata` AND no zone file SHALL be modified AND no `.bak` file SHALL be created

#### Scenario: dry-run with no candidates reports clean state

- **WHEN** the sub-command is run without `--apply` AND no deletion candidates exist
- **THEN** the output SHALL indicate no redundant records were found AND exit status SHALL be 0


<!-- @trace
source: prune-redundant-backup-records
updated: 2026-05-04
code:
  - internal/transfer/axfr.go
  - internal/ephemeral/store.go
  - internal/prunebackup/include.go
  - internal/prunebackup/lexer.go
  - cmd/shadowdns/prune_backup.go
  - testdata/integration/shadowdns.yaml
  - internal/server/build.go
  - packaging/shadowdns.yaml.example
  - internal/server/server.go
  - internal/alias/rewrite.go
  - internal/server/handler.go
  - internal/prunebackup/rewrite.go
  - .release-please-manifest.json
  - internal/prunebackup/apply.go
  - internal/prunebackup/doc.go
  - internal/prunebackup/diff.go
  - CHANGELOG.md
  - README.md
  - internal/api/server.go
  - internal/zone/zone.go
  - cmd/shadowdns/main.go
  - internal/dnsutil/dnsutil.go
  - internal/shadowdnscfg/config.go
  - internal/alias/override.go
  - internal/zone/parser.go
  - internal/config/aliases.go
  - internal/prunebackup/prunebackup.go
tests:
  - internal/prunebackup/diff_test.go
  - test/integration/stress_ceiling_test.go
  - internal/zone/zone_test.go
  - test/integration/stress_shared_bucket_test.go
  - internal/config/aliases_test.go
  - internal/zone/parser_test.go
  - internal/alias/rewrite_anywhere_test.go
  - internal/prunebackup/apply_test.go
  - internal/server/handler_ephemeral_test.go
  - internal/ephemeral/store_test.go
  - test/integration/reload_diff_test.go
  - internal/prunebackup/prunebackup_test.go
  - internal/prunebackup/include_test.go
  - internal/server/build_test.go
  - internal/server/handler_test.go
  - internal/prunebackup/rewrite_test.go
  - internal/server/server_test.go
  - internal/alias/rewrite_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/shadowdnscfg/config_test.go
  - test/integration/case_preservation_test.go
  - test/integration/alias_rdata_rewrite_test.go
  - internal/alias/override_test.go
  - test/integration/compression_budget_test.go
  - test/integration/ephemeral_overrides_cname_test.go
  - test/integration/listenon_test.go
  - test/integration/helpers_test.go
  - test/integration/prune_backup_test.go
  - internal/prunebackup/lexer_test.go
  - cmd/shadowdns/prune_backup_test.go
  - internal/transfer/axfr_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/axfr_test.go
-->

### Requirement: Atomic per-file write with .bak backup on --apply

When `--apply` is supplied, for every file with at least one deletion, the sub-command SHALL:

1. Rename the original file to `<original-path>.bak`, overwriting any pre-existing `.bak` at that path, emitting an INFO log message when overwriting.
2. Write the pruned content to a temporary file in the same directory, fsync it, then rename it to the original path.
3. If any file's write step fails, the sub-command SHALL stop processing further files AND SHALL exit with a non-zero status, leaving already-written files in their post-apply state and their `.bak` backups intact so the operator can restore manually.

Files with zero deletions SHALL NOT be touched and SHALL NOT produce a `.bak`.

#### Scenario: successful apply creates .bak and updates original

- **WHEN** `--apply` runs against a backup zone file with pending deletions
- **THEN** the original file SHALL be renamed to `<path>.bak` AND the original path SHALL contain the pruned content AND both files SHALL be present on disk after the run

#### Scenario: apply preserves untouched files

- **WHEN** `--apply` runs across multiple files and one file has zero deletions
- **THEN** that file SHALL NOT be renamed AND no `<path>.bak` SHALL be created for it

#### Scenario: pre-existing .bak is overwritten with a log notice

- **WHEN** `--apply` runs AND `<path>.bak` already exists
- **THEN** the existing `.bak` SHALL be overwritten AND an INFO log entry SHALL record the overwrite with the file path


<!-- @trace
source: prune-redundant-backup-records
updated: 2026-05-04
code:
  - internal/transfer/axfr.go
  - internal/ephemeral/store.go
  - internal/prunebackup/include.go
  - internal/prunebackup/lexer.go
  - cmd/shadowdns/prune_backup.go
  - testdata/integration/shadowdns.yaml
  - internal/server/build.go
  - packaging/shadowdns.yaml.example
  - internal/server/server.go
  - internal/alias/rewrite.go
  - internal/server/handler.go
  - internal/prunebackup/rewrite.go
  - .release-please-manifest.json
  - internal/prunebackup/apply.go
  - internal/prunebackup/doc.go
  - internal/prunebackup/diff.go
  - CHANGELOG.md
  - README.md
  - internal/api/server.go
  - internal/zone/zone.go
  - cmd/shadowdns/main.go
  - internal/dnsutil/dnsutil.go
  - internal/shadowdnscfg/config.go
  - internal/alias/override.go
  - internal/zone/parser.go
  - internal/config/aliases.go
  - internal/prunebackup/prunebackup.go
tests:
  - internal/prunebackup/diff_test.go
  - test/integration/stress_ceiling_test.go
  - internal/zone/zone_test.go
  - test/integration/stress_shared_bucket_test.go
  - internal/config/aliases_test.go
  - internal/zone/parser_test.go
  - internal/alias/rewrite_anywhere_test.go
  - internal/prunebackup/apply_test.go
  - internal/server/handler_ephemeral_test.go
  - internal/ephemeral/store_test.go
  - test/integration/reload_diff_test.go
  - internal/prunebackup/prunebackup_test.go
  - internal/prunebackup/include_test.go
  - internal/server/build_test.go
  - internal/server/handler_test.go
  - internal/prunebackup/rewrite_test.go
  - internal/server/server_test.go
  - internal/alias/rewrite_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/shadowdnscfg/config_test.go
  - test/integration/case_preservation_test.go
  - test/integration/alias_rdata_rewrite_test.go
  - internal/alias/override_test.go
  - test/integration/compression_budget_test.go
  - test/integration/ephemeral_overrides_cname_test.go
  - test/integration/listenon_test.go
  - test/integration/helpers_test.go
  - test/integration/prune_backup_test.go
  - internal/prunebackup/lexer_test.go
  - cmd/shadowdns/prune_backup_test.go
  - internal/transfer/axfr_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/axfr_test.go
-->

### Requirement: Exit code semantics

The sub-command SHALL exit with status 0 when: a dry-run completes (even when deletion candidates are found), or an `--apply` completes and every targeted file has been written successfully. The sub-command SHALL exit with a non-zero status only when: a required flag is missing, `named.conf` or the unified config fails to load or validate, a zone file fails to parse, or an `--apply` write step fails.

#### Scenario: dry-run reporting deletions exits 0

- **WHEN** a dry-run completes and reports N>0 deletion candidates
- **THEN** the exit status SHALL be 0

#### Scenario: parse failure of a zone file exits non-zero

- **WHEN** a backup zone file contains syntactically invalid DNS records
- **THEN** the sub-command SHALL exit with a non-zero status AND SHALL NOT proceed to apply writes


<!-- @trace
source: prune-redundant-backup-records
updated: 2026-05-04
code:
  - internal/transfer/axfr.go
  - internal/ephemeral/store.go
  - internal/prunebackup/include.go
  - internal/prunebackup/lexer.go
  - cmd/shadowdns/prune_backup.go
  - testdata/integration/shadowdns.yaml
  - internal/server/build.go
  - packaging/shadowdns.yaml.example
  - internal/server/server.go
  - internal/alias/rewrite.go
  - internal/server/handler.go
  - internal/prunebackup/rewrite.go
  - .release-please-manifest.json
  - internal/prunebackup/apply.go
  - internal/prunebackup/doc.go
  - internal/prunebackup/diff.go
  - CHANGELOG.md
  - README.md
  - internal/api/server.go
  - internal/zone/zone.go
  - cmd/shadowdns/main.go
  - internal/dnsutil/dnsutil.go
  - internal/shadowdnscfg/config.go
  - internal/alias/override.go
  - internal/zone/parser.go
  - internal/config/aliases.go
  - internal/prunebackup/prunebackup.go
tests:
  - internal/prunebackup/diff_test.go
  - test/integration/stress_ceiling_test.go
  - internal/zone/zone_test.go
  - test/integration/stress_shared_bucket_test.go
  - internal/config/aliases_test.go
  - internal/zone/parser_test.go
  - internal/alias/rewrite_anywhere_test.go
  - internal/prunebackup/apply_test.go
  - internal/server/handler_ephemeral_test.go
  - internal/ephemeral/store_test.go
  - test/integration/reload_diff_test.go
  - internal/prunebackup/prunebackup_test.go
  - internal/prunebackup/include_test.go
  - internal/server/build_test.go
  - internal/server/handler_test.go
  - internal/prunebackup/rewrite_test.go
  - internal/server/server_test.go
  - internal/alias/rewrite_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/shadowdnscfg/config_test.go
  - test/integration/case_preservation_test.go
  - test/integration/alias_rdata_rewrite_test.go
  - internal/alias/override_test.go
  - test/integration/compression_budget_test.go
  - test/integration/ephemeral_overrides_cname_test.go
  - test/integration/listenon_test.go
  - test/integration/helpers_test.go
  - test/integration/prune_backup_test.go
  - internal/prunebackup/lexer_test.go
  - cmd/shadowdns/prune_backup_test.go
  - internal/transfer/axfr_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/axfr_test.go
-->

### Requirement: $GENERATE directives are opaque

If a backup zone main file or any `$include` target contains a `$GENERATE` directive, the sub-command SHALL retain the `$GENERATE` line verbatim AND SHALL NOT attempt to expand or diff the records it would produce. The sub-command SHALL emit an INFO log entry naming the file so the operator can review manually.

#### Scenario: $GENERATE line is preserved

- **WHEN** a file contains `$GENERATE 1-10 dyn$ IN A 192.0.2.$`
- **THEN** that directive line SHALL be retained in the output AND an INFO log entry SHALL name the file as containing an opaque `$GENERATE`

## Requirements

<!-- @trace
source: prune-redundant-backup-records
updated: 2026-05-04
code:
  - internal/transfer/axfr.go
  - internal/ephemeral/store.go
  - internal/prunebackup/include.go
  - internal/prunebackup/lexer.go
  - cmd/shadowdns/prune_backup.go
  - testdata/integration/shadowdns.yaml
  - internal/server/build.go
  - packaging/shadowdns.yaml.example
  - internal/server/server.go
  - internal/alias/rewrite.go
  - internal/server/handler.go
  - internal/prunebackup/rewrite.go
  - .release-please-manifest.json
  - internal/prunebackup/apply.go
  - internal/prunebackup/doc.go
  - internal/prunebackup/diff.go
  - CHANGELOG.md
  - README.md
  - internal/api/server.go
  - internal/zone/zone.go
  - cmd/shadowdns/main.go
  - internal/dnsutil/dnsutil.go
  - internal/shadowdnscfg/config.go
  - internal/alias/override.go
  - internal/zone/parser.go
  - internal/config/aliases.go
  - internal/prunebackup/prunebackup.go
tests:
  - internal/prunebackup/diff_test.go
  - test/integration/stress_ceiling_test.go
  - internal/zone/zone_test.go
  - test/integration/stress_shared_bucket_test.go
  - internal/config/aliases_test.go
  - internal/zone/parser_test.go
  - internal/alias/rewrite_anywhere_test.go
  - internal/prunebackup/apply_test.go
  - internal/server/handler_ephemeral_test.go
  - internal/ephemeral/store_test.go
  - test/integration/reload_diff_test.go
  - internal/prunebackup/prunebackup_test.go
  - internal/prunebackup/include_test.go
  - internal/server/build_test.go
  - internal/server/handler_test.go
  - internal/prunebackup/rewrite_test.go
  - internal/server/server_test.go
  - internal/alias/rewrite_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/shadowdnscfg/config_test.go
  - test/integration/case_preservation_test.go
  - test/integration/alias_rdata_rewrite_test.go
  - internal/alias/override_test.go
  - test/integration/compression_budget_test.go
  - test/integration/ephemeral_overrides_cname_test.go
  - test/integration/listenon_test.go
  - test/integration/helpers_test.go
  - test/integration/prune_backup_test.go
  - internal/prunebackup/lexer_test.go
  - cmd/shadowdns/prune_backup_test.go
  - internal/transfer/axfr_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/axfr_test.go
-->

### Requirement: Provide a prune-backup sub-command

The `shadowdns` binary SHALL expose a `prune-backup` sub-command that runs an offline scan of backup zone files, compares each backup zone's records against its aliased root zone, and reports or removes redundant records. The sub-command SHALL NOT open any network sockets, SHALL NOT send SIGHUP, and SHALL NOT mutate the running server's state.

#### Scenario: sub-command is registered

- **WHEN** an operator runs `shadowdns prune-backup --help`
- **THEN** the help output SHALL list `--named-conf`, `--config`, `--apply` flags, AND the invocation SHALL exit with status 0 without attempting to bind any UDP/TCP listener

#### Scenario: required flags are enforced

- **WHEN** an operator runs `shadowdns prune-backup` without `--named-conf` or without `--config`
- **THEN** the sub-command SHALL exit with a non-zero status AND SHALL print a message naming the missing required flag

---
### Requirement: Load config and named.conf identically to the server

The sub-command SHALL load `named.conf` via the existing config-loader path and the unified YAML config via `internal/shadowdnscfg`, using the same validation rules as the server startup path. Any load or validation failure SHALL abort the sub-command with a non-zero exit status before any zone file is read or modified.

#### Scenario: named.conf load failure aborts prune

- **WHEN** `--named-conf` points to a file that fails to parse
- **THEN** the sub-command SHALL exit with a non-zero status AND no backup zone file SHALL be read

#### Scenario: unified config validation failure aborts prune

- **WHEN** the unified YAML config contains an invalid `aliases` entry (e.g., self-alias)
- **THEN** the sub-command SHALL exit with a non-zero status AND no backup zone file SHALL be read

---
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

---
### Requirement: Merge main file and $include files before diffing

For each backup zone, the sub-command SHALL recursively expand `$include` directives, producing a single merged list of RRs where each RR is annotated with its source file path and 1-based line range. `$include` path resolution SHALL follow the `directory` option from `named.conf`, matching the loader's current behavior.

#### Scenario: included fragment contributes to the merged set

- **WHEN** a backup main file contains `$include "backup.example_overrides"` AND that fragment contributes a `_sip._tcp IN SRV 10 5 5060 sip-backup.example.net.` record
- **THEN** the merged RR list SHALL contain that SRV record with source annotation pointing at `backup.example_overrides` with the correct line range

#### Scenario: relative include path uses named.conf directory

- **WHEN** `named.conf` declares `directory "/var/lib/shadowdns/zones"` AND a zone file contains `$include "overrides.fwd"`
- **THEN** the include target SHALL be resolved as `/var/lib/shadowdns/zones/overrides.fwd`

---
### Requirement: Determine deletion candidates using RRSet-level rules

For every `(owner, type)` RRSet present in the merged backup zone, the sub-command SHALL classify it as follows:

1. If `type` is SOA, the RRSet SHALL be retained.
2. If `type` is NS AND `owner` equals the backup zone origin, the RRSet SHALL be retained.
3. Otherwise, if `type` is not in the overridable set {TXT, MX, SRV}, the entire RRSet SHALL be marked for deletion.
4. Otherwise (overridable type), the RRSet SHALL be marked for deletion only when the backup RRSet equals the root RRSet at the same `(rewritten-owner, type)` as sets of `(class, rdata)` tuples, ignoring TTL and ignoring order. Any difference — including one RR more, one RR fewer, or any differing rdata — SHALL cause the entire RRSet to be retained.

#### Scenario: A record at a non-apex owner is deleted

- **WHEN** the backup zone contains `www IN A 192.0.2.10` AND the type A is not in the overridable set
- **THEN** the `www A` RRSet SHALL be marked for deletion regardless of whether the root zone has the same record

#### Scenario: apex NS is retained

- **WHEN** the backup zone origin is `backup.example.` AND the file contains `@ IN NS ns1.backup.example.`
- **THEN** the `@ NS` RRSet SHALL be retained

#### Scenario: identical MX RRSet is deleted

- **WHEN** the backup RRSet at `mail.backup.example.` is `{MX 10 a.example.net., MX 20 b.example.net., MX 30 c.example.net.}` AND the root RRSet at `mail.example.com.` is the same three MX records (in any order, with any TTL)
- **THEN** the backup `mail MX` RRSet SHALL be marked for deletion

##### Example: RRSet comparison outcomes

| Backup RRSet at (owner, type) | Root RRSet at (rewritten owner, type) | Outcome |
|---|---|---|
| {MX 10 a, MX 20 b, MX 30 c} | {MX 10 a, MX 20 b, MX 30 c} | Delete entire RRSet |
| {MX 10 a, MX 20 b} | {MX 10 a, MX 20 b, MX 30 c} | Retain entire RRSet (backup shadows) |
| {MX 10 a, MX 20 b} | {MX 10 a, MX 20 b} | Delete entire RRSet |
| {MX 10 a} | {MX 10 z} | Retain entire RRSet (rdata differs) |
| {TXT "v=spf1 a -all"} | {} (no TXT at root) | Retain entire RRSet |
| {A 192.0.2.10} | any | Delete entire RRSet (type not overridable) |

#### Scenario: sub-delegation NS below apex is deleted

- **WHEN** the backup zone origin is `backup.example.` AND the file contains `child IN NS ns.other.` (owner is `child.backup.example.`, not the apex)
- **THEN** the `child NS` RRSet SHALL be marked for deletion because NS below apex is not in the overridable set and is not apex-NS

---
### Requirement: Delete records by line range while preserving formatting

When writing is requested, the sub-command SHALL perform line-based deletion: for every RR marked for deletion, the corresponding line range in its source file SHALL be removed. Retained RR lines SHALL be written back byte-identical to their original form, including any trailing `;` comment. Directive lines (`$TTL`, `$ORIGIN`, `$INCLUDE`, `$GENERATE`) SHALL be retained regardless of deletion decisions on surrounding records. Lines consisting only of a `;` comment and blank lines SHALL be removed even when no RR deletion occurs on that line.

#### Scenario: relative owner name is preserved

- **WHEN** a retained RR line in the original file is `www   IN A   192.0.2.1 ; primary frontend`
- **THEN** after prune, the same line SHALL appear byte-identical in the output file — owner SHALL NOT be expanded to `www.backup.example.` and the trailing comment SHALL be preserved

#### Scenario: $include directive is retained even if its target becomes empty

- **WHEN** the main file contains `$include "backup.example_overrides"` AND after prune the `backup.example_overrides` file contains no remaining RRs
- **THEN** the `$include` line in the main file SHALL be retained

#### Scenario: multi-line RR enclosed in parentheses is treated as a single range

- **WHEN** a retained SOA spans multiple physical lines via `( ... )` parenthesization
- **THEN** all lines of the SOA block SHALL be retained as a contiguous range

#### Scenario: blank lines and stand-alone comments are removed

- **WHEN** the original file contains blank lines and lines whose only non-whitespace content begins with `;`
- **THEN** after prune, those lines SHALL NOT appear in the output file

---
### Requirement: Default to dry-run; require --apply to modify files

The sub-command SHALL default to dry-run mode. In dry-run mode it SHALL print, for each RR marked for deletion, the source file path, 1-based line range, owner, type, and rdata, and SHALL NOT modify any file. When `--apply` is supplied, the sub-command SHALL perform the writes described in the next requirement.

#### Scenario: dry-run prints planned deletions without writing

- **WHEN** the sub-command is run without `--apply` AND deletion candidates exist
- **THEN** the output SHALL list each candidate with `file:line-range owner type rdata` AND no zone file SHALL be modified AND no `.bak` file SHALL be created

#### Scenario: dry-run with no candidates reports clean state

- **WHEN** the sub-command is run without `--apply` AND no deletion candidates exist
- **THEN** the output SHALL indicate no redundant records were found AND exit status SHALL be 0

---
### Requirement: Atomic per-file write with .bak backup on --apply

When `--apply` is supplied, for every file with at least one deletion, the sub-command SHALL:

1. Rename the original file to `<original-path>.bak`, overwriting any pre-existing `.bak` at that path, emitting an INFO log message when overwriting.
2. Write the pruned content to a temporary file in the same directory, fsync it, then rename it to the original path.
3. If any file's write step fails, the sub-command SHALL stop processing further files AND SHALL exit with a non-zero status, leaving already-written files in their post-apply state and their `.bak` backups intact so the operator can restore manually.

Files with zero deletions SHALL NOT be touched and SHALL NOT produce a `.bak`.

#### Scenario: successful apply creates .bak and updates original

- **WHEN** `--apply` runs against a backup zone file with pending deletions
- **THEN** the original file SHALL be renamed to `<path>.bak` AND the original path SHALL contain the pruned content AND both files SHALL be present on disk after the run

#### Scenario: apply preserves untouched files

- **WHEN** `--apply` runs across multiple files and one file has zero deletions
- **THEN** that file SHALL NOT be renamed AND no `<path>.bak` SHALL be created for it

#### Scenario: pre-existing .bak is overwritten with a log notice

- **WHEN** `--apply` runs AND `<path>.bak` already exists
- **THEN** the existing `.bak` SHALL be overwritten AND an INFO log entry SHALL record the overwrite with the file path

---
### Requirement: Exit code semantics

The sub-command SHALL exit with status 0 when: a dry-run completes (even when deletion candidates are found), or an `--apply` completes and every targeted file has been written successfully. The sub-command SHALL exit with a non-zero status only when: a required flag is missing, `named.conf` or the unified config fails to load or validate, a zone file fails to parse, or an `--apply` write step fails.

#### Scenario: dry-run reporting deletions exits 0

- **WHEN** a dry-run completes and reports N>0 deletion candidates
- **THEN** the exit status SHALL be 0

#### Scenario: parse failure of a zone file exits non-zero

- **WHEN** a backup zone file contains syntactically invalid DNS records
- **THEN** the sub-command SHALL exit with a non-zero status AND SHALL NOT proceed to apply writes

---
### Requirement: $GENERATE directives are opaque

If a backup zone main file or any `$include` target contains a `$GENERATE` directive, the sub-command SHALL retain the `$GENERATE` line verbatim AND SHALL NOT attempt to expand or diff the records it would produce. The sub-command SHALL emit an INFO log entry naming the file so the operator can review manually.

#### Scenario: $GENERATE line is preserved

- **WHEN** a file contains `$GENERATE 1-10 dyn$ IN A 192.0.2.$`
- **THEN** that directive line SHALL be retained in the output AND an INFO log entry SHALL name the file as containing an opaque `$GENERATE`

---
### Requirement: Process zone pairs as a streaming pipeline

The sub-command SHALL process each `(view, backup zone)` pair as an independent pipeline stage: plan, sort the pair's deletion list, emit dry-run output for that pair (or apply that pair's pruned files when `--apply` is supplied), then release the pair's intermediate plan structures before advancing to the next pair. The sub-command SHALL NOT retain the union of every pair's `Deletion` list or the union of every pair's pruned file contents in memory simultaneously.

#### Scenario: peak memory tracks single-pair work, not full job

- **WHEN** the sub-command runs across N pairs whose combined deletion count exceeds any single pair's count by orders of magnitude
- **THEN** the resident memory ceiling SHALL be proportional to the largest single pair plus a fixed overhead, AND SHALL NOT grow proportionally to the sum across all pairs

#### Scenario: per-pair release between pairs

- **WHEN** the sub-command completes plan generation, output, and (under `--apply`) writes for pair P
- **THEN** the in-memory `Deletion` list and pruned-file map for pair P SHALL become unreachable from the running goroutine BEFORE the sub-command begins planning pair P+1


<!-- @trace
source: stream-prune-backup-pairs
updated: 2026-05-04
code:
  - cmd/shadowdns/prune_backup.go
  - cmd/shadowdns/main.go
  - internal/zone/classify.go
tests:
  - cmd/shadowdns/main_test.go
  - internal/zone/classify_test.go
  - cmd/shadowdns/prune_backup_test.go
  - internal/server/server_test.go
-->

---
### Requirement: Deterministic dry-run output order across pairs and within each pair

The sub-command SHALL emit dry-run output with two-level deterministic ordering: pairs SHALL be processed in ascending `(view-name, backup-origin)` order, and within each pair the emitted lines SHALL be sorted ascending by `(source-file-path, start-line)`. The sub-command SHALL NOT reorder lines across pair boundaries; pair P's output SHALL appear in full before pair P+1's first line.

#### Scenario: two pairs produce contiguous, ordered blocks

- **WHEN** the sub-command processes pairs `(view-a, backup-1)` and `(view-a, backup-2)` with deletion candidates in each
- **THEN** every line for `(view-a, backup-1)` SHALL appear before any line for `(view-a, backup-2)` AND each block's lines SHALL be sorted ascending by `(file-path, start-line)`

##### Example: ordering across two pairs

| Pair processed | File:Line emitted | Position in stream |
|---|---|---|
| (view-a, backup-1) | /zones/b1.fwd:10-10 | 1 |
| (view-a, backup-1) | /zones/b1.fwd:20-20 | 2 |
| (view-a, backup-1) | /zones/b1_inc.fwd:5-5 | 3 |
| (view-a, backup-2) | /zones/b2.fwd:1-1 | 4 |
| (view-a, backup-2) | /zones/b2.fwd:7-7 | 5 |


<!-- @trace
source: stream-prune-backup-pairs
updated: 2026-05-04
code:
  - cmd/shadowdns/prune_backup.go
  - cmd/shadowdns/main.go
  - internal/zone/classify.go
tests:
  - cmd/shadowdns/main_test.go
  - internal/zone/classify_test.go
  - cmd/shadowdns/prune_backup_test.go
  - internal/server/server_test.go
-->

---
### Requirement: Apply writes flush per pair instead of batched at the end

When `--apply` is supplied, the sub-command SHALL invoke `ApplyAll` once per pair, immediately after that pair's plan is computed and dry-run output for the pair is emitted, BEFORE advancing to the next pair. The sub-command SHALL NOT defer all writes until every pair has been planned. The fail-stop semantics SHALL be preserved: any pair whose write step fails SHALL stop the sub-command immediately with a non-zero exit, leaving already-written files in their post-apply state and their `.bak` backups intact.

#### Scenario: write failure on pair K stops the run

- **WHEN** `--apply` runs across pairs P1 ... Pn AND the write step for pair Pk (1 < k ≤ n) fails
- **THEN** pairs P1 ... P(k-1) SHALL have their files rewritten on disk with corresponding `.bak` files present, AND pairs P(k+1) ... Pn SHALL NOT have been read or modified, AND the sub-command SHALL exit with a non-zero status

#### Scenario: parse failure on pair K stops the run before any later pair runs

- **WHEN** `--apply` runs across pairs P1 ... Pn AND a backup zone file in pair Pk fails to parse during plan generation
- **THEN** pairs P1 ... P(k-1) SHALL have already completed their writes on disk, AND pairs Pk ... Pn SHALL have produced no writes, AND the sub-command SHALL exit with a non-zero status


<!-- @trace
source: stream-prune-backup-pairs
updated: 2026-05-04
code:
  - cmd/shadowdns/prune_backup.go
  - cmd/shadowdns/main.go
  - internal/zone/classify.go
tests:
  - cmd/shadowdns/main_test.go
  - internal/zone/classify_test.go
  - cmd/shadowdns/prune_backup_test.go
  - internal/server/server_test.go
-->

---
### Requirement: Output writer flushes before exit

The sub-command SHALL wrap its dry-run output destination in a buffered writer with a buffer size of at least 64 KiB to coalesce per-line writes into batched syscalls. The sub-command SHALL flush the writer before any return path that signals completion or failure, so no candidate line is lost on either the success or the error exit.

#### Scenario: success exit emits every candidate

- **WHEN** the sub-command completes processing without error AND M > 0 deletion candidates were produced across all pairs
- **THEN** the output SHALL contain exactly M candidate lines AND the trailing line SHALL be flushed to the destination before the sub-command exits

#### Scenario: error exit preserves emitted lines

- **WHEN** the sub-command emits J > 0 candidate lines THEN encounters a fatal error before processing finishes
- **THEN** the J already-emitted lines SHALL be flushed to the destination AND SHALL NOT be lost as a side effect of buffering

<!-- @trace
source: stream-prune-backup-pairs
updated: 2026-05-04
code:
  - cmd/shadowdns/prune_backup.go
  - cmd/shadowdns/main.go
  - internal/zone/classify.go
tests:
  - cmd/shadowdns/main_test.go
  - internal/zone/classify_test.go
  - cmd/shadowdns/prune_backup_test.go
  - internal/server/server_test.go
-->