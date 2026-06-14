## ADDED Requirements

### Requirement: Expose Go runtime and process metrics

The system SHALL register the Go runtime collector and the process collector on the same custom Prometheus registry that holds the ShadowDNS collectors, so that the `/metrics` endpoint exposes the standard `go_*` and `process_*` metric families in addition to the `shadowdns_*` metrics. The `process_*` family is provided by the platform process collector and SHALL be present on Linux; on platforms where the process collector reports no data (e.g. non-Linux), the absence of `process_*` series is expected and SHALL NOT be treated as an error.

#### Scenario: Go runtime metrics are exposed

- **WHEN** the `/metrics` endpoint is scraped
- **THEN** the response SHALL include at least the `go_goroutines` gauge and the `go_memstats_alloc_bytes` series from the Go runtime collector

#### Scenario: Process metrics are exposed on Linux

- **WHEN** the `/metrics` endpoint is scraped on a Linux host
- **THEN** the response SHALL include the `process_resident_memory_bytes` and `process_cpu_seconds_total` series

#### Scenario: Metrics disabled registers no runtime collectors

- **WHEN** ShadowDNS starts with `-metrics-addr` set to an empty string
- **THEN** no Prometheus collectors SHALL be registered, including the Go runtime and process collectors

### Requirement: Count ECS option classifications

The system SHALL expose a counter metric `shadowdns_dns_ecs_queries_total` with labels `family` and `status`. The metric SHALL be incremented once for each query that carries an EDNS Client Subnet (ECS) option while ECS handling is enabled, partitioned by the classification outcome. The `status` label SHALL be one of `valid`, `opt_out`, or `malformed`, corresponding to the ECS classification. The `family` label SHALL be derived from the ECS option's address-family field, rendered as `ipv4` for family 1, `ipv6` for family 2, and `unknown` for any other value. When ECS handling is disabled, or when a query carries no ECS option, the system SHALL NOT increment this counter for that query.

#### Scenario: Valid ECS option increments the valid counter

- **WHEN** ECS handling is enabled and a query carries a well-formed ECS option with a non-zero source prefix length for an IPv4 subnet
- **THEN** `shadowdns_dns_ecs_queries_total{family="ipv4",status="valid"}` SHALL increase by 1

#### Scenario: ECS opt-out increments the opt_out counter

- **WHEN** ECS handling is enabled and a query carries an ECS option with source prefix length 0 (opt-out)
- **THEN** `shadowdns_dns_ecs_queries_total{status="opt_out"}` SHALL increase by 1

#### Scenario: Malformed ECS option increments the malformed counter before the error response

- **WHEN** ECS handling is enabled and a query carries a malformed ECS option
- **THEN** `shadowdns_dns_ecs_queries_total{status="malformed"}` SHALL increase by 1
- **AND** the system SHALL still reply with FORMERR as it did before this metric existed

#### Scenario: No increment when ECS handling is disabled

- **WHEN** ECS handling is disabled
- **THEN** the system SHALL NOT increment `shadowdns_dns_ecs_queries_total` for any query, regardless of whether the query carries an ECS option

### Requirement: Count view selections by ECS-geo participation

The system SHALL expose a counter metric `shadowdns_dns_view_selected_total` with labels `view` and `ecs_geo`. On the main query path, for each query whose view is successfully resolved, the system SHALL increment this counter exactly once with `view` set to the resolved view name. The `ecs_geo` label SHALL be `true` when an ECS-derived address was used as the geo-lookup address during view resolution for that query, and `false` otherwise. The label denotes that an ECS-derived geo address was available to the matcher for that query; it does not assert that an ECS-driven rule determined the resulting view. Queries that are refused before a view is resolved SHALL NOT increment this counter.

#### Scenario: View resolved using an ECS-derived geo address

- **WHEN** a query's view is resolved while a valid ECS-derived address is used as the geo-lookup address
- **THEN** `shadowdns_dns_view_selected_total{view="<resolved-view>",ecs_geo="true"}` SHALL increase by 1

#### Scenario: View resolved from the source IP only

- **WHEN** a query's view is resolved and no ECS-derived geo address is in use (ECS absent, opt-out, or disabled)
- **THEN** `shadowdns_dns_view_selected_total{view="<resolved-view>",ecs_geo="false"}` SHALL increase by 1

#### Scenario: No view matched does not increment

- **WHEN** a query matches no view and is refused
- **THEN** the system SHALL NOT increment `shadowdns_dns_view_selected_total` for that query
