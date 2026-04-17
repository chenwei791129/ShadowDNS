## 1. Setup and dependencies

- [x] 1.1 Add `github.com/cespare/xxhash/v2` to go.mod and run `go mod tidy` (decision: xxhash library: github.com/cespare/xxhash/v2)

## 2. Zone file fingerprint (TDD)

- [x] 2.1 [P] Write failing table-driven test in `internal/server/fingerprint_test.go` for Requirement: Zone file fingerprint strategy — covering unchanged file (size + hash match → reuse), rsync `-avc --inplace` scenario (same size, different content → hash detects change), and different size (early exit without computing hash)
- [x] 2.2 [P] Implement `zoneFingerprint` struct and `computeFingerprint(path string, mode VerifyMode)` helper in new file `internal/server/fingerprint.go` (decision: Zone file fingerprint: size + xxhash64)

## 3. Reload verify mode CLI flag (TDD)

- [x] 3.1 [P] Write failing test in `cmd/shadowdns/main_test.go` for Requirement: Reload verify mode configuration — default value `hash`, accept `hash|size|none`, reject invalid value with non-zero exit code and printed error
- [x] 3.2 [P] Implement `-reload-verify` flag registration, parsing, and validation in `cmd/shadowdns/main.go`; store parsed value on `runOptions` and make sticky across SIGHUP (decision: Reload verify mode: CLI flag `-reload-verify=hash|size|none`)

## 4. BuildState diff logic (TDD)

- [x] 4.1 Write failing test in `internal/server/build_test.go` for Requirement: Diff-based zone pointer reuse preserves immutability — when `prev` state's fingerprint matches, the new state's `*zone.Zone` is pointer-equal to the old one and its `Records` map is not mutated (decision: Diff-based state rebuild with pointer reuse)
- [x] 4.2 Write failing test for decision: First-reload / startup fallback — when `prev == nil`, BuildState parses every zone and records a fingerprint for each in the returned state
- [x] 4.3 Write failing test for decision: Rollback semantics on partial failure — when one zone fails to parse mid-build, BuildState returns an error, no state is swapped, and the caller's previous state remains intact with pointers unchanged
- [x] 4.4 Extend `BuildState(cfg, aliases, prev *ServerState, mode VerifyMode, ...)` signature in `internal/server/build.go` and implement the diff loop: compute fingerprint per zone, reuse pointer on match, re-parse on miss, store new fingerprints on the returned `ServerState`
- [x] 4.5 Wire `prev` state and `VerifyMode` through `reload()` in `cmd/shadowdns/main.go` so Requirement: SIGHUP triggers configuration reload (MODIFIED) is honored end-to-end (named.conf + aliases always re-read, zones conditionally re-parsed)

## 5. Post-swap garbage collection (TDD)

- [x] 5.1 [P] Write failing test in `internal/server/server_test.go` for Requirement: Post-swap garbage collection — `SwapState` triggers `runtime.GC()` and `debug.FreeOSMemory()` exactly once on success, and NOT at all on failed reload paths (use an injected hook or counter for observability) (decision: Post-swap GC trigger)
- [x] 5.2 [P] Implement `runtime.GC()` + `debug.FreeOSMemory()` invocation at the tail of `Server.SwapState` in `internal/server/server.go`

## 6. Reload diff logging

- [x] 6.1 Write failing test that a successful reload emits an INFO log entry containing verify mode, reused zone count, and re-parsed zone count, per Requirement: Reload diff logging
- [x] 6.2 Track reused vs re-parsed counts inside `BuildState`, return them in a summary struct, and emit the diff INFO log in `reload()` after `SwapState` returns in `cmd/shadowdns/main.go`

## 7. End-to-end integration

- [x] 7.1 Integration test `test/integration/reload_diff_test.go`: start server with two zones, trigger SIGHUP with no zone file changes, assert every `*zone.Zone` pointer is identical before/after (expose via a test-only state accessor)
- [x] 7.2 Integration test: simulate rsync `-avc --inplace` scenario (zone file rewritten with identical size and preserved mtime but different content); assert that `-reload-verify=hash` detects and re-parses, while `-reload-verify=size` misses the change (negative-control assertion proving the default is the safe mode)
- [x] 7.3 Integration test: `-reload-verify=none` forces full rebuild — every zone re-parsed, no pointer reuse, covering the escape-hatch path of Requirement: SIGHUP triggers configuration reload
