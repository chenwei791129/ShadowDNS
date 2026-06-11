## ADDED Requirements

### Requirement: Reload metrics are emitted on every reload attempt

The server SHALL increment the `shadowdns_reload_total` counter with label `result="success"` after each successful SIGHUP reload, and with label `result="failure"` after each failed reload. A failure is defined as any error returned from the `reload()` function (configuration parse error, GeoIP reload error, zone build error, rate-limiter construction error, query-log sink open error). The counter SHALL be incremented exactly once per reload attempt regardless of which step fails. A SIGHUP coalesced away by the dispatch loop's drain (received while a reload is already in progress) triggers no reload attempt and therefore SHALL NOT increment the counter.

This capability owns only the emission triggers on the reload path. Metric registration, naming, label pre-initialisation, and exposition semantics are owned by the `prometheus-metrics` capability and are not restated here.

#### Scenario: Successful reload increments success counter

- **WHEN** a SIGHUP is received and the full reload sequence completes without error
- **THEN** `shadowdns_reload_total{result="success"}` SHALL increment by exactly 1
- **THEN** `shadowdns_reload_total{result="failure"}` SHALL NOT increment

#### Scenario: Failed reload increments failure counter

- **WHEN** a SIGHUP is received and any step in the reload sequence returns an error (e.g., named.conf parse error or GeoIP open error)
- **THEN** `shadowdns_reload_total{result="failure"}` SHALL increment by exactly 1
- **THEN** `shadowdns_reload_total{result="success"}` SHALL NOT increment

#### Scenario: Reload completes when metrics are disabled

- **WHEN** the server runs with metrics disabled (`--metrics-addr ""`) and a SIGHUP is received
- **THEN** the reload SHALL complete with its normal success or failure semantics (state swap, GeoIP reload, limiter rebuild, query-log re-apply all behave identically)
- **THEN** no metric SHALL be recorded and the process SHALL NOT panic or crash (all metrics methods on the reload path are nil-receiver safe; the rate-limiter recorder is not attached)

---

### Requirement: Last-reload-success timestamp is updated on successful reload

The server SHALL set the `shadowdns_config_last_reload_success_timestamp_seconds` gauge to the current Unix timestamp at the point where `reload()` is about to return nil, and SHALL NOT update it on any failure path. Gauge registration, naming, and the startup-initialisation semantics are owned by the `prometheus-metrics` capability; this capability owns only the reload-path update trigger.

#### Scenario: Gauge is updated after successful reload

- **WHEN** a SIGHUP reload completes successfully
- **THEN** `shadowdns_config_last_reload_success_timestamp_seconds` SHALL equal the Unix seconds of the time at which the reload function returned nil, within 2 seconds of wall time

#### Scenario: Gauge is not updated after failed reload

- **WHEN** a SIGHUP reload fails
- **THEN** `shadowdns_config_last_reload_success_timestamp_seconds` SHALL retain its previous value (the startup-initialisation value if no reload has succeeded yet)

---

### Requirement: All fallible reload steps precede the state swap

The reload sequence SHALL perform every step that can fail — named.conf parse, ShadowDNS config load, GeoIP database open, server state build, rate-limiter construction, and query-log sink open — before `SwapState` is called. Steps executed after `SwapState` (installing the new rate limiter, query-log logger, and GeoIP handles; closing the superseded query-log sink; rotating superseded GeoIP handles into the deferred-close slot; clearing the ephemeral store; recording metrics) SHALL be infallible installation steps that cannot abort the reload. This guarantees a failed reload never leaves a partially applied configuration.

#### Scenario: Any fallible step failure preserves the full previous runtime state

- **WHEN** any fallible reload step returns an error (parse, GeoIP open, state build, limiter construction, or query-log sink open)
- **THEN** the previous server state SHALL remain active in full: zone data, view matching, rate limiter, query-log logger, and GeoIP handles are all unchanged
- **THEN** `shadowdns_reload_total{result="failure"}` SHALL increment and `reload()` SHALL return a non-nil error

---

### Requirement: GeoIP databases are reloaded on SIGHUP

The server SHALL re-open the GeoIP country and ASN mmdb files from the `geoip-directory` path on every SIGHUP reload. The new `*CountryDB` and `*ASNDB` handles SHALL be used when building the new server state. After the state swap, the superseded DB handles SHALL NOT be closed immediately — in-flight queries can still resolve views against the previous state, and closing an mmdb unmaps its memory (use-after-munmap is a fatal, unrecoverable crash). Superseded handles SHALL instead be retained and closed at the start of the next reload, or at process shutdown after the reload goroutine has been joined, whichever comes first (deferred-by-one-generation close). If either mmdb cannot be opened, the reload SHALL fail and the server SHALL retain the previous server state and the previous DB handles. If the reloaded named.conf carries an empty `geoip-directory`, the reload SHALL fail with an explicit configuration error (mirroring the startup validation) rather than a relative-path file-open error.

#### Scenario: GeoIP databases reloaded after mmdb file update

- **WHEN** the operator places updated mmdb files in the configured `geoip-directory` and sends SIGHUP
- **THEN** the server SHALL open new DB handles from the updated files and build the new state with them
- **THEN** subsequent DNS queries SHALL use the updated GeoIP data for view matching

#### Scenario: GeoIP reload failure preserves existing state

- **WHEN** the mmdb files are temporarily unavailable (removed or permission-denied) and SIGHUP is received
- **THEN** `reload()` SHALL return an error, the previous server state SHALL remain active, and the previous DB handles SHALL remain in use
- **THEN** `shadowdns_reload_total{result="failure"}` SHALL increment

#### Scenario: GeoIP db_info gauge updated after successful reload

- **WHEN** a SIGHUP reload completes successfully with new mmdb files whose build epochs differ from the startup values
- **THEN** `shadowdns_geoip_db_info{database="country",build_time="<new-ISO8601>"}` and `shadowdns_geoip_db_info{database="asn",build_time="<new-ISO8601>"}` SHALL be set to 1
- **THEN** the gauge series carrying the previous `build_time` label values SHALL be deleted, so at most one `build_time` series exists per `database` label at any time

#### Scenario: Superseded GeoIP handles are closed deferred, never immediately after the swap

- **WHEN** a SIGHUP reload completes successfully and replaces the GeoIP handles
- **THEN** the superseded handles SHALL remain open and usable (in-flight queries holding the previous state snapshot can still perform lookups against them)
- **THEN** the superseded handles SHALL be closed at the start of the next reload, or at process shutdown after the reload goroutine has finished — and at no other time

##### Example: handle lifecycle across two reloads

| Event | gen-1 handles (startup) | gen-2 handles | gen-3 handles |
| ----- | ----------------------- | ------------- | ------------- |
| startup | current (open) | — | — |
| reload #1 succeeds | prev (open, deferred) | current (open) | — |
| reload #2 begins (step 0) | closed | current (open) | — |
| reload #2 succeeds | closed | prev (open, deferred) | current (open) |
| shutdown (after reload-goroutine join) | closed | closed | closed |

## REMOVED Requirements

### Requirement: GeoIP databases are not reloaded

**Reason**: This constraint is lifted by the reload-coverage-and-metrics change. GeoIP mmdb files are now re-opened on every SIGHUP to allow MaxMind monthly updates to take effect without a full process restart.

**Migration**: No operator action required. The behavior change is transparent: SIGHUP now reloads GeoIP in addition to zones and aliases. The `shadowdns_geoip_db_info` gauge will reflect the new build_time after a successful reload.

#### Scenario: GeoIP databases are now reloaded instead of retained on SIGHUP

- **WHEN** updated mmdb files are placed in the `geoip-directory` and SIGHUP is received
- **THEN** the server SHALL open new DB handles (this requirement is REMOVED — the old behavior of retaining startup handles SHALL NOT apply)
- **THEN** operators SHALL NOT need to restart the process to pick up MaxMind monthly db updates
