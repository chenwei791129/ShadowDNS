## ADDED Requirements

### Requirement: In-process server harness SHALL synchronize teardown with the server goroutine lifecycle

Any in-process integration test helper under `test/integration/` that constructs a `*server.Server` directly (rather than launching the shadowdns binary as a child process) MUST coordinate startup and shutdown with the server's serve goroutine through deterministic synchronization primitives. The helper MUST NOT use `time.Sleep` as a substitute for waiting on bind completion, listener readiness, serve-goroutine exit, or in-flight request drain. Cleanup callbacks that close shared resources (e.g., GeoIP readers, file handles, temporary directories) MUST execute strictly after the serve goroutine has returned.

#### Scenario: Helper waits for bind before reading the listener address

- **WHEN** the helper starts an in-process `*server.Server` and the test reads the kernel-assigned ephemeral address (e.g., via `srv.UDPAddr()`)
- **THEN** the helper has already returned from a synchronous `Bind` (or equivalent) call before the test goroutine observes the address, so no goroutine race exists between writing and reading `s.listeners`

#### Scenario: Teardown waits for the serve goroutine to exit before closing dependencies

- **WHEN** the test's cleanup callback runs and the helper holds shared resources used by request handlers (e.g., `*view.CountryDB`, `*view.ASNDB`)
- **THEN** the helper cancels the server context, waits for the serve goroutine to return (signalling that all in-flight handlers have drained), and only then closes those shared resources

#### Scenario: Race detector reports zero races for the harness

- **WHEN** the test suite is executed with `go test -race -count=1 ./test/integration/...`
- **THEN** no `WARNING: DATA RACE` originates from the in-process harness or its teardown sequence

<!-- @trace
source: fix-test-race-conditions
updated: 2026-04-22
tests:
  - test/integration/helpers_test.go
-->

---

### Requirement: Test helpers SHALL capture server log output through thread-safe sinks

Any test helper that captures log output from a running shadowdns server (whether in-process or out-of-process) for later assertion MUST use a sink that is safe for concurrent writes from multiple goroutines. A naked `bytes.Buffer` shared between the logger and the asserting goroutine is forbidden. Acceptable sinks include `go.uber.org/zap/zaptest/observer` or any equivalent mechanism that internally synchronizes writes and reads.

#### Scenario: Reload-path log assertion uses a synchronized sink

- **WHEN** a test exercises a code path that emits log entries from a goroutine other than the test goroutine (e.g., the SIGHUP reload handler) and later asserts on the captured log content
- **THEN** the captured log entries are read through a thread-safe sink, and `go test -race` reports no data race on the sink

<!-- @trace
source: fix-test-race-conditions
updated: 2026-04-22
tests:
  - cmd/shadowdns/listenon_test.go
-->
