## Context

DNS queries load the server state once via an atomic-pointer load at the start of processing and hold that snapshot — view matcher plus the country/ASN mmdb readers it references — for the entire query. On SIGHUP the reload path builds a new state, atomically swaps it, and manages the superseded GeoIP generation with a "deferred-by-one-generation" close: the superseded generation is parked and its mmdb unmapped (munmap) at the start of the *next* reload.

The mmdb dependency (maxminddb-golang/v2, pinned at v2.2.0) already reclaims an unreferenced reader's mapping safely on its own: each reader registers a runtime cleanup that munmaps once the reader becomes unreachable, and the reader's close is idempotent (guarded by an atomic compare-and-swap flag). So a superseded generation that ShadowDNS simply stops referencing would be unmapped automatically once the last in-flight query holding it finishes — reachability-driven, correct under any cadence. The reload path defeats this by closing the mapping *early* on a cadence-based timer.

The deferred-by-one-generation schedule is safe only if a full reload interval elapses before a generation is unmapped. A rapid double SIGHUP breaks that: the second reload unmaps a generation one reload old while a query that loaded its snapshot before the first reload is still reading it → fatal use-after-munmap SIGSEGV (GitHub issue #13). The defect is the early explicit close, not any absence of GC reclamation.

## Goals / Non-Goals

**Goals:**

- Eliminate the use-after-munmap under any reload cadence, including arbitrarily rapid successive SIGHUPs.
- Keep the per-query hot path byte-for-byte unchanged (no added synchronization, reference counting, or locking).
- Preserve all existing SIGHUP GeoIP reload behavior (re-open on new mmdb, keep-old on failure, geoip_enabled log field, db_info gauge update, enable/disable transitions).

**Non-Goals:**

- Rate-limiter, query-log, and zone-data reload paths are untouched.
- No new mmap/close/cleanup machinery inside ShadowDNS; the fix relies on the dependency's existing idempotent close and per-reader reclamation cleanup and does not reimplement or test them.
- Conditional GeoIP-requirement semantics and reload-failure keep-old-state behavior are unchanged.
- No change to GeoIP db_info metrics behavior.

## Decisions

**Decision 1 — Stop closing superseded generations from the reload path; let the dependency reclaim them by reachability.**
After the state swap the reload path drops its reference to the superseded generation and closes nothing. The superseded generation (and its mmdb mapping) stays reachable — and therefore mapped and usable — for exactly as long as any in-flight query holds the previous state snapshot (server state → view matcher → country/ASN reader). Once no query can reach it, the dependency's own per-reader runtime cleanup munmaps it. Reclamation is tied to reachability, not reload cadence, so no number of rapid reloads can unmap a generation a query is still reading. The state swap forces a garbage-collection cycle (`runtime.GC()` + `debug.FreeOSMemory()`), but that cycle cannot reclaim the generation the *same* reload just superseded: the holder still pins it through its current-generation reference at swap time (the reference is overwritten only after the swap). A fully-drained superseded generation is instead reclaimed by a later GC cycle — the next reload's forced GC once the generation has drained, a subsequent natural GC, or the OS at process exit — without any per-query work. ShadowDNS registers no cleanup of its own and does not rely on the *timing* of the dependency's cleanup for correctness — only on the guarantee that the mapping is never freed while still reachable.

**Decision 2 — Remove the deferred-by-one-generation machinery; no path in the process closes an installed generation, shutdown included.**
Delete the parked-generation slots and the "close the parked generation" step from the GeoIP-runtime holder and the reload path. The holder retains only the current generation. On reload, the outgoing generation reference is simply overwritten (dropped), never parked and never closed — this applies equally when the replacing generation is nil (GeoIP disabled by the reload). Process shutdown closes nothing either: the shutdown joins do not cover every query — the DoH listener drain is bounded by a timeout, so a slow handler can still be mid-lookup after the run loop's defers execute — and an explicit shutdown close would munmap under that straggler, the same crash class relocated to shutdown. The current generation is therefore left for the OS to reclaim at process exit (or the dependency's cleanup, if it becomes unreachable first). The only explicit closes remaining are for handles that were never installed into any state (new handles discarded by a failed reload) and the test-only deterministic cleanup helper, which stays nil-safe and a no-op on a second call.

**Decision 3 — Harden the reachability invariant at the point of use.**
Correctness depends on the mmdb reader staying reachable for the duration of a lookup's decode. End the country and ASN lookup wrappers with a keep-alive of the underlying reader (`runtime.KeepAlive`) after the decode call, so the reader cannot be considered dead — and its mapping reclaimed — mid-decode, independent of how a caller uses the state snapshot after the lookup returns. `runtime.KeepAlive` is a compile-time liveness barrier that emits no runtime instructions, so the hot path pays nothing.

## Implementation Contract

**Behavior:** After a successful reload the superseded GeoIP mmdb stays mapped and usable for every in-flight query that loaded the previous state snapshot, for that query's full duration, regardless of how many further reloads occur in the meantime. The mapping is reclaimed only after no query can still reach the generation. No reload cadence can cause a use-after-munmap. From an operator's perspective the crash under rapid double SIGHUP no longer occurs; all other reload-observable behavior is identical.

**Interface / data shape:**
- `cmd/shadowdns` GeoIP-runtime holder retains only the current country/ASN generation (the parked-generation fields and the park-and-close step are removed). Its reload entry point overwrites the current-generation reference after the swap and closes nothing; the run loop registers no shutdown close, so no production path ever closes an installed generation. The deterministic close-all helper moves to the test files (fixture cleanup only) and stays nil-safe and idempotent on repeated calls.
- `internal/view` country/ASN lookup wrappers are unchanged in signature and add a trailing `runtime.KeepAlive` of the underlying reader after the decode call. Their `Close` remains a thin, nil-safe pass-through to the dependency's idempotent reader close; no `sync.Once` or self-registered cleanup is added.

**Failure modes:** A failed reload (mmdb open error, parse error, state-build error) returns an error, keeps the previous state and its GeoIP handles in use, and increments the failure metric — unchanged. Superseded-generation mappings are now reclaimed by the dependency's cleanup, which ignores munmap errors (as it did before via the discarded return); ShadowDNS no longer emits a warn-on-close log for superseded generations. This is an accepted, deliberate trade-off: a munmap error during reclamation is not operator-actionable, and the previous warn only ever covered it incidentally. The same applies to the current generation at shutdown, which is no longer explicitly closed (the OS reclaims it at exit); the only warn-on-close logging left covers new handles discarded by a failed reload.

**Acceptance criteria:**
- Race-detector regression test: concurrent goroutines continuously perform country/ASN lookups against a generation while two reloads fire in immediate succession; the test runs under `-race` and MUST NOT crash or report a data race. This test faults on the pre-fix deferred-close code and passes after the fix.
- Deterministic superseded-generation-stays-usable test: hold a reference to the startup generation (so nothing can reclaim it), drive one reload and then two successive reloads, and assert a lookup against the retained reference still returns its data — proving the reload path closed nothing. Because the test pins the generation, the result does not depend on GC timing.
- No shutdown close: the run loop's exit paths register no close of the GeoIP holder, so a straggler query that outlives the bounded DoH drain can never hit a munmapped current generation. The test-only close-all helper stays nil-safe and a no-op on a second call (existing close-twice test continues to pass).
- The existing sighup-reload GeoIP scenarios pass, with assertions that required the removed synchronous close (a superseded generation's lookup returning "closed") rewritten to assert the generation stays usable (reachability-driven, not closed by reload).
- Perf-Guard on ns2: QPS not down > 5% and p99 not up > 15% on both CNAME and A lists.

**Scope boundaries:**
- In scope: the GeoIP-runtime holder + reload/shutdown wiring in `cmd/shadowdns/main.go`, the two existing lookup wrappers in `internal/view/geoip_country.go` and `internal/view/geoip_asn.go` (KeepAlive only), and the reconciliation of `cmd/shadowdns/main_reload_test.go` with the new behavior.
- Out of scope: rate-limiter, query-log, and zone reload paths; the per-query view-matching hot path structure; conditional-requirement and keep-old-state semantics; db_info metrics; the dependency's internal reclamation/idempotency (relied upon, never tested here).

## Risks / Trade-offs

- **Reclamation timing depends on GC.** A superseded generation's mapping is released only after it drains AND a GC cycle runs the dependency's cleanup. The reload that supersedes a generation still pins it through the holder's current-generation reference at swap time, so its own post-swap forced GC does not reclaim it; a fully-drained generation is reclaimed by a subsequent GC cycle (the next reload's forced GC, a later natural GC, or the OS at exit). Under a pathological never-returning query the mapping is held until that query ends — the correct, safe behavior (holding memory beats crashing). Worst case is bounded extra resident mmap for the generations still referenced by live queries (mmdb files are tens of MB). At process exit the OS reclaims any still-mapped region regardless of whether the cleanup ran.
- **Reachability is load-bearing.** The fix is correct only while the reader stays reachable through a lookup's decode. Decision 3's `runtime.KeepAlive` defends this at the point of use so a future change to how the handler holds the state snapshot cannot silently reintroduce the crash. The reload/handler hot path is otherwise unchanged.
- **Lost warn-on-close for superseded generations.** Documented under Failure modes: an accepted trade-off, since the munmap-error warn was incidental and non-actionable, and the dependency discards the same error itself.
- **We depend on the dependency's reclamation guarantee.** If a future maxminddb major version dropped its per-reader cleanup or made close non-idempotent, superseded mappings could leak (not crash). This is pinned by go.mod; a dependency bump would re-exercise Perf-Guard and the regression test. We deliberately do not add a test for the dependency's behavior (repo policy: do not test third-party packages).
- **Alternatives considered and rejected:**
  - *Self-registered per-generation GC cleanup + `sync.Once` close inside `internal/view`*: duplicates behavior the dependency already provides (per-reader cleanup + idempotent close), adding machinery and tests that would mostly assert the dependency's own guarantees. Rejected as redundant.
  - *Per-generation reference counting* acquired/released around each query's state use: deterministic and unit-testable, but adds an atomic increment/decrement to the per-query lifecycle — a hot-path cost this project's Perf-Guard discipline specifically guards against. Rejected to keep the hot path untouched.
  - *Fixed time-based grace period* before unmap (grace > max query duration): hot-path-free, but replaces the one-generation assumption with a bounded-query-duration assumption — still assumption-based, and picking the grace constant is fragile. Rejected in favor of a scheme with no timing assumption.
  - *Never unmap (keep every generation mapped until shutdown)*: trivially safe but leaks one mmdb mapping per reload for the process lifetime; unbounded for long-lived, frequently-reloaded processes. Rejected.
