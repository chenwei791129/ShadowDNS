## Context

`IsInZone` (dnsutil package) and `LookupWildcard` (zone package) decide zone membership and wildcard label boundaries by scanning the presentation-form name for `.` bytes. miekg/dns escapes a literal `.` inside a wire label to `\.` in presentation form, so these byte scans mistake the escaped dot for a label boundary, mis-routing a query with a dotted-label name to the wrong zone or wildcard (security audit run-2 Finding #2, LOW; correctness/availability, no cross-trust leak).

Constraints:
- Hot path: `IsInZone` runs in `alias.Detect`'s per-query loop over every loaded zone, and in zone lookup per query; `LookupWildcard` runs per query on exact-miss. Both are currently allocation-free byte operations.
- `LookupKey` intentionally does not un-escape; the escape sequence must reach the matchers intact so they can decide boundaries correctly.
- Escaped-dot labels are rare in real traffic; the common case must not pay for the fix.

## Goals / Non-Goals

### Goals
- Zone attribution (`IsInZone`) and wildcard label stepping (`LookupWildcard`) treat `\.` as a within-label character, not a label boundary.
- Zero behavior change and zero measurable performance change for names that contain no backslash.

### Non-Goals
- No change to `LookupKey` folding; no un-escaping of names in the pipeline.
- No change to callers' signatures — the fix is confined to the two shared primitives, so all callers inherit the correct behavior.
- No new matching capabilities; only correcting existing matching to honor RFC 1035 escaping.

## Decisions

### Decision 1: Backslash-gated fast path
Both primitives keep their current allocation-free byte path for the common case and only take the label-aware path when the name contains a backslash. Rationale: an escaped dot can only exist in a name that contains a backslash, so `strings.IndexByte(name, '\\') < 0` is a cheap, sufficient guard that a name has no escape sequence and the existing byte logic is already correct for it. This preserves the hot-path performance (`alias.Detect` loops over all zones per query) while fixing the escaped-dot case. The guard is checked once per call.

### Decision 2: Label-aware path honors RFC 1035 escaping via miekg primitives
When a backslash is present, `IsInZone` decides membership with a label walk that counts preceding backslashes (an odd count means the `.` is escaped and not a boundary) — using miekg/dns `dns.IsSubDomain` / `dns.CompareDomainName` (backed by `NextLabel`, which already implements this rule) or an equivalent local label walk. `LookupWildcard`'s first-label strip obtains the parent by offset — `dns.NextLabel(name, 0)` returns the offset of the start of the second label, and `parent := name[off:]` — instead of `strings.Index(name, ".")`, so the split occurs at the first true label boundary. Offset-based slicing (rather than `SplitDomainName` + rejoin) is required because `parent` is used verbatim as a map key: the code probes `z.Records["*."+parent]` and checks the ENT blocker `z.Records[parent]`, both of which must match the zone's stored presentation form byte-for-byte; re-joining split labels risks altering the escaped form and missing the map entry. Rationale: miekg's label walk is the authoritative implementation of RFC 1035 escaping and is already a dependency; reusing its offset primitive avoids a bespoke escape parser and preserves the stored byte form.

## Implementation Contract

- **Observable behavior — attribution**: With loaded zones `example.com.` and `a.example.com.`, `IsInZone("x\\.a.example.com.", "a.example.com.")` returns false (the `\.` is within the first label, so the name is not a subdomain of `a.example.com.`), and `alias.Detect` attributes `x\.a.example.com.` to `example.com.`. `IsInZone("x\\.a.example.com.", "example.com.")` returns true.
- **Observable behavior — wildcard stepping**: `LookupWildcard` for `x\.a.example.com.` steps to parent `example.com.` (not `a.example.com.`) and probes `*.example.com.`, not `*.a.example.com.`.
- **Observable behavior — fast path unchanged**: For any name containing no backslash, `IsInZone` and `LookupWildcard` return byte-for-byte identical results to the current implementation.
- **Failure modes**: No panic on malformed escapes or trailing backslash; a name that is not a valid subdomain simply returns not-in-zone / no-wildcard. The label-aware path allocates only for names containing a backslash.
- **Acceptance criteria / verification**: Unit tests assert (a) the escaped-dot attribution and wildcard-stepping examples above; (b) a differential/regression check that for a representative set of backslash-free names the label-aware result equals the fast-path result (fast path and label-aware path agree on non-escaped names); (c) no behavior change for existing test cases. `make test` and `make lint` pass. Per CLAUDE.local.md Perf-Guard, an ns1→ns2 benchmark is run after implementation (expected performance-neutral because backslash-free queries never enter the label-aware path).
- **In scope**: `IsInZone` in the dnsutil package and `LookupWildcard`'s label stepping in the zone package.
- **Out of scope**: `LookupKey`, caller signatures, the alias RDATA rewrite change, and any change to how names are stored or unpacked.
