## 1. Label-aware zone attribution

- [x] 1.1 In `IsInZone` (internal/dnsutil/dnsutil.go), add a backslash-gated fast path: when the name contains no backslash (`strings.IndexByte(name, '\\') < 0`), keep the current allocation-free byte-suffix comparison unchanged; only when a backslash is present, decide subdomain membership with an RFC 1035-escaping-aware label walk (miekg `dns.IsSubDomain` / `dns.CompareDomainName`, backed by `NextLabel`, or an equivalent local walk that counts preceding backslashes so an escaped `.` is not a boundary). This implements the requirement "Zone attribution honors RFC 1035 label escaping" and design Decision 1 (backslash-gated fast path) + Decision 2 (label-aware path via miekg primitives). Do not change the function signature; all callers inherit the corrected behavior.

## 2. Label-aware wildcard stepping

- [x] 2.1 In `LookupWildcard` (internal/zone/zone.go), replace the first-label strip so it splits at a true label boundary: decide once (per call) whether the query name contains a backslash; when it does not, keep the current `strings.Index(name, ".")` byte behavior; when it does, obtain the parent by offset using `dns.NextLabel(name, 0)` and slicing `parent := name[off:]` (NOT `SplitDomainName` + rejoin), so an escaped dot is not treated as a boundary AND `parent` stays byte-identical to the zone's stored presentation form. This byte-identity matters because `parent` is used verbatim as a map key in the wildcard probe `z.Records["*."+parent]` and the ENT-blocker check `z.Records[parent]`; a re-joined form could miss the entry. This implements the requirement "Wildcard label stepping honors RFC 1035 label escaping" and design Decision 1 + Decision 2. Preserve the existing ENT-blocking and origin-stop logic.

## 3. Unit tests (own code only)

- [x] 3.1 In internal/dnsutil/dnsutil_test.go, add cases for `IsInZone` with escaped-dot names: assert `IsInZone("x\\.a.example.com.", "a.example.com.")` is false and `IsInZone("x\\.a.example.com.", "example.com.")` is true, plus a differential/regression check that for a representative set of backslash-free names the label-aware result equals the current byte-path result (fast path and label-aware path agree on non-escaped names).
- [x] 3.2 In internal/zone/zone_test.go, add cases for `LookupWildcard` with an escaped-dot name: with `*.example.com.` and `*.a.example.com.` present, a query for the name whose single leftmost label is `x.a` (presented `x\.a.example.com.`) matches `*.example.com.` and not `*.a.example.com.`; and assert backslash-free names step and match exactly as before (regression guard).

## 4. Verification

- [x] 4.1 Run `make test` (race detector) and `make lint`; all pass.
- [x] 4.2 (User) After the post-implementation review chain, run the Perf-Guard ns1→ns2 benchmark per CLAUDE.local.md (this change touches the `internal/dnsutil` and `internal/zone` hot path; the backslash-free fast path is expected to be performance-neutral) and confirm no regression before commit.
