## ADDED Requirements

### Requirement: Expose pprof profiling endpoints (opt-in)

The system SHALL support an opt-in pprof profiling feature controlled by the boolean flag `-pprof-enable` (default `false`). When `-pprof-enable=true` AND the metrics HTTP server is enabled (i.e. `-metrics-addr` is not empty), the system SHALL register Go standard library `net/http/pprof` handlers on the metrics HTTP server under the path prefix `/debug/pprof/`. The registered handlers SHALL include `pprof.Index`, `pprof.Cmdline`, `pprof.Profile`, `pprof.Symbol`, `pprof.Trace`, and the named profile handlers for `heap`, `goroutine`, `allocs`, `threadcreate`, `block`, and `mutex` via `pprof.Handler(name)`. The system SHALL NOT use `_ "net/http/pprof"` blank import to avoid polluting `http.DefaultServeMux`.

When `-pprof-enable=false`, the system SHALL NOT register any pprof handlers and the `/debug/pprof/` path SHALL return HTTP 404 Not Found.

The system SHALL NOT enable block profile sampling (`runtime.SetBlockProfileRate`) or mutex profile sampling (`runtime.SetMutexProfileFraction`) automatically; operators requiring these profiles MUST enable them through separate means.

pprof endpoints SHALL share the same bind address and access control boundary as the metrics HTTP server. The system SHALL NOT provide authentication, rate limiting, or a separate bind port for pprof.

The `-pprof-enable` flag SHALL be read only at startup; SIGHUP reload SHALL NOT change its value.

#### Scenario: pprof disabled by default

- **WHEN** ShadowDNS starts without specifying `-pprof-enable` and the metrics server is enabled at `:9153`
- **THEN** an HTTP GET to `http://localhost:9153/debug/pprof/` returns HTTP 404 Not Found
- **AND** an HTTP GET to `http://localhost:9153/metrics` still returns HTTP 200

#### Scenario: pprof enabled via flag

- **WHEN** ShadowDNS starts with `-pprof-enable` and `-metrics-addr :9153`
- **THEN** an HTTP GET to `http://localhost:9153/debug/pprof/` returns HTTP 200 with the pprof index page
- **AND** an HTTP GET to `http://localhost:9153/debug/pprof/heap` returns a heap profile in pprof binary format
- **AND** an HTTP GET to `http://localhost:9153/debug/pprof/goroutine?debug=1` returns a goroutine dump in text format

#### Scenario: Conflicting flags produce fatal startup error

- **WHEN** ShadowDNS starts with `-pprof-enable` AND `-metrics-addr ""`
- **THEN** the process SHALL log a fatal error explaining the conflict
- **AND** SHALL exit with a non-zero status code before serving any DNS traffic

#### Scenario: DefaultServeMux is not polluted

- **WHEN** ShadowDNS starts with `-pprof-enable` and the metrics server is enabled
- **THEN** `http.DefaultServeMux` SHALL NOT have any `/debug/pprof/` handlers registered
- **AND** the pprof handlers SHALL only be reachable through the metrics HTTP server's mux

#### Scenario: Block and mutex profiles return empty by default

- **WHEN** ShadowDNS starts with `-pprof-enable` but without external code calling `runtime.SetBlockProfileRate` or `runtime.SetMutexProfileFraction`
- **THEN** an HTTP GET to `http://localhost:9153/debug/pprof/block` returns an empty profile
- **AND** an HTTP GET to `http://localhost:9153/debug/pprof/mutex` returns an empty profile

<!-- @trace
source: add-pprof-endpoint
updated: 2026-04-20
code:
  - cmd/shadowdns/main.go
  - cmd/shadowdns/pprof.go
tests:
  - cmd/shadowdns/pprof_test.go
-->

