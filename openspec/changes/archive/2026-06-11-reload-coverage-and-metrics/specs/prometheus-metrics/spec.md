## ADDED Requirements

### Requirement: Reload outcome is tracked by a total counter

The server SHALL expose a `shadowdns_reload_total` counter vector with a single label `result`. The only permitted label values are `"success"` and `"failure"`. The counter SHALL be registered in the ShadowDNS custom Prometheus registry (not the default global registry). Both label combinations SHALL be pre-initialised at registration time (via `WithLabelValues`) so that the series are present with value 0 from startup — alert expressions like `increase(shadowdns_reload_total{result="failure"}[5m])` then work without absent-metric special-casing.

#### Scenario: Counter is present at startup with value zero

- **WHEN** the server starts and no SIGHUP has yet been received
- **THEN** the `/metrics` endpoint SHALL expose `shadowdns_reload_total{result="success"} 0` and `shadowdns_reload_total{result="failure"} 0`

#### Scenario: Successful reload increments success label

- **WHEN** a SIGHUP reload completes without error
- **THEN** `shadowdns_reload_total{result="success"}` SHALL increase by exactly 1
- **THEN** `shadowdns_reload_total{result="failure"}` SHALL remain unchanged

#### Scenario: Failed reload increments failure label

- **WHEN** a SIGHUP reload returns an error (any step — parse, GeoIP, zone build, limiter construction, query-log sink)
- **THEN** `shadowdns_reload_total{result="failure"}` SHALL increase by exactly 1
- **THEN** `shadowdns_reload_total{result="success"}` SHALL remain unchanged

---

### Requirement: Last-reload-success timestamp is exposed as a gauge

The server SHALL expose a `shadowdns_config_last_reload_success_timestamp_seconds` gauge with no label dimensions (the `_seconds` unit suffix follows the Prometheus ecosystem convention, mirroring `prometheus_config_last_reload_success_timestamp_seconds`). Its value SHALL be the Unix timestamp (float64, seconds since epoch) of the most recent configuration load that completed without error: the gauge SHALL be initialised at startup once the initial configuration load succeeds (mirroring Prometheus's own startup behaviour, so `time() - <gauge>` staleness alert expressions work on servers that never reload), and SHALL be updated after each SIGHUP reload that completes without error. The registration-time `0` value SHALL NOT be externally observable — the metrics HTTP listener starts only after the startup initialisation.

#### Scenario: Gauge is initialised at startup

- **WHEN** the server starts with metrics enabled and the initial configuration load succeeds
- **THEN** `shadowdns_config_last_reload_success_timestamp_seconds` SHALL equal a float64 within 2 seconds of the startup time, not `0`

#### Scenario: Gauge is set after successful reload

- **WHEN** a SIGHUP reload completes successfully at time T
- **THEN** `shadowdns_config_last_reload_success_timestamp_seconds` SHALL equal a float64 within 2 seconds of `T.Unix()`

#### Scenario: Gauge is not changed by a failed reload

- **WHEN** a SIGHUP reload fails after the gauge was last set to value V (at startup or by a prior successful reload)
- **THEN** `shadowdns_config_last_reload_success_timestamp_seconds` SHALL still equal V
