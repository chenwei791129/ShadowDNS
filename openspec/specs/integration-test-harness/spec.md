# integration-test-harness Specification

## Purpose

TBD - created by archiving change 'fix-integration-test-port-race'. Update Purpose after archive.

## Requirements

### Requirement: Integration test harness SHALL launch shadowdns binary without port-allocation races

Any test helper under `test/integration/` that starts the shadowdns binary as a child process and passes it a loopback port MUST tolerate transient `address already in use` failures caused by the inherent TOCTOU window between probe-bind and child-bind, and MUST NOT surface such transient failures as test failures when the application itself is correct.

#### Scenario: First-attempt bind succeeds

- **WHEN** the harness allocates a fresh loopback port and launches shadowdns with that port, and no competing process binds the port during the handoff window
- **THEN** the child process starts, logs its successful startup within the detection window, and the harness returns the running process to the test without retrying

#### Scenario: First-attempt bind loses the race

- **WHEN** the harness allocates a loopback port, launches shadowdns, and the child fails to bind because another process took the port between allocation and child bind
- **THEN** the harness detects the bind failure from the child's output within the detection window, terminates the failed child, allocates a different fresh port, and restarts shadowdns

#### Scenario: Repeated retries exhaust the budget

- **WHEN** the harness's retry budget is exhausted without ever observing a successful child startup
- **THEN** the harness fails the test with a diagnostic message that identifies port contention as the cause, and includes the captured output of every attempt for operator debugging


<!-- @trace
source: fix-integration-test-port-race
updated: 2026-04-18
tests:
  - test/integration/notify_test.go
-->

---
### Requirement: Integration test harness SHALL provide a single entry point for launching shadowdns

All integration tests that need to start the shadowdns binary MUST use one shared harness function; ad-hoc `freeLoopbackPort` + `exec.Command` call sites inside individual test files are disallowed.

#### Scenario: New integration test launches shadowdns

- **WHEN** a new integration test file needs a running shadowdns instance
- **THEN** the test calls the shared harness entry point and does not independently call `net.ListenPacket("udp", "127.0.0.1:0")` nor construct its own `exec.Command` for shadowdns

#### Scenario: Existing test launchers are migrated

- **WHEN** the change lands
- **THEN** every call site across `test/integration/` that previously launched shadowdns goes through the unified harness, verified by a grep audit in the tasks


<!-- @trace
source: fix-integration-test-port-race
updated: 2026-04-18
tests:
  - test/integration/notify_test.go
-->

---
### Requirement: Integration test harness SHALL clean up child processes on retry and on test completion

The harness MUST ensure that no orphaned shadowdns child processes remain after a test finishes, regardless of whether the test succeeded, failed, was retried, or was cancelled.

#### Scenario: Retry path kills the losing child

- **WHEN** the harness decides to retry after observing a bind failure
- **THEN** the harness sends SIGKILL to the failed child and waits for the process to be reaped before launching the next attempt

#### Scenario: Test completion reaps the child

- **WHEN** a test that launched shadowdns via the harness reaches its cleanup callback
- **THEN** the cleanup sends SIGTERM to the child process and waits for termination, preventing the child from outliving the test

<!-- @trace
source: fix-integration-test-port-race
updated: 2026-04-18
tests:
  - test/integration/notify_test.go
-->

---
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


<!-- @trace
source: fix-test-race-conditions
updated: 2026-04-22
code:
  - Makefile
tests:
  - internal/server/server_test.go
  - cmd/shadowdns/listenon_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/axfr_test.go
  - test/integration/helpers_test.go
  - test/integration/listenon_test.go
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

<!-- @trace
source: fix-test-race-conditions
updated: 2026-04-22
code:
  - Makefile
tests:
  - internal/server/server_test.go
  - cmd/shadowdns/listenon_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/axfr_test.go
  - test/integration/helpers_test.go
  - test/integration/listenon_test.go
-->