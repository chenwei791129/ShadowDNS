## Context

ShadowDNS builds each zone's in-memory index in the record-insertion primitive `AddRR` (`internal/zone/zone.go`), driven by the `ParseFile` loop (`internal/zone/parser.go`) over `miekg/dns`'s `ZoneParser` with `$INCLUDE` expansion enabled. `AddRR` currently appends every record at a `(owner, qtype)` key with no equality check, so a record declared both inline and inside a `$INCLUDE`d fragment is stored twice and served twice.

BIND collapses byte-identical RRs within an RRset at load (RFC 2181 §5.2 — an RRset is a set). Production master data routinely declares the same CNAME both inline in a per-view file and inside a shared CNAME fragment that the per-view file `$INCLUDE`s, so the same record is presented to the parser twice. Measured scale: a single backup root zone-view holds ~2900 such duplicates; this repeats across 7 views and dozens of backup root zones. The external DNS consistency checker compares ShadowDNS against BIND and flagged the divergence.

Each zone is loaded by exactly one `ParseFile` call (`internal/config/zones.go` gives each zone a single `File`; `internal/server/build.go` calls `ParseFile(z.File, origin, logger)` once per zone), and `ParseFile` already receives a `*zap.Logger`. So "one `ParseFile` = one zone" holds and the per-zone summary has a natural home without any signature change to `ParseFile`.

## Goals / Non-Goals

### Goals

- Guarantee the storage invariant: an in-memory RRset never contains byte-identical duplicate RRs (TTL excluded from identity).
- Match BIND's served output for affected names so the consistency checker passes.
- Provide operability (which zones carry duplicate data, how much) without flooding logs at production scale.
- Keep the query hot path untouched — deduplication is load-time only.

### Non-Goals

- Reconciling duplicates that differ only in TTL beyond keeping the first occurrence's TTL. TTL is not part of RR identity; no special TTL-mismatch warning is produced.
- Deduplicating across distinct RDATA (legitimate multi-record RRsets such as round-robin A sets are preserved in full).
- Changing the upstream master-data generation pipeline that emits the inline + `$INCLUDE` duplication. ShadowDNS faithfully mirrors operator data; the server-side dedup is the durable fix.
- Any change to wildcard, CNAME-following, alias-rewrite, or AXFR logic. Those paths consume the already-deduplicated store unchanged.

## Decisions

### Decision 1: Deduplicate inside `AddRR` (insertion-time), not in a post-parse pass

Make deduplication intrinsic to the insertion primitive so the invariant "an RRset has no duplicate RRs" is a property of the store itself and holds for every caller and every load path (inline and `$INCLUDE`-expanded records both flow through `AddRR`). `AddRR` compares the incoming RR against the records already stored at that `(owner, qtype)` and skips the insert when a duplicate is found.

`AddRR` gains a boolean return value reporting whether the record was actually stored (`true`) or skipped as a duplicate (`false`). Adding a return value is backward compatible: existing Go callers that invoke `AddRR` as a statement continue to compile and behave identically.

**Alternative — post-parse dedup pass over `Zone.Records`:** rejected. It leaves `AddRR` able to introduce duplicates (the invariant would depend on a caller remembering to run the pass), and it re-scans the whole zone after the fact instead of catching the duplicate at the single insertion choke point.

### Decision 2: Use `miekg/dns.IsDuplicate` for RR identity

Identity is owner + type + RDATA, excluding TTL — exactly what `dns.IsDuplicate(r1, r2)` (miekg/dns v1.1.72, already vendored) computes. Delegating avoids hand-rolling per-type RDATA comparison and matches BIND semantics (TTL is not part of RR identity).

The comparison runs only against records already stored at the same `(owner, qtype)` key (the existing single/sub storage shape), so the scan is bounded by the size of that one RRset. Real RRsets are tiny (the production duplication is one inline + one included copy, i.e. comparing against a 1-element set), so the cost is negligible. This is load-time work and never touches the query hot path.

**Alternative — `RR.String()` equality:** rejected. `String()` includes the TTL and owner and allocates; it would need post-processing to exclude TTL and is slower and more error-prone than the purpose-built `IsDuplicate`.

### Decision 3: Log shape mirrors the existing backup-override drop summary

The established precedent in `internal/zone/classify.go` (`filterBackupRecords` / `dropHistogram`) exists precisely to avoid a per-record log flood at the same scale (its comment cites a "7-view × 2854-CNAME blow-up"). Deduplication reuses that shape:

- **Per duplicate → DEBUG**, guarded by `logger.Core().Enabled(zapcore.DebugLevel)` so a disabled DEBUG level pays no per-record formatting cost. Fields: zone origin, owner, RR type.
- **Per zone → one WARN** when any duplicate was discarded, carrying the zone origin, the total discarded count, and a by-RR-type histogram. The histogram reuses the `dropHistogram` type's deterministic `MarshalLogObject` (alphabetic key order) for grep-stable output. A zero-duplicate zone emits nothing.

WARN (not INFO) is chosen for the summary because duplicate source data is an operator-actionable data-quality signal; the existing backup-override summary stays INFO and is unaffected.

**Alternative — one WARN per duplicate:** rejected. At ~2900 duplicates/zone-view × 7 views × dozens of zones, startup would emit hundreds of thousands of WARN lines — the exact failure the histogram precedent was built to prevent.

### Decision 4: Aggregate the summary in `ParseFile`

`ParseFile` owns the per-record loop and already holds the `*zap.Logger`, and one `ParseFile` call equals one zone. The loop tallies a by-type histogram from `AddRR`'s boolean return (`if !z.AddRR(rr) { dup[label]++ }`) and emits the WARN summary after the loop. The per-duplicate DEBUG is emitted at the point of detection. Deduplication runs during parse, therefore before `Classify`/`filterBackupRecords`, so root and backup-override zones are both covered.

## Implementation Contract

### Observable behavior

- After load, for any `(owner, qtype)`, `Zone.Records[owner][qtype]` contains no two RRs for which `dns.IsDuplicate` returns true. The first-stored occurrence (with its TTL) is retained; later byte-identical copies are dropped.
- Distinct records at the same `(owner, qtype)` (differing RDATA) are all retained, in insertion order.
- A DNS response that previously carried N records including duplicates now carries only the distinct records, matching BIND's output for the same name. Concretely: a CNAME chain whose first link was declared inline and via `$INCLUDE` is served with that CNAME exactly once.

### Interface / data shape

- `AddRR` returns `bool`: `true` when the record was stored, `false` when it was skipped as a duplicate. The signature change is additive and existing call sites compile unchanged.
- No change to `Zone.Records` shape, to `Lookup`/`LookupWildcard` return contracts, or to any resolution, alias-rewrite, or AXFR code.

### Logging

- DEBUG entry per discarded duplicate (zone, owner, type), emitted only when DEBUG is enabled.
- One WARN summary per zone with at least one duplicate: fields zone origin, total count, by-type histogram. No entry when a zone has zero duplicates.

### Acceptance criteria

- Unit tests in `internal/zone/zone_test.go` assert: identical RR inserted twice yields a length-1 RRset; distinct RRs are all retained in order; TTL-only-differing records collapse to one with the first TTL; `AddRR` returns false on the duplicate insert and true otherwise.
- Unit tests in `internal/zone/parser_test.go` assert: a zone file declaring a record inline and again via `$INCLUDE` yields a length-1 RRset; the WARN summary fires with the correct count and histogram when duplicates exist; no summary fires for a duplicate-free zone; per-record DEBUG is gated by level.
- `make test` (race detector) and `make lint` pass.
- Manual cross-check against the reproducing name (a backup-alias name whose CNAME is declared both inline and via `$INCLUDE`, e.g. `host.example.com`; the concrete production name is recorded under `.local/` only) on the test nameserver returns the same record set as BIND (no duplicated CNAME).

### In scope

- `internal/zone/zone.go` (`AddRR` dedup + boolean return), `internal/zone/parser.go` (tally + WARN summary, per-record DEBUG).
- Reuse of the `dropHistogram` marshaler pattern from `internal/zone/classify.go` for the summary histogram (shared helper or equivalent).
- Tests in `internal/zone`.

### Out of scope

- Resolution, wildcard, CNAME-following, alias-rewrite, AXFR, and ratelimit code.
- Upstream master-data generation.
- Any query-hot-path change.
