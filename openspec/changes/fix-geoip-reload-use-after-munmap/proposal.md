## Problem

On a rapid double SIGHUP reload, ShadowDNS can crash with a fatal use-after-munmap SIGSEGV inside a GeoIP lookup. A DNS query captures the server state once at the start of its processing (an atomic-pointer load) and then holds that state snapshot — including the view matcher and the country/ASN mmdb readers it points at — for the whole query. Reloading the configuration swaps in a new state and, one reload later, unmaps (munmap) the mmdb memory of the superseded GeoIP generation. When two SIGHUPs arrive in quick succession, that unmap can happen while a slow query started before the first reload is still reading the old mmdb, dereferencing freed memory and crashing the process.

## Root Cause

The reload path closes (munmaps) a superseded GeoIP generation on a fixed "deferred-by-one-generation" schedule: it parks the superseded generation and calls its close at the start of the next reload. That schedule is safe only under the assumption — stated verbatim in the reload path's own comment — that a full reload interval has elapsed since the generation was swapped out, so no in-flight query still references it. A rapid double SIGHUP violates the assumption: the second reload closes a generation only one reload old, while an in-flight query that loaded its state snapshot before the first reload is still reading that generation's mmdb, so the munmap frees memory the query is actively dereferencing.

The reclamation clock is tied to reload cadence rather than to whether the generation is still reachable by any query. Crucially, this early explicit close is the *only* defect: the mmdb dependency (maxminddb-golang/v2) already reclaims an unreferenced reader's mapping safely on its own — each reader registers a runtime cleanup that munmaps once the reader becomes unreachable, and the reader's own close is idempotent (guarded by an atomic flag). ShadowDNS's reload path defeats that safe, reachability-driven reclamation by closing the mapping early on a cadence-based timer instead of letting it be reclaimed when it is genuinely unreachable.

## Proposed Solution

Stop closing superseded GeoIP generations from the reload path; let each generation's mapping be reclaimed by the dependency's existing reachability-driven cleanup once no in-flight query can reach it. This is correct under any reload cadence, including arbitrarily rapid successive SIGHUPs, and adds nothing to the per-query hot path.

- Remove the deferred-by-one-generation machinery from the GeoIP-runtime holder: drop the parked-generation slots and the close-the-parked-generation step. After the state swap the reload path SHALL simply drop its reference to the superseded generation and close nothing; the superseded generation stays reachable (and its mmdb mapped and usable) as long as any in-flight query holds the previous state snapshot, and its mapping is reclaimed by the dependency's per-reader cleanup once it becomes unreachable — no earlier.
- Do not close the *current* generation at process shutdown either: a query can outlive the shutdown joins (the DoH listener drain is bounded by a timeout, so a slow handler may still be mid-lookup when the run loop returns), and an explicit shutdown close would munmap under it — the same crash class, relocated to shutdown. No path in the process closes an installed generation; reclamation is always the dependency's reachability cleanup, or the OS at process exit. Tests keep a deterministic close helper for fixture cleanup only.
- Harden the reachability invariant at the point of use: end the country and ASN lookup wrappers with a keep-alive of the underlying reader so the mapping cannot be reclaimed mid-decode regardless of how a caller uses the state snapshot afterward. This is a compile-time liveness barrier with no runtime cost.

The per-query hot path keeps loading the state pointer and performing country/ASN lookups exactly as before, with no added synchronization, reference counting, or locking.

## Non-Goals

- No change to the rate-limiter reload path, the query-log sink reload/close path, or the zone-data reload path — only GeoIP generation lifetime is addressed.
- No change to the per-query hot path (state load, view matching, country/ASN lookup): the fix MUST NOT add reference counting, mutexes, or other per-query synchronization. Per-generation reference counting was considered and rejected for this reason (see design Alternatives Considered).
- No new mmap/close/cleanup machinery inside ShadowDNS: the fix relies on the dependency's existing idempotent close and per-reader reclamation cleanup rather than reimplementing them. No test in this change verifies the dependency's own reclamation or idempotency behavior.
- No change to the conditional GeoIP-requirement semantics (when geoip-directory is required, optional, or absent) or to the reload-failure keep-old-state behavior.
- No change to the GeoIP db_info metrics behavior on reload.

## Success Criteria

- Two SIGHUP reloads issued in immediate succession, while concurrent queries continuously perform GeoIP lookups against the generation being superseded, never cause a use-after-munmap crash or data race — verified by a race-detector regression test (run under `-race`) that faults on the current deferred-close code and passes after the fix.
- After one and after two successive reloads, the superseded generation remains usable: a lookup against a retained reference to it still returns its data, proving the reload path performed no close — verified by a deterministic test that holds a reference to the superseded generation (so nothing else can reclaim it) and asserts the lookup still succeeds.
- No path in the process closes an installed generation — the run loop registers no shutdown close, so a straggler query that outlives the bounded DoH drain can never hit a munmapped current generation. The test-only deterministic cleanup helper remains nil-safe and a no-op on a second call — verified by the existing close-twice test.
- The existing SIGHUP GeoIP reload behavior (re-open on new mmdb, keep-old on open failure, geoip_enabled log field, db_info gauge update, enable/disable transitions) continues to pass, with the assertions that required the removed synchronous close rewritten to assert the new reachability-driven behavior.
- Perf-Guard on ns2 shows no regression (QPS not down more than 5%, p99 not up more than 15%) on both the CNAME and A benchmark lists, since the query path is unchanged.

## Impact

- Affected code:
  - Modified: cmd/shadowdns/main.go
  - Modified: cmd/shadowdns/main_test.go
  - Modified: cmd/shadowdns/main_reload_test.go
  - Modified: internal/view/geoip_country.go
  - Modified: internal/view/geoip_asn.go
  - New: cmd/shadowdns/main_geoip_reload_test.go
- Affected specs:
  - Modified: sighup-reload (GeoIP databases are reloaded on SIGHUP; All fallible reload steps precede the state swap)
