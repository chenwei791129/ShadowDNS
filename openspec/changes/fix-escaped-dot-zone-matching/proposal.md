## Problem

Zone attribution and wildcard label-stepping compare the presentation-form query name with pure byte matching that treats every `.` as a label separator — including a `.` that is actually an escaped dot (`\.`) inside a single DNS label. When miekg/dns unpacks a wire label that contains a literal `.` byte, it escapes that byte to `\.` in the presentation string that becomes the query name. `IsInZone` in the dnsutil package does a byte-suffix test (checking that the byte before the matched suffix is `.`), and `LookupWildcard` in the zone package strips the first label with a byte scan for `.`; neither un-escapes first, so both mistake the `.` of a `\.` sequence for a label boundary.

Concrete effect: with two loaded zones in the same view where one is a strict suffix of the other (`example.com.` and `a.example.com.`), a query whose first label literally contains a dot (wire label `x.a`, presented as `x\.a.example.com.`) is truly a three-label child of `example.com.`, but longest-suffix `Detect` attributes it to `a.example.com.` (because `IsInZone` returns true for both) and `LookupWildcard` splits at the escaped dot and probes `*.a.example.com.`. The result is a wrong-zone answer, a wrong wildcard match, or an NXDOMAIN for a name that should have resolved.

## Root Cause

`IsInZone` (dnsutil package) and `LookupWildcard`'s label stepping (zone package) perform byte-level label splitting that does not honor RFC 1035 escaping, so an escaped dot (`\.`) inside a label is treated as a label boundary. `LookupKey` (dnsutil package) only lowercases and trims the trailing dot; it correctly does not un-escape, so the escape sequence reaches the byte-level matchers intact.

## Proposed Solution

Make zone attribution and wildcard label stepping label-aware, honoring RFC 1035 escaping, while preserving the current allocation-free hot path for the common case:

- Names that contain no backslash (the overwhelming majority of real queries) continue to use the existing byte-level fast path — behavior and performance are unchanged.
- Only when a name contains a backslash (an escape sequence is present) does the comparison fall back to label-aware matching that counts preceding backslashes so an escaped `.` is not treated as a boundary. This uses miekg/dns primitives whose label walk already honors escaping (`dns.IsSubDomain` / `dns.SplitDomainName`, backed by miekg's `NextLabel`) or an equivalent label-aware routine.

`LookupKey` is deliberately left unchanged (not un-escaping is correct; the escape sequence must survive to the label-aware comparison). Fixing the two shared primitives (`IsInZone` and `LookupWildcard`'s label stepping) corrects every caller, including the ephemeral-api PUT bailiwick check for FQDNs that contain an escaped dot.

## Non-Goals

- Not changing `LookupKey`'s lowercase/trailing-dot folding, and not un-escaping names anywhere in the pipeline.
- Not replacing the byte-level fast path for backslash-free names (that would risk a hot-path performance regression for zero behavioral benefit).
- Not addressing the separate alias RDATA name-rewrite leak (tracked as its own change).
- Not adding new zone-matching capabilities; this only corrects existing matching to honor escaping.

## Success Criteria

- With loaded zones `example.com.` and `a.example.com.`, a query for the name whose single first label is `x.a` (presented `x\.a.example.com.`) is attributed to `example.com.` (its true enclosing zone), not `a.example.com.`, and wildcard stepping does not split at the escaped dot.
- `IsInZone` returns the label-correct result for names containing `\.` (an escaped dot is not a zone boundary), and returns identical results to today for all names without a backslash.
- `LookupWildcard` steps labels at true label boundaries for names containing `\.`, and is unchanged for backslash-free names.
- Unit tests cover both the fast path (backslash-free) and the label-aware path (names containing `\.`) for `IsInZone` and `LookupWildcard`, asserting they agree with label-correct expectations.
- No behavior change for any name that contains no backslash (regression guard).

## Impact

- Affected code:
  - Modified:
    - internal/dnsutil/dnsutil.go
    - internal/zone/zone.go
    - internal/dnsutil/dnsutil_test.go
    - internal/zone/zone_test.go
  - New:
    - (none)
  - Removed:
    - (none)
- Affected specs (modified capabilities): `alias-resolver` (the "Detect whether a query target is a backup zone" requirement gains escaped-dot label-boundary correctness) and `dns-server` (the "Match wildcard records per RFC 4592 when exact lookup fails" requirement gains escaped-dot label-boundary correctness).
- Shared-primitive beneficiaries (no signature change): all `IsInZone` callers (alias detect/rewrite, zone parser/collapse/lookup, ephemeral-api bailiwick check) become correct for escaped-dot names.
- Operational: hot-path change in `internal/dnsutil` and `internal/zone`; per CLAUDE.local.md Perf-Guard, an ns1→ns2 benchmark is required after implementation (the backslash-free fast path is designed to be performance-neutral).
