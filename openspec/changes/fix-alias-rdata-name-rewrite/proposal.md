## Problem

When a query is answered from a backup (alias) zone, the alias resolver rewrites the record's owner name into the backup namespace for every record type, but only rewrites RDATA name values for six record types (`CNAME`, `NS`, `MX`, `PTR`, `SRV`, `SOA`). Any other record type that carries a domain name inside its RDATA is emitted with the RDATA name left in the backend (root) namespace, while its owner name has already been rewritten to the backup namespace. This half-rewritten record discloses the real backend zone origin that the aliasing feature is designed to hide.

The query path applies no record-type allowlist, so an anonymous client can query these record types under a backup domain over UDP/TCP/53 or DoH and read the backend origin from the returned RDATA. Modern browsers routinely query `HTTPS` (type 65), so this is reachable in practice. The same rewrite primitive also feeds the synthesized alias `AXFR` path, so a zone transfer of an alias zone leaks the same way (that path is additionally gated by the allow-transfer ACL).

## Root Cause

The RDATA name rewrite helper in the alias package switches on record type and handles only `CNAME`/`NS`/`MX`/`PTR`/`SRV`/`SOA`; every other type falls through with its RDATA untouched. This is the only RDATA-name rewrite in the alias, zone, and transfer packages — there is no fallback. Meanwhile the owner-name rewrite is unconditional across all types, producing the owner-versus-RDATA asymmetry.

## Proposed Solution

Two complementary parts, both scoped to the alias resolver:

1. **Curated expansion of the RDATA name rewrite (primary fix).** Extend the RDATA name rewrite to cover the remaining record types whose RDATA carries an in-bailiwick domain name and that are meaningful alias targets: `HTTPS` (Target), `SVCB` (Target), `DNAME` (Target), `NAPTR` (Replacement), `RP` (Mbox and Txt), `KX` (Exchanger), `AFSDB` (Hostname), `PX` (Map822 and Mapx400), `RT` (Host). The existing six cases are unchanged. Each newly covered field uses the same rewrite rule the six existing fields use, selected by the alias group's `rewrite_rdata_labels` flag (in-bailiwick suffix rule when false, label-anywhere rule when true). DNSSEC record types (`RRSIG`, `NSEC`, `NSEC3`) and the rarely-used mailbox record types (`MB`, `MG`, `MR`, `MD`, `MF`, `MINFO`) are deliberately excluded from rewriting.

2. **Fail-closed safety net (secondary defense).** When a backup-zone answer contains a record whose type carries an in-bailiwick RDATA name but is NOT covered by the rewrite (for example a DNSSEC or excluded type appearing under an alias), the resolver SHALL withhold that record from the response rather than emit it with an unrewritten RDATA name, and SHALL log a warning identifying the withheld record type. The withheld record produces an empty (NODATA) answer for that name and type; the rest of the zone is served normally. This guarantees the server never emits a half-rewritten backup record even for record types the curated list does not cover.

## Non-Goals

- Not adopting a reflection / struct-tag-driven walk over all `domain-name` fields. That approach would also rewrite DNSSEC and mailbox fields (which must not be rewritten), requires a maintained exclusion list and slice-field handling, and adds reflection cost on the query hot path. The curated switch plus the fail-closed net achieves the same safety with lower risk.
- Not rewriting DNSSEC record RDATA names (`RRSIG` SignerName, `NSEC` NextDomain, `NSEC3`). Under an alias these records are already invalidated by the owner-name rewrite; the fail-closed net withholds them instead.
- Not changing owner-name rewriting, query-name rewriting, CNAME collapse, view selection, or any non-alias code path.
- Not addressing the separate escaped-dot zone-matching correctness issue (tracked as a distinct change).

## Success Criteria

- A backup-zone query for `HTTPS`, `SVCB`, `DNAME`, `NAPTR`, `RP`, `KX`, `AFSDB`, `PX`, or `RT` returns a record whose RDATA domain name(s) are rewritten into the backup namespace (in-bailiwick names) or preserved byte-for-byte (out-of-bailiwick names), with no backend origin remaining in the RDATA.
- The existing six record types (`CNAME`, `NS`, `MX`, `PTR`, `SRV`, `SOA`) behave exactly as before, under both `rewrite_rdata_labels: false` and `true`.
- `A`, `AAAA`, and `TXT` record RDATA remain unmodified.
- A backup-zone answer containing a record of an uncovered name-bearing type (e.g. `RRSIG`, `NSEC`) is withheld (NODATA for that name/type), a warning is logged, and other records in the zone are unaffected.
- The synthesized alias `AXFR` path produces the same fully-rewritten records for the newly covered types.
- Unit tests cover the rewrite of each newly added type and the fail-closed withholding path; existing alias tests continue to pass.

## Impact

- Affected specs: `alias-resolver` (modified — the RDATA rewrite requirement expands its covered record types and gains a fail-closed withholding requirement).
- Affected code:
  - Modified:
    - internal/alias/rewrite.go
    - internal/alias/override.go
    - internal/server/handler.go
    - internal/transfer/axfr.go
    - internal/alias/rewrite_test.go
    - internal/alias/override_test.go
  - New:
    - (none)
  - Removed:
    - (none)
- Operational: hot-path change in `internal/alias`; per CLAUDE.local.md Perf-Guard, an ns1→ns2 benchmark is required after implementation (the curated switch expansion is expected to be performance-neutral).
- Documentation: the alias/backup feature guide under the manual site should note which record types are rewritten and that uncovered name-bearing types are withheld from backup answers.
