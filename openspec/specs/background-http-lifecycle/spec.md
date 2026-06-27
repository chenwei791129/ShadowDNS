# background-http-lifecycle Specification

## Purpose

TBD - created by archiving change 'extract-httpserver-lifecycle'. Update Purpose after archive.

## Requirements

### Requirement: Single source of truth for graceful-shutdown deadline

All background HTTP servers (the Prometheus metrics server, the ephemeral TXT API server, and the DoH HTTPS/ACME servers) SHALL bound their graceful-shutdown drain with one shared deadline constant defined in a single location. The codebase SHALL NOT define the graceful-shutdown deadline separately per server.

#### Scenario: Shutdown deadline is defined once

- **WHEN** the source tree is searched for the graceful-shutdown drain deadline used by any background HTTP server
- **THEN** the deadline is defined in exactly one shared location
- **AND** the metrics, ephemeral API, and DoH servers all reference that shared definition rather than their own literal or constant


<!-- @trace
source: extract-httpserver-lifecycle
updated: 2026-06-27
code:
  - packaging/shadowdns.yaml.example
  - internal/httpserver/server.go
  - internal/shadowdnscfg/config.go
  - internal/doh/acme_key.go
  - internal/doh/acme.go
  - docs/guides/doh.md
  - internal/doh/server.go
  - internal/api/server.go
  - docs/guides/doh.zh.md
  - docs/configuration/shadowdns-yaml.md
  - docs/configuration/shadowdns-yaml.zh.md
  - packaging/shadowdns.service
  - cmd/shadowdns/main.go
tests:
  - internal/doh/acme_key_test.go
  - internal/doh/helpers_test.go
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/server_test.go
  - internal/shadowdnscfg/doh_test.go
  - internal/doh/acme_integration_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/httpserver/server_test.go
-->

---
### Requirement: Hardened connection timeouts on all background HTTP servers

Every background HTTP server SHALL be constructed with bounded read, idle, and header-read timeouts so that no background HTTP server is left with net/http's unbounded defaults. Each server SHALL also have a bounded write timeout EXCEPT where the server hosts long-running streaming response handlers, in which case the write timeout SHALL be left unbounded so streaming responses are not truncated.

#### Scenario: Metrics server has bounded read/idle/header timeouts

- **WHEN** the Prometheus metrics HTTP server is constructed
- **THEN** its `http.Server` has non-zero read, idle, and read-header timeouts

#### Scenario: Metrics server leaves write timeout unbounded for pprof streaming

- **WHEN** the Prometheus metrics HTTP server is constructed (which hosts the optional `/debug/pprof/profile` and `/debug/pprof/trace` streaming endpoints when pprof is enabled)
- **THEN** its `http.Server` write timeout is left unbounded (zero)
- **AND** a pprof CPU profile request whose duration exceeds the other servers' write timeout returns a complete, untruncated profile

#### Scenario: Ephemeral API server has bounded connection timeouts

- **WHEN** the ephemeral TXT API HTTP server is constructed
- **THEN** its `http.Server` has non-zero read, write, idle, and read-header timeouts

#### Scenario: DoH servers retain bounded connection timeouts

- **WHEN** the DoH HTTPS server and the ACME HTTP-01 listener are constructed
- **THEN** each `http.Server` has non-zero read, write, idle, and read-header timeouts


<!-- @trace
source: extract-httpserver-lifecycle
updated: 2026-06-27
code:
  - packaging/shadowdns.yaml.example
  - internal/httpserver/server.go
  - internal/shadowdnscfg/config.go
  - internal/doh/acme_key.go
  - internal/doh/acme.go
  - docs/guides/doh.md
  - internal/doh/server.go
  - internal/api/server.go
  - docs/guides/doh.zh.md
  - docs/configuration/shadowdns-yaml.md
  - docs/configuration/shadowdns-yaml.zh.md
  - packaging/shadowdns.service
  - cmd/shadowdns/main.go
tests:
  - internal/doh/acme_key_test.go
  - internal/doh/helpers_test.go
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/server_test.go
  - internal/shadowdnscfg/doh_test.go
  - internal/doh/acme_integration_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/httpserver/server_test.go
-->

---
### Requirement: Graceful drain on signal-driven shutdown

When the process receives a shutdown signal that cancels the root context, every running background HTTP server SHALL stop accepting new connections and drain in-flight requests within the shared graceful-shutdown deadline before the process exits.

#### Scenario: In-flight request drains on signal shutdown

- **WHEN** the root context is cancelled by a shutdown signal while a background HTTP server has an in-flight request
- **THEN** the server calls graceful shutdown bounded by the shared deadline
- **AND** the in-flight request is allowed to complete or is bounded by that deadline rather than being cut at the moment of cancellation


<!-- @trace
source: extract-httpserver-lifecycle
updated: 2026-06-27
code:
  - packaging/shadowdns.yaml.example
  - internal/httpserver/server.go
  - internal/shadowdnscfg/config.go
  - internal/doh/acme_key.go
  - internal/doh/acme.go
  - docs/guides/doh.md
  - internal/doh/server.go
  - internal/api/server.go
  - docs/guides/doh.zh.md
  - docs/configuration/shadowdns-yaml.md
  - docs/configuration/shadowdns-yaml.zh.md
  - packaging/shadowdns.service
  - cmd/shadowdns/main.go
tests:
  - internal/doh/acme_key_test.go
  - internal/doh/helpers_test.go
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/server_test.go
  - internal/shadowdnscfg/doh_test.go
  - internal/doh/acme_integration_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/httpserver/server_test.go
-->

---
### Requirement: Graceful drain on listener-death exit path

When a DNS listener dies at runtime and causes the process to exit while the root context is still alive, every running background HTTP server SHALL still be driven through graceful shutdown rather than being terminated abruptly with the process.

#### Scenario: Background servers drain when a DNS listener death triggers exit

- **WHEN** a DNS listener dies at runtime, causing the DNS serve loop to return with the root context still alive
- **THEN** the metrics, ephemeral API, and DoH background HTTP servers are each driven through graceful shutdown bounded by the shared deadline before the process exits


<!-- @trace
source: extract-httpserver-lifecycle
updated: 2026-06-27
code:
  - packaging/shadowdns.yaml.example
  - internal/httpserver/server.go
  - internal/shadowdnscfg/config.go
  - internal/doh/acme_key.go
  - internal/doh/acme.go
  - docs/guides/doh.md
  - internal/doh/server.go
  - internal/api/server.go
  - docs/guides/doh.zh.md
  - docs/configuration/shadowdns-yaml.md
  - docs/configuration/shadowdns-yaml.zh.md
  - packaging/shadowdns.service
  - cmd/shadowdns/main.go
tests:
  - internal/doh/acme_key_test.go
  - internal/doh/helpers_test.go
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/server_test.go
  - internal/shadowdnscfg/doh_test.go
  - internal/doh/acme_integration_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/httpserver/server_test.go
-->

---
### Requirement: Serve helper reports the first real serve error

The shared serve helper SHALL block until the supplied context is cancelled or the underlying server stops serving, and SHALL return the first error that is not `http.ErrServerClosed`. A normal shutdown that yields only `http.ErrServerClosed` SHALL be reported as success (nil error).

#### Scenario: Normal shutdown reports success

- **WHEN** the serve helper's server is shut down normally and `Serve` returns `http.ErrServerClosed`
- **THEN** the helper returns a nil error

#### Scenario: Bind or serve failure is surfaced

- **WHEN** the serve helper's underlying server fails to bind or serve with an error other than `http.ErrServerClosed`
- **THEN** the helper returns that error


<!-- @trace
source: extract-httpserver-lifecycle
updated: 2026-06-27
code:
  - packaging/shadowdns.yaml.example
  - internal/httpserver/server.go
  - internal/shadowdnscfg/config.go
  - internal/doh/acme_key.go
  - internal/doh/acme.go
  - docs/guides/doh.md
  - internal/doh/server.go
  - internal/api/server.go
  - docs/guides/doh.zh.md
  - docs/configuration/shadowdns-yaml.md
  - docs/configuration/shadowdns-yaml.zh.md
  - packaging/shadowdns.service
  - cmd/shadowdns/main.go
tests:
  - internal/doh/acme_key_test.go
  - internal/doh/helpers_test.go
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/server_test.go
  - internal/shadowdnscfg/doh_test.go
  - internal/doh/acme_integration_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/httpserver/server_test.go
-->

---
### Requirement: DoH preserves cross-listener failure propagation

The DoH subsystem coordinates more than one listener (the HTTPS server and the ACME HTTP-01 challenge listener) plus a certificate-renewal loop. When any one of its listeners fails to bind or serve, the DoH subsystem SHALL tear down its sibling listener and stop the certificate-renewal loop, so it never deadlocks waiting on the renewal loop and never keeps contacting the ACME directory for an endpoint that is not serving. Routing DoH's per-listener serve and drain through the shared helper SHALL NOT remove this cross-listener failure-propagation behavior.

#### Scenario: One DoH listener failure tears down the others

- **WHEN** one of the DoH listeners fails to bind or serve while the root context is still alive
- **THEN** the sibling DoH listener is shut down
- **AND** the certificate-renewal loop is stopped rather than left running against the ACME directory

<!-- @trace
source: extract-httpserver-lifecycle
updated: 2026-06-27
code:
  - packaging/shadowdns.yaml.example
  - internal/httpserver/server.go
  - internal/shadowdnscfg/config.go
  - internal/doh/acme_key.go
  - internal/doh/acme.go
  - docs/guides/doh.md
  - internal/doh/server.go
  - internal/api/server.go
  - docs/guides/doh.zh.md
  - docs/configuration/shadowdns-yaml.md
  - docs/configuration/shadowdns-yaml.zh.md
  - packaging/shadowdns.service
  - cmd/shadowdns/main.go
tests:
  - internal/doh/acme_key_test.go
  - internal/doh/helpers_test.go
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/server_test.go
  - internal/shadowdnscfg/doh_test.go
  - internal/doh/acme_integration_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/httpserver/server_test.go
-->