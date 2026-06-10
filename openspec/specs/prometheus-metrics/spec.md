# prometheus-metrics Specification

## Purpose

Expose operational metrics from ShadowDNS in Prometheus exposition format. An HTTP server serves the `/metrics` endpoint so that Prometheus can scrape request counts, response codes, query latency, zone counts, GeoIP database metadata, build information, and panic occurrences. The metrics endpoint is optional and can be disabled by setting `-metrics-addr` to an empty string.

## Requirements

### Requirement: Expose Prometheus metrics via HTTP endpoint

The system SHALL start an HTTP server on the address specified by the `-metrics-addr` flag (default `:9153`) and serve Prometheus-format metrics at the `/metrics` path. When `-metrics-addr` is set to an empty string, the system SHALL NOT start the metrics HTTP server and SHALL NOT register any Prometheus collectors.

#### Scenario: Default metrics endpoint is reachable

- **WHEN** ShadowDNS starts without specifying `-metrics-addr`
- **THEN** an HTTP GET to `http://localhost:9153/metrics` returns HTTP 200 with `text/plain` content in Prometheus exposition format

#### Scenario: Custom metrics address

- **WHEN** ShadowDNS starts with `-metrics-addr :9200`
- **THEN** the metrics endpoint is available at port 9200 instead of 9153

#### Scenario: Metrics disabled

- **WHEN** ShadowDNS starts with `-metrics-addr ""`
- **THEN** no HTTP server is started for metrics and no Prometheus collectors are registered

<!-- @trace
source: prometheus-metrics
updated: 2026-04-14
code: []
tests: []
-->


<!-- @trace
source: prometheus-metrics
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - internal/server/server.go
  - internal/view/geoip_country.go
  - internal/view/geoip_asn.go
  - go.mod
  - internal/metrics/writer.go
  - internal/server/handler.go
  - internal/metrics/metrics.go
tests:
  - internal/view/geoip_country_test.go
  - internal/metrics/writer_test.go
  - internal/metrics/metrics_test.go
  - internal/server/server_test.go
  - internal/view/geoip_asn_test.go
-->

---
### Requirement: Count DNS requests by protocol, family, type, and view

The system SHALL expose a counter metric `shadowdns_dns_requests_total` with labels `proto` (udp/tcp), `family` (ipv4/ipv6), `type` (A/AAAA/MX/etc.), and `view` (the matched view name, or `refused` when no view matches). The counter SHALL increment by 1 for every DNS query received by `ServeDNS`.

#### Scenario: UDP A query increments counter

- **WHEN** a client sends an A query over UDP and the client IP matches view `view-th`
- **THEN** `shadowdns_dns_requests_total{proto="udp",family="ipv4",type="A",view="view-th"}` increments by 1

#### Scenario: Query with no matching view uses refused label

- **WHEN** a client sends a query but no view matches the client IP
- **THEN** `shadowdns_dns_requests_total{...,view="refused"}` increments by 1

<!-- @trace
source: prometheus-metrics
updated: 2026-04-14
code: []
tests: []
-->


<!-- @trace
source: prometheus-metrics
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - internal/server/server.go
  - internal/view/geoip_country.go
  - internal/view/geoip_asn.go
  - go.mod
  - internal/metrics/writer.go
  - internal/server/handler.go
  - internal/metrics/metrics.go
tests:
  - internal/view/geoip_country_test.go
  - internal/metrics/writer_test.go
  - internal/metrics/metrics_test.go
  - internal/server/server_test.go
  - internal/view/geoip_asn_test.go
-->

---
### Requirement: Count DNS responses by rcode and view

The system SHALL expose a counter metric `shadowdns_dns_responses_total` with labels `rcode` (NOERROR/NXDOMAIN/SERVFAIL/REFUSED/FORMERR/NOTIMP) and `view`. The counter SHALL increment by 1 for every DNS response sent via `WriteMsg`.

#### Scenario: Successful response increments NOERROR counter

- **WHEN** a query is answered with rcode NOERROR in view `view-th`
- **THEN** `shadowdns_dns_responses_total{rcode="NOERROR",view="view-th"}` increments by 1

#### Scenario: NXDOMAIN response

- **WHEN** a query name does not exist in the zone
- **THEN** `shadowdns_dns_responses_total{rcode="NXDOMAIN",...}` increments by 1

<!-- @trace
source: prometheus-metrics
updated: 2026-04-14
code: []
tests: []
-->


<!-- @trace
source: prometheus-metrics
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - internal/server/server.go
  - internal/view/geoip_country.go
  - internal/view/geoip_asn.go
  - go.mod
  - internal/metrics/writer.go
  - internal/server/handler.go
  - internal/metrics/metrics.go
tests:
  - internal/view/geoip_country_test.go
  - internal/metrics/writer_test.go
  - internal/metrics/metrics_test.go
  - internal/server/server_test.go
  - internal/view/geoip_asn_test.go
-->

---
### Requirement: Measure DNS request processing duration

The system SHALL expose a histogram metric `shadowdns_dns_request_duration_seconds` with label `view`. The histogram SHALL record the elapsed time from the entry of `ServeDNS` to the completion of `WriteMsg` for each query. The histogram SHALL use the following DNS-optimised buckets (in seconds): `0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1`, covering a range of approximately 100 µs to 100 ms. Queries completing in under 100 µs or over 100 ms SHALL still be counted in the implicit `+Inf` bucket.

#### Scenario: Query duration is recorded

- **WHEN** a DNS query is processed and a response is sent
- **THEN** the elapsed time in seconds is observed in `shadowdns_dns_request_duration_seconds{view="<matched-view>"}` using the DNS-optimised buckets

#### Scenario: Sub-millisecond query falls in 100µs–1ms buckets

- **WHEN** a DNS query completes in 300 µs (0.0003 seconds)
- **THEN** the observation is counted in the `le="0.0005"` bucket and all larger buckets, but NOT in the `le="0.00025"` bucket

##### Example: bucket assignment for representative durations

| Query duration | Lowest bucket that captures it (`le` value) |
| -------------- | ------------------------------------------ |
| 80 µs          | `0.0001`                                   |
| 150 µs         | `0.00025`                                  |
| 300 µs         | `0.0005`                                   |
| 800 µs         | `0.001`                                    |
| 3 ms           | `0.005`                                    |
| 20 ms          | `0.025`                                    |
| 75 ms          | `0.1`                                      |
| 200 ms         | `+Inf`                                     |

#### Scenario: Metrics endpoint exposes correct bucket boundaries

- **WHEN** the `/metrics` endpoint is scraped
- **THEN** the `shadowdns_dns_request_duration_seconds_bucket` series SHALL include exactly the `le` labels `0.0001`, `0.00025`, `0.0005`, `0.001`, `0.0025`, `0.005`, `0.01`, `0.025`, `0.05`, `0.1`, and `+Inf`
- **AND** the series SHALL NOT include `le` labels from the former default Prometheus buckets that are absent from the new set (e.g., `0.25`, `0.5`, `1`, `2.5`, `5`, `10`)


<!-- @trace
source: dns-latency-histogram-buckets
updated: 2026-06-10
code:
  - internal/metrics/metrics.go
tests:
  - internal/metrics/metrics_test.go
-->

---
### Requirement: Expose build information as a gauge

The system SHALL expose a gauge metric `shadowdns_build_info` with labels `version` and `goversion`, set to the constant value 1. The `version` label SHALL contain the value of the `main.version` variable (set via ldflags at build time, defaulting to `dev`). The `goversion` label SHALL contain the Go runtime version.

#### Scenario: Build info gauge is present

- **WHEN** the metrics endpoint is scraped
- **THEN** `shadowdns_build_info{version="<version>",goversion="<goversion>"}` has value 1

<!-- @trace
source: prometheus-metrics
updated: 2026-04-14
code: []
tests: []
-->


<!-- @trace
source: prometheus-metrics
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - internal/server/server.go
  - internal/view/geoip_country.go
  - internal/view/geoip_asn.go
  - go.mod
  - internal/metrics/writer.go
  - internal/server/handler.go
  - internal/metrics/metrics.go
tests:
  - internal/view/geoip_country_test.go
  - internal/metrics/writer_test.go
  - internal/metrics/metrics_test.go
  - internal/server/server_test.go
  - internal/view/geoip_asn_test.go
-->

---
### Requirement: Report loaded zone counts per view

The system SHALL expose two gauge metrics: `shadowdns_zones_loaded` and `shadowdns_zones_backup`, both with label `view`. `shadowdns_zones_loaded` SHALL report the number of root zones loaded for each view. `shadowdns_zones_backup` SHALL report the number of backup (alias) zones loaded for each view. Both gauges SHALL be updated when the server state is created or swapped (including after SIGHUP reload).

#### Scenario: Initial zone counts after startup

- **WHEN** ShadowDNS starts with 2 root zones and 3 backup zones in view `view-th`
- **THEN** `shadowdns_zones_loaded{view="view-th"}` equals 2 AND `shadowdns_zones_backup{view="view-th"}` equals 3

#### Scenario: Zone counts update after reload

- **WHEN** SIGHUP triggers a reload and the new configuration has 4 root zones in view `view-th`
- **THEN** `shadowdns_zones_loaded{view="view-th"}` equals 4

<!-- @trace
source: prometheus-metrics
updated: 2026-04-14
code: []
tests: []
-->


<!-- @trace
source: prometheus-metrics
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - internal/server/server.go
  - internal/view/geoip_country.go
  - internal/view/geoip_asn.go
  - go.mod
  - internal/metrics/writer.go
  - internal/server/handler.go
  - internal/metrics/metrics.go
tests:
  - internal/view/geoip_country_test.go
  - internal/metrics/writer_test.go
  - internal/metrics/metrics_test.go
  - internal/server/server_test.go
  - internal/view/geoip_asn_test.go
-->

---
### Requirement: Expose GeoIP database metadata

The system SHALL expose a gauge metric `shadowdns_geoip_db_info` with labels `database` and `build_time`, set to the constant value 1. The `database` label SHALL be `country` or `asn`. The `build_time` label SHALL contain the database build timestamp formatted as ISO 8601 (UTC). The metadata SHALL be read from `maxminddb.Reader.Metadata.BuildEpoch`.

#### Scenario: GeoIP country database info

- **WHEN** the metrics endpoint is scraped and the loaded GeoLite2-Country database was built at Unix epoch 1700000000
- **THEN** `shadowdns_geoip_db_info{database="country",build_time="2023-11-14T22:13:20Z"}` has value 1

#### Scenario: GeoIP ASN database info

- **WHEN** the metrics endpoint is scraped
- **THEN** `shadowdns_geoip_db_info{database="asn",build_time="<ISO8601>"}` has value 1

<!-- @trace
source: prometheus-metrics
updated: 2026-04-14
code: []
tests: []
-->


<!-- @trace
source: prometheus-metrics
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - internal/server/server.go
  - internal/view/geoip_country.go
  - internal/view/geoip_asn.go
  - go.mod
  - internal/metrics/writer.go
  - internal/server/handler.go
  - internal/metrics/metrics.go
tests:
  - internal/view/geoip_country_test.go
  - internal/metrics/writer_test.go
  - internal/metrics/metrics_test.go
  - internal/server/server_test.go
  - internal/view/geoip_asn_test.go
-->

---
### Requirement: Count panics recovered in DNS handler

The system SHALL expose a counter metric `shadowdns_panics_total` with no labels. The counter SHALL increment by 1 each time the panic recovery in `ServeDNS` catches a panic.

#### Scenario: Panic increments counter

- **WHEN** a panic occurs during DNS query processing and is recovered
- **THEN** `shadowdns_panics_total` increments by 1

<!-- @trace
source: prometheus-metrics
updated: 2026-04-14
code: []
tests: []
-->


<!-- @trace
source: prometheus-metrics
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - internal/server/server.go
  - internal/view/geoip_country.go
  - internal/view/geoip_asn.go
  - go.mod
  - internal/metrics/writer.go
  - internal/server/handler.go
  - internal/metrics/metrics.go
tests:
  - internal/view/geoip_country_test.go
  - internal/metrics/writer_test.go
  - internal/metrics/metrics_test.go
  - internal/server/server_test.go
  - internal/view/geoip_asn_test.go
-->

---
### Requirement: Graceful shutdown of metrics HTTP server

The system SHALL gracefully shut down the metrics HTTP server when the application context is cancelled (SIGINT/SIGTERM). In-flight scrape requests SHALL be allowed to complete before the server exits.

#### Scenario: Metrics server shuts down on SIGTERM

- **WHEN** ShadowDNS receives SIGTERM while the metrics HTTP server is running
- **THEN** the metrics HTTP server completes any in-flight requests and stops accepting new connections

<!-- @trace
source: prometheus-metrics
updated: 2026-04-14
code: []
tests: []
-->

<!-- @trace
source: prometheus-metrics
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - internal/server/server.go
  - internal/view/geoip_country.go
  - internal/view/geoip_asn.go
  - go.mod
  - internal/metrics/writer.go
  - internal/server/handler.go
  - internal/metrics/metrics.go
tests:
  - internal/view/geoip_country_test.go
  - internal/metrics/writer_test.go
  - internal/metrics/metrics_test.go
  - internal/server/server_test.go
  - internal/view/geoip_asn_test.go
-->

---
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

<!-- @trace
source: add-pprof-endpoint
updated: 2026-04-20
code:
  - CHANGELOG.md
  - internal/transfer/axfr.go
  - cmd/shadowdns/pprof.go
  - internal/zone/classify.go
  - internal/alias/override.go
  - internal/zone/zone.go
  - cmd/shadowdns/main.go
  - internal/transfer/notify.go
  - internal/server/handler.go
  - internal/zone/parser.go
  - README.md
tests:
  - internal/server/server_test.go
  - internal/alias/override_test.go
  - cmd/shadowdns/pprof_test.go
  - internal/zone/zone_test.go
  - internal/zone/parser_test.go
-->

---
### Requirement: Expose response rate limiting counters

When response rate limiting is configured, the prometheus-metrics endpoint SHALL expose a counter tracking rate-limit decisions, labeled by response category (`responses`, `nxdomains`, `nodata`, `errors`) and by action (`dropped`, `slipped`, `exempted`, `logonly_would_drop`). The counter SHALL increment once per UDP response for which the limiter took a rate-limit-relevant action. Responses that are allowed without being over-limit SHALL NOT increment this counter. When rate limiting is unconfigured, the counter MAY be absent or remain at zero.

#### Scenario: Dropped response increments the dropped counter

- **WHEN** the limiter drops an over-limit NXDOMAIN response over UDP
- **THEN** the rate-limit counter labeled category `nxdomains` and action `dropped` SHALL increment by one

#### Scenario: Slipped response increments the slipped counter

- **WHEN** the limiter truncates (slips) an over-limit positive response over UDP
- **THEN** the rate-limit counter labeled category `responses` and action `slipped` SHALL increment by one

#### Scenario: Log-only would-drop increments the logonly counter

- **WHEN** `log-only` is enabled and a response that would have been dropped is delivered unchanged
- **THEN** the rate-limit counter labeled action `logonly_would_drop` SHALL increment by one and no `dropped` increment SHALL occur

<!-- @trace
source: add-response-rate-limiting
updated: 2026-06-09
code:
  - internal/ratelimit/exempt.go
  - cmd/shadowdns/main.go
  - internal/config/options.go
  - internal/server/handler.go
  - internal/ratelimit/writer.go
  - testdata/integration/named.conf
  - internal/config/ratelimit.go
  - internal/config/zones.go
  - internal/ratelimit/slip.go
  - internal/ratelimit/table.go
  - internal/ratelimit/classify.go
  - internal/ratelimit/limiter.go
  - internal/metrics/metrics.go
  - internal/server/server.go
  - README.md
  - internal/ratelimit/key.go
tests:
  - internal/config/ratelimit_test.go
  - internal/ratelimit/classify_test.go
  - internal/ratelimit/table_test.go
  - internal/ratelimit/limiter_decide_test.go
  - internal/server/handler_ratelimit_test.go
  - internal/metrics/metrics_ratelimit_test.go
  - internal/ratelimit/writer_test.go
  - internal/ratelimit/slip_test.go
  - internal/ratelimit/limiter_credit_test.go
  - internal/ratelimit/key_test.go
  - internal/config/ratelimit_warn_test.go
-->