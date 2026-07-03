## 1. Remove deferred-by-one-generation machinery in cmd/shadowdns

- [x] 1.1 In cmd/shadowdns/main.go, reduce the GeoIP-runtime holder (`geoipRuntime`) to the current generation only: delete the `prevCountry`/`prevASN` deferred-close fields and the `closePrev` method.
- [x] 1.2 In `reload()`, delete the step-0 `closePrev` call and change the post-`SwapState` handoff so it overwrites `geo.country`/`geo.asn` with the new generation and closes nothing — the superseded generation reference is simply dropped, never parked and never closed.
- [x] 1.3 Update the GeoIP-runtime shutdown path (`closeAll`) to close only the current generation via the mmdb reader's idempotent `Close`; keep it nil-safe and a no-op on a second call, and confirm the enable→disable reload path drops the outgoing generation rather than parking it.
- [x] 1.4 Rewrite the in-code comments that assert the "deferred-by-one-generation" / "a full reload interval has passed" rationale to describe the reachability-driven lifecycle: the reload path closes nothing, and a superseded generation's mmdb is reclaimed by the mmdb reader's own runtime cleanup once it is unreachable (or by the OS at process exit).

## 2. Harden the reachability invariant at the point of use

- [x] 2.1 In internal/view/geoip_country.go, end `CountryDB.Lookup` with `runtime.KeepAlive` of the underlying reader after the `DecodePath` call, so the reader (hence its mmap) cannot be reclaimed mid-decode regardless of how a caller uses the state snapshot afterward; signature and return values unchanged.
- [x] 2.2 [P] Apply the identical trailing `runtime.KeepAlive` of the underlying reader to `ASNDB.Lookup` in internal/view/geoip_asn.go.

## 3. Reconcile existing tests and add the regression test

- [x] 3.1 Rewrite the GeoIP-handle-rotation assertions in cmd/shadowdns/main_reload_test.go that reference the deleted `geo.prevCountry` field and that assert a superseded generation becomes closed after a later reload (the "generation 1 still answers lookups after the second reload; it must be closed" checks, and the equivalent in the disable-then-re-enable test): change them to hold a reference to the startup generation and assert that after one reload and after two successive reloads its lookup STILL succeeds — i.e. the reload path closed nothing (reachability-driven). Update the file's header comment that describes the "deferred-by-one-generation close" lifecycle.
- [x] 3.2 Keep and adjust the existing GeoIP-runtime `closeAll`-idempotency test for the removed parked-generation slots: two successive `closeAll` calls must not panic and the second must be a no-op.
- [x] 3.3 Add cmd/shadowdns/main_geoip_reload_test.go with a rapid-double-SIGHUP race regression test: spawn concurrent goroutines that continuously perform country/ASN lookups against a generation held via a retained reference while two reloads (to N+1 then N+2) fire in immediate succession; the test MUST pass under `go test -race` and MUST NOT crash. Confirm it faults/races on the pre-fix deferred-close code before the fix lands. Do NOT add tests that assert the maxminddb reader's own close-idempotency or its cleanup firing on GC — those verify the dependency, not our code.

## 4. Verification

- [x] 4.1 Run `make test` (race detector) and confirm the full suite passes, including the rewritten reload tests, the new regression test, and all existing sighup-reload GeoIP scenarios.
- [x] 4.2 Run `make lint` and confirm zero issues.

## 5. Manual site review

- [x] 5.1 Review the Operations reload page and the GeoIP configuration pages (both language files) against this change; since it is an internal reliability fix with no new config field, CLI flag, or operator-visible knob, either add a one-line robustness note about SIGHUP GeoIP-handle safety or explicitly record the conclusion that no manual update is required, then run `make docs-build` if any file changed.

## 6. Review follow-up: no explicit close at shutdown either


- [x] 6.1 Remove the `closeAll` shutdown defer from `run()`; no production path closes an installed generation any more — the current generation is reclaimed by the OS at process exit. Rewrite the geoipRuntime and run() comments accordingly.
- [x] 6.2 Move `closeAll` to cmd/shadowdns/main_test.go as the test-only deterministic fixture-cleanup helper (unchanged semantics: nil-safe, no-op on second call).
- [x] 6.3 Reconcile proposal.md / design.md / specs/sighup-reload/spec.md with the shutdown-closes-nothing lifecycle (requirement text, lifecycle-example table, acceptance criteria) and add the shutdown scenario.

## 7. Perf-Guard (hot-path-adjacent)

- [x] 7.1 請使用者確認：本變更觸及 reload 路徑與 query hot path 讀取的 GeoIP generation 生命週期（`cmd/shadowdns` + `internal/view`）；實作與 review chain 完成後需依 Perf-Guard 在 ns2 跑 baseline → 部署 → 重測，確認 QPS 未下降 > 5% 且 p99 未上升 > 15%（query path 未改，預期無位移）。
