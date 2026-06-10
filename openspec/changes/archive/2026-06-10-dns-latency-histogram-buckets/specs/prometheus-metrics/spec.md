## MODIFIED Requirements

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
