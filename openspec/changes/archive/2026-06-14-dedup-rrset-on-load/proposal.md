## Why

ShadowDNS preserves byte-for-byte duplicate resource records within a single RRset, so a name whose identical record is declared both inline and via a `$INCLUDE`d fragment is served with the duplicate present. BIND collapses such duplicates at load time (an RRset is a set; RFC 2181 §5.2 forbids identical RRs in one RRset), so the two servers disagree and the external consistency checker flags ShadowDNS as inconsistent. The duplication is pervasive in production data (~2900 duplicated CNAMEs per zone-view), not a one-off.

## What Changes

- Deduplicate identical resource records within each `(owner, qtype)` RRset at zone load time, keeping the first occurrence and discarding later byte-identical copies. Record identity is owner + type + RDATA, ignoring TTL (delegated to `miekg/dns.IsDuplicate`).
- Make "an RRset never contains duplicate RRs" an invariant of the in-memory zone store: the deduplication happens in the record-insertion primitive so every load path (inline records and `$INCLUDE`-expanded fragments alike) benefits.
- Emit per-duplicate detail at DEBUG (guarded so disabled DEBUG costs nothing) and one aggregated WARN summary per zone (total count + by-type histogram) whenever any duplicate was discarded. This mirrors the existing backup-override drop-summary logging and avoids a per-record log flood at production scale.
- Deduplication applies to all zones (root and backup-override) and runs before backup-override classification.

## Non-Goals

(captured in design.md)

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `zone-parser`: add a requirement that identical RRs within an RRset are deduplicated at load with aggregated logging; clarify the existing same-qtype insertion-order requirement to apply to distinct records.

## Impact

- Affected specs: `zone-parser`
- Affected code:
  - Modified:
    - internal/zone/zone.go
    - internal/zone/parser.go
  - New: (none)
  - Removed: (none)
- Behavior: zone load output for affected names changes from N+duplicates to N distinct RRs (now matching BIND); query hot path is untouched (dedup is load-time only). `AddRR` gains a boolean return value (backward compatible — Go callers may ignore it).
- Tests: internal/zone/zone_test.go, internal/zone/parser_test.go
- Docs: review zone-loading / operations pages in the MkDocs manual for a note that duplicate RRs are collapsed at load.
