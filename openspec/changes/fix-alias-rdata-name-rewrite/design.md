## Context

The alias resolver serves a backup/mirror domain from a single loaded root zone by rewriting names between the backup namespace and the root namespace. Every backup-zone answer record has its owner name rewritten unconditionally, but RDATA domain names are rewritten only for `CNAME`/`NS`/`MX`/`PTR`/`SRV`/`SOA`. Records of any other name-bearing type are emitted with their RDATA name still in the root namespace, disclosing the backend origin the alias is meant to hide (security audit run-2 Finding #1, MEDIUM). The rewrite primitive (`RewriteRR` → `rewriteRDATANames` in the alias package) is shared by the live query path (`finalizeBackupRRs` / `collapseBackupResult` in the alias package) and the synthesized alias AXFR path (`buildAliasRecords` in the transfer package).

Constraints:
- Hot path: alias resolution runs per query. Per-record, per-query reflection over all fields is undesirable.
- ShadowDNS does not sign zones; DNSSEC records, if present in a zone file, are served unsigned and are already invalidated by owner-name rewriting under an alias.
- Fix must not change behavior for the six currently handled types or for `A`/`AAAA`/`TXT`.

## Goals / Non-Goals

### Goals
- Rewrite RDATA domain names for all common name-bearing record types that are meaningful alias targets, so backup answers never leak the backend origin.
- Guarantee fail-closed behavior: even a name-bearing record type the rewrite does not explicitly cover must never be emitted with an unrewritten RDATA name from a backup zone.
- Keep the six existing types and `A`/`AAAA`/`TXT` behavior byte-for-byte identical.

### Non-Goals
- No reflection-driven rewriting of RDATA fields (only a cached reflection-based classification check; see Decision 2).
- No rewriting of DNSSEC RDATA names (`RRSIG`, `NSEC`, `NSEC3`); they are withheld instead.
- No change to owner-name rewrite, query-name rewrite, CNAME collapse, view/GeoIP selection, or any non-alias path.

## Decisions

### Decision 1: Curated switch expansion for rewriting (not reflection)
Extend the RDATA name rewrite switch to add: `HTTPS` (Target), `SVCB` (Target), `DNAME` (Target), `NAPTR` (Replacement), `RP` (Mbox, Txt), `KX` (Exchanger), `AFSDB` (Hostname), `PX` (Map822, Mapx400), `RT` (Host). Each new field uses the same `rewriteValue` function the existing six use (the in-bailiwick `RewriteName` rule, or the label-anywhere `RewriteNameAnywhere` rule when the alias group sets `rewrite_rdata_labels: true`).

Rationale: a hand-written switch applies the exact rule per field, adds no hot-path reflection for the rewrite itself, and is trivially reviewable/testable. A full reflection walk would also touch DNSSEC and mailbox fields (which must not be rewritten) and require slice-field handling and a maintained exclusion list — more risk for no benefit.

### Decision 2: Fail-closed classification via cached reflection boolean (not a hand-maintained denylist)
Introduce a per-record classification with three outcomes: `covered` (a type in the switch above), `noName` (no name-bearing RDATA field — e.g. `A`/`AAAA`/`TXT`), and `uncoveredName` (has a name-bearing RDATA field but is not in the switch — e.g. `RRSIG`, `NSEC`, `MINFO`).

The "has a name-bearing RDATA field" test SHALL be derived authoritatively from the record type's name-carrying struct tags via reflection — `dns:"domain-name"` / `dns:"cdomain-name"`, plus the dedicated gateway-host tags `dns:"ipsechost"` / `dns:"amtrelayhost"` (IPSECKEY/AMTRELAY carry a domain-name gateway under those wire-format tags) — computed once per concrete Go type and cached (keyed by the record's rrtype or `reflect.Type`), so the hot path pays only a cached map lookup. A hand-maintained denylist is rejected because it reproduces the original bug's failure mode: a name-bearing type absent from both the covered set and the denylist would leak. Deriving "name-bearing" from struct tags makes the net authoritative and future-proof — any name-bearing type not explicitly covered is classified `uncoveredName` and withheld.

The reflection walk MUST recurse into anonymous embedded structs. Some record types carry their name-bearing field by composition rather than as a top-level field — notably `dns.HTTPS` is declared `type HTTPS struct { SVCB }` and its `Target` (`dns:"domain-name"`) field lives on the embedded `SVCB`. A one-level `NumField` scan over `reflect.TypeOf(dns.HTTPS{})` finds no tagged field and would misclassify `HTTPS` as `noName`, violating the covered ⊆ name-bearing invariant and silently under-protecting any embedded-struct type. The walk descends into every anonymous embedded struct field.

Invariant (two directions, both tested): (a) covered ⊆ name-bearing — a test asserts every `covered` rrtype is classified name-bearing (guards against adding a switch case for a no-name type, and against the embedded-struct miss above); (b) covered == switch — because the `rewriteRDATANames` type switch cannot be introspected, the covered rrtype set is maintained separately, so a test SHALL feed one record of each covered rrtype with an in-bailiwick RDATA name through `rewriteRDATANames` and assert the RDATA was actually rewritten. This round-trip catches drift in both directions: a type added to the switch but not the covered set (which would wrongly withhold a rewritable record — data loss) and a type in the covered set the switch does not actually handle.

### Decision 3: Withhold-and-warn happens at the resolution boundary
`covered` records are rewritten and emitted; `noName` records are emitted unchanged; `uncoveredName` records are dropped from the result slice. The classification-and-filter step lives at the points that assemble backup answers — `finalizeBackupRRs` and `collapseBackupResult` in the alias package, and `buildAliasRecords` in the transfer package — so both the live query path and the synthesized alias AXFR path are covered by one rule. Other records in the same response and zone are unaffected. No `SERVFAIL`, no `NXDOMAIN`.

**Existing-name-NODATA vs stage-miss (critical).** Dropping records must NOT be indistinguishable from "the name/type was not found", or the handler's staged fallback (`handleBackupQuery` tries exact → CNAME fallback → wildcard) will fall through to wildcard synthesis and answer a name that actually exists — violating RFC 4592 (an existing owner name blocks wildcard matching) and returning a wildcard-synthesized answer instead of the correct NODATA. Therefore the assembly functions distinguish "found records but all were withheld" from "found nothing":

- `finalizeBackupRRs` returns `(kept []dns.RR, withheld []WithheldRecord)`. A non-empty `withheld` with empty `kept` means the name/type existed in root but every record was withheld — an existing-name NODATA.
- `collapseBackupResult` already returns `(records []dns.RR, nodata bool)`; when the filter empties `records` for a name that existed, it SHALL set `nodata = true` and also return the `withheld` list.
- `handleBackupQuery` treats "kept empty AND withheld non-empty" (and the collapse `nodata=true`) as a terminal NODATA: it replies with `negativeReply` and does NOT proceed to the CNAME-fallback or wildcard stages.

**Withheld-list plumbing (enumerated, logger-free alias package).** The filter returns a `withheld` list rather than logging directly, so the alias package keeps no logging dependency and withholding stays unit-testable. `WithheldRecord` carries the rrtype and owner name. The list is propagated out of every alias entry point the handler calls, and the handler logs it via `s.Logger`; the AXFR path logs via the logger already in `HandleAliasAXFR`. See the tasks for the exact list of signatures and call sites that change (all of `override.go`'s exported `Resolve*` functions, `synthesizeWildcardRRs`, `collapseChainAt`, the `answeredCollapsed` closure and its call sites in `handler.go`, and `buildAliasRecords` in `axfr.go`).

Rationale: DNSSEC records under an alias are already invalidated by owner rewriting; withholding is cleaner than emitting an invalid or origin-leaking record. Per-record (not per-zone, not per-response) withholding keeps the blast radius to the single offending record. Surfacing a withheld list (instead of logging in-place) keeps the classification pure and testable and avoids coupling the alias package to `zap`.

## Implementation Contract

- **Observable behavior — rewrite**: A backup-zone query (over UDP/TCP/53 or DoH) for `HTTPS`, `SVCB`, `DNAME`, `NAPTR`, `RP`, `KX`, `AFSDB`, `PX`, or `RT` returns a record whose RDATA domain-name field(s) are rewritten from the root origin to the backup origin when in-bailiwick, and preserved byte-for-byte when out-of-bailiwick. Example: root holds `www.example.net. HTTPS 1 svc.example.net.`, alias backup origin `example.com.` → query `www.example.com. HTTPS` returns `www.example.com. HTTPS 1 svc.example.com.` (no `example.net.` in the RDATA). With `rewrite_rdata_labels: true`, a root origin appearing as a mid-label sequence in the RDATA name is also rewritten, matching the existing behavior for the six types.
- **Observable behavior — unchanged types**: `CNAME`/`NS`/`MX`/`PTR`/`SRV`/`SOA` produce identical output to today under both flag values; `A`/`AAAA`/`TXT` RDATA is unmodified.
- **Observable behavior — fail-closed**: A backup-zone answer that would contain a record of an uncovered name-bearing type (e.g. `RRSIG`, `NSEC`) omits that record (NODATA for that name/type) and emits one warning log line naming the withheld record type and owner. Records of covered or no-name types in the same answer are still returned.
- **AXFR parity**: The synthesized alias AXFR stream contains fully-rewritten records for the newly covered types and omits uncovered name-bearing records, consistent with the query path.
- **Failure modes**: No panic on any record type (including unknown/private types — `dns.RFC3597`/unknown types have no typed name-bearing field and classify as `noName`, emitted as-is). Classification cache is concurrency-safe for the shared read-mostly access pattern.
- **Acceptance criteria / verification**: Unit tests assert (a) correct rewrite output for each newly covered type under both flag values, in-bailiwick and out-of-bailiwick; (b) the six existing types and `A`/`AAAA`/`TXT` are unchanged; (c) an uncovered name-bearing type is withheld on the query path (absent from the result, surfaced in the withheld list) and the existing-name-NODATA path does not fall through to wildcard synthesis; (d) both invariants of Decision 2 — covered ⊆ name-bearing (including the `HTTPS`/embedded-`SVCB` case) and the covered == switch round-trip; (e) the AXFR path (`internal/transfer/axfr_test.go`) withholds an uncovered name-bearing type and still emits rewritten covered types. `make test` and `make lint` pass. Per CLAUDE.local.md Perf-Guard, an ns1→ns2 benchmark is run after implementation (expected performance-neutral).
- **In scope**: RDATA name rewriting and fail-closed classification/withholding for backup-zone answers on both the query and synthesized-AXFR paths (alias and transfer packages).
- **Out of scope**: owner/query-name rewrite, CNAME collapse, view/GeoIP/ECS, the escaped-dot zone-matching issue (separate change), and any DNSSEC signing/validation.
