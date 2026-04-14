## ADDED Requirements

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

### Requirement: Count DNS requests by protocol, family, type, and view

The system SHALL expose a counter metric `shadowdns_dns_requests_total` with labels `proto` (udp/tcp), `family` (ipv4/ipv6), `type` (A/AAAA/MX/etc.), and `view` (the matched view name, or `refused` when no view matches). The counter SHALL increment by 1 for every DNS query received by `ServeDNS`.

#### Scenario: UDP A query increments counter

- **WHEN** a client sends an A query over UDP and the client IP matches view `view-th`
- **THEN** `shadowdns_dns_requests_total{proto="udp",family="ipv4",type="A",view="view-th"}` increments by 1

#### Scenario: Query with no matching view uses refused label

- **WHEN** a client sends a query but no view matches the client IP
- **THEN** `shadowdns_dns_requests_total{...,view="refused"}` increments by 1

### Requirement: Count DNS responses by rcode and view

The system SHALL expose a counter metric `shadowdns_dns_responses_total` with labels `rcode` (NOERROR/NXDOMAIN/SERVFAIL/REFUSED/FORMERR/NOTIMP) and `view`. The counter SHALL increment by 1 for every DNS response sent via `WriteMsg`.

#### Scenario: Successful response increments NOERROR counter

- **WHEN** a query is answered with rcode NOERROR in view `view-th`
- **THEN** `shadowdns_dns_responses_total{rcode="NOERROR",view="view-th"}` increments by 1

#### Scenario: NXDOMAIN response

- **WHEN** a query name does not exist in the zone
- **THEN** `shadowdns_dns_responses_total{rcode="NXDOMAIN",...}` increments by 1

### Requirement: Measure DNS request processing duration

The system SHALL expose a histogram metric `shadowdns_dns_request_duration_seconds` with label `view`. The histogram SHALL record the elapsed time from the entry of `ServeDNS` to the completion of `WriteMsg` for each query. The histogram SHALL use default Prometheus buckets.

#### Scenario: Query duration is recorded

- **WHEN** a DNS query is processed and a response is sent
- **THEN** the elapsed time in seconds is observed in `shadowdns_dns_request_duration_seconds{view="<matched-view>"}`

### Requirement: Expose build information as a gauge

The system SHALL expose a gauge metric `shadowdns_build_info` with labels `version` and `goversion`, set to the constant value 1. The `version` label SHALL contain the value of the `main.version` variable (set via ldflags at build time, defaulting to `dev`). The `goversion` label SHALL contain the Go runtime version.

#### Scenario: Build info gauge is present

- **WHEN** the metrics endpoint is scraped
- **THEN** `shadowdns_build_info{version="<version>",goversion="<goversion>"}` has value 1

### Requirement: Report loaded zone counts per view

The system SHALL expose two gauge metrics: `shadowdns_zones_loaded` and `shadowdns_zones_backup`, both with label `view`. `shadowdns_zones_loaded` SHALL report the number of root zones loaded for each view. `shadowdns_zones_backup` SHALL report the number of backup (alias) zones loaded for each view. Both gauges SHALL be updated when the server state is created or swapped (including after SIGHUP reload).

#### Scenario: Initial zone counts after startup

- **WHEN** ShadowDNS starts with 2 root zones and 3 backup zones in view `view-th`
- **THEN** `shadowdns_zones_loaded{view="view-th"}` equals 2 AND `shadowdns_zones_backup{view="view-th"}` equals 3

#### Scenario: Zone counts update after reload

- **WHEN** SIGHUP triggers a reload and the new configuration has 4 root zones in view `view-th`
- **THEN** `shadowdns_zones_loaded{view="view-th"}` equals 4

### Requirement: Expose GeoIP database metadata

The system SHALL expose a gauge metric `shadowdns_geoip_db_info` with labels `database` and `build_time`, set to the constant value 1. The `database` label SHALL be `country` or `asn`. The `build_time` label SHALL contain the database build timestamp formatted as ISO 8601 (UTC). The metadata SHALL be read from `maxminddb.Reader.Metadata.BuildEpoch`.

#### Scenario: GeoIP country database info

- **WHEN** the metrics endpoint is scraped and the loaded GeoLite2-Country database was built at Unix epoch 1700000000
- **THEN** `shadowdns_geoip_db_info{database="country",build_time="2023-11-14T22:13:20Z"}` has value 1

#### Scenario: GeoIP ASN database info

- **WHEN** the metrics endpoint is scraped
- **THEN** `shadowdns_geoip_db_info{database="asn",build_time="<ISO8601>"}` has value 1

### Requirement: Count panics recovered in DNS handler

The system SHALL expose a counter metric `shadowdns_panics_total` with no labels. The counter SHALL increment by 1 each time the panic recovery in `ServeDNS` catches a panic.

#### Scenario: Panic increments counter

- **WHEN** a panic occurs during DNS query processing and is recovered
- **THEN** `shadowdns_panics_total` increments by 1

### Requirement: Graceful shutdown of metrics HTTP server

The system SHALL gracefully shut down the metrics HTTP server when the application context is cancelled (SIGINT/SIGTERM). In-flight scrape requests SHALL be allowed to complete before the server exits.

#### Scenario: Metrics server shuts down on SIGTERM

- **WHEN** ShadowDNS receives SIGTERM while the metrics HTTP server is running
- **THEN** the metrics HTTP server completes any in-flight requests and stops accepting new connections
