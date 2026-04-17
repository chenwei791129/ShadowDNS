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