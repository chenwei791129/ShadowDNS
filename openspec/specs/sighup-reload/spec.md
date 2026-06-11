# sighup-reload Specification

## Purpose

TBD - created by archiving change 'sighup-reload'. Update Purpose after archive.

## Requirements

### Requirement: SIGHUP triggers configuration reload

The server process SHALL listen for the SIGHUP signal. Upon receiving SIGHUP, the server SHALL re-read the named.conf configuration file and the aliases file unconditionally. For each zone file referenced by the configuration, the server SHALL compute a fingerprint and compare it to the fingerprint recorded during the previous load; the server SHALL re-parse only zone files whose fingerprint has changed, new zone files that had no previous fingerprint, and (when verify mode is `none`) all zone files unconditionally. Zone files whose fingerprint is unchanged SHALL reuse the previously parsed `*zone.Zone` object by pointer. After constructing the new server state, the server SHALL replace the in-memory server state atomically.

#### Scenario: Successful reload after zone file update

- **WHEN** a zone file on disk is modified and the operator sends SIGHUP to the server process
- **THEN** the server SHALL detect the fingerprint change, re-parse only the modified zone file, reuse pointers for all other unchanged zone files, and atomically replace the current state
- **THEN** subsequent DNS queries SHALL be answered using the new state

#### Scenario: Successful reload after aliases file update

- **WHEN** the aliases file is modified and the operator sends SIGHUP to the server process
- **THEN** the server SHALL reload the aliases file and rebuild the server state with the updated alias map
- **THEN** zone files whose fingerprints are unchanged SHALL have their `*zone.Zone` pointers reused in the new state

#### Scenario: Successful reload after named.conf update

- **WHEN** named.conf is modified (e.g., a new zone is added or a view's match-clients is changed) and the operator sends SIGHUP to the server process
- **THEN** the server SHALL reload named.conf and rebuild the server state reflecting the new configuration
- **THEN** newly added zones SHALL be parsed fresh, removed zones SHALL be dropped, and unchanged zones SHALL reuse their `*zone.Zone` pointer

#### Scenario: First reload with no previous fingerprint

- **WHEN** the server starts up and performs its initial state build before any SIGHUP has been received
- **THEN** the server SHALL parse every zone file referenced by the configuration (no previous fingerprint exists)
- **THEN** the server SHALL record a fingerprint for each parsed zone file for use by subsequent reloads


<!-- @trace
source: diff-based-reload
updated: 2026-04-17
code:
  - CHANGELOG.md
  - go.mod
  - internal/server/fingerprint.go
  - internal/server/server.go
  - testdata/integration/master/example.com_view-th.fwd
  - internal/server/build.go
  - .release-please-manifest.json
  - README.md
  - internal/alias/override.go
  - internal/zone/zone.go
  - Makefile
  - internal/server/handler.go
  - cmd/shadowdns/main.go
  - testdata/integration/master/example.com_view-other.fwd
tests:
  - internal/zone/zone_test.go
  - internal/zone/parser_test.go
  - internal/alias/override_test.go
  - test/integration/negative_test.go
  - test/integration/helpers_test.go
  - test/integration/listenon_test.go
  - internal/server/server_test.go
  - test/integration/reload_diff_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/axfr_test.go
  - test/integration/wildcard_test.go
  - test/integration/cname_following_test.go
  - internal/server/fingerprint_test.go
  - internal/server/build_test.go
  - test/integration/cname_synthesis_test.go
-->

---
### Requirement: Zero-downtime state replacement

The server SHALL replace its in-memory state using an atomic pointer swap mechanism. DNS queries that are in-flight at the time of the swap SHALL complete using the state snapshot they obtained at the start of their processing.

#### Scenario: In-flight query during reload

- **WHEN** a DNS query is being processed and a reload occurs concurrently
- **THEN** the in-flight query SHALL complete using the server state it loaded at the start of its processing
- **THEN** the query SHALL NOT observe partially updated state

#### Scenario: New query after reload

- **WHEN** a reload completes successfully and a new DNS query arrives
- **THEN** the new query SHALL be processed using the newly loaded server state


<!-- @trace
source: sighup-reload
updated: 2026-04-14
code:
  - internal/server/server.go
  - cmd/shadowdns/main.go
  - internal/server/handler.go
tests:
  - test/integration/backup_test.go
  - test/integration/negative_test.go
  - test/integration/query_test.go
  - internal/zone/classify_test.go
  - internal/zone/zone_test.go
  - cmd/shadowdns/main_test.go
  - internal/server/server_test.go
  - test/integration/axfr_test.go
  - internal/zone/parser_test.go
-->

---
### Requirement: Reload failure preserves existing state

When any step of the reload process fails (configuration parse error, zone file read error, state build error), the server SHALL log an error message describing the failure, retain the previously loaded state, and continue serving DNS queries without interruption.

#### Scenario: Malformed zone file during reload

- **WHEN** a zone file contains a syntax error and the operator sends SIGHUP
- **THEN** the server SHALL log an error identifying the problematic zone file and the nature of the error
- **THEN** the server SHALL continue serving queries using the state from before the reload attempt

#### Scenario: Missing zone file during reload

- **WHEN** a zone file referenced in named.conf does not exist on disk and the operator sends SIGHUP
- **THEN** the server SHALL log an error and continue serving queries using the previous state

#### Scenario: Malformed named.conf during reload

- **WHEN** named.conf contains a syntax error and the operator sends SIGHUP
- **THEN** the server SHALL log an error and continue serving queries using the previous state


<!-- @trace
source: sighup-reload
updated: 2026-04-14
code:
  - internal/server/server.go
  - cmd/shadowdns/main.go
  - internal/server/handler.go
tests:
  - test/integration/backup_test.go
  - test/integration/negative_test.go
  - test/integration/query_test.go
  - internal/zone/classify_test.go
  - internal/zone/zone_test.go
  - cmd/shadowdns/main_test.go
  - internal/server/server_test.go
  - test/integration/axfr_test.go
  - internal/zone/parser_test.go
-->

---
### Requirement: NOTIFY dispatch after successful reload

After a successful reload, the server SHALL dispatch NOTIFY messages to the NS targets of all root zones, following the same logic and deduplication used at startup.

#### Scenario: NOTIFY sent after reload

- **WHEN** a reload completes successfully
- **THEN** the server SHALL send NOTIFY messages to all NS targets of the reloaded root zones
- **THEN** NOTIFY failures SHALL be logged but SHALL NOT affect the reload outcome


<!-- @trace
source: sighup-reload
updated: 2026-04-14
code:
  - internal/server/server.go
  - cmd/shadowdns/main.go
  - internal/server/handler.go
tests:
  - test/integration/backup_test.go
  - test/integration/negative_test.go
  - test/integration/query_test.go
  - internal/zone/classify_test.go
  - internal/zone/zone_test.go
  - cmd/shadowdns/main_test.go
  - internal/server/server_test.go
  - test/integration/axfr_test.go
  - internal/zone/parser_test.go
-->

---
### Requirement: Reload does not restart listeners

The server SHALL NOT close or rebind UDP/TCP listeners during a reload. Only the in-memory server state SHALL be replaced.

#### Scenario: Listeners remain bound during reload

- **WHEN** a reload is triggered via SIGHUP
- **THEN** the UDP and TCP listeners SHALL remain bound to their original addresses throughout the reload process
- **THEN** no incoming connections or packets SHALL be dropped due to listener restart


<!-- @trace
source: sighup-reload
updated: 2026-04-14
code:
  - internal/server/server.go
  - cmd/shadowdns/main.go
  - internal/server/handler.go
tests:
  - test/integration/backup_test.go
  - test/integration/negative_test.go
  - test/integration/query_test.go
  - internal/zone/classify_test.go
  - internal/zone/zone_test.go
  - cmd/shadowdns/main_test.go
  - internal/server/server_test.go
  - test/integration/axfr_test.go
  - internal/zone/parser_test.go
-->

---
### Requirement: Reload logging

The server SHALL log the start and outcome of each reload attempt at INFO level.

#### Scenario: Reload start is logged

- **WHEN** the server receives SIGHUP
- **THEN** the server SHALL log an INFO message indicating that a reload has been initiated

#### Scenario: Reload success is logged

- **WHEN** a reload completes successfully
- **THEN** the server SHALL log an INFO message indicating successful reload, including the count of views and zones loaded

#### Scenario: Reload failure is logged

- **WHEN** a reload fails
- **THEN** the server SHALL log an ERROR message with the specific error that caused the failure

<!-- @trace
source: sighup-reload
updated: 2026-04-14
code:
  - internal/server/server.go
  - cmd/shadowdns/main.go
  - internal/server/handler.go
tests:
  - test/integration/backup_test.go
  - test/integration/negative_test.go
  - test/integration/query_test.go
  - internal/zone/classify_test.go
  - internal/zone/zone_test.go
  - cmd/shadowdns/main_test.go
  - internal/server/server_test.go
  - test/integration/axfr_test.go
  - internal/zone/parser_test.go
-->

---
### Requirement: Zone file fingerprint strategy

The server SHALL compute a fingerprint for each zone file consisting of a size component and a content-hash component. The size component SHALL be the file size in bytes obtained via a single `os.Stat` call. The content-hash component SHALL be the xxhash64 of the full file contents computed using `github.com/cespare/xxhash/v2`. A zone file's fingerprint SHALL be treated as unchanged if and only if both the size component and the content-hash component match the fingerprint recorded during the previous successful load. Fingerprints SHALL be stored in the server state so that each reload compares against the fingerprints produced by the immediately preceding successful load.

#### Scenario: Unchanged zone file is detected as unchanged

- **WHEN** a zone file has the same size and the same content as at the previous load
- **THEN** the xxhash64 computed from its contents SHALL equal the previously recorded hash
- **THEN** the fingerprint comparison SHALL return unchanged and the `*zone.Zone` pointer SHALL be reused

#### Scenario: Zone file with same size but different content is detected as changed

- **WHEN** a zone file has been modified such that its size is identical to the previous load but its contents differ (e.g., after `rsync -avc --inplace` from a source that preserves mtime)
- **THEN** the xxhash64 SHALL differ from the previously recorded hash
- **THEN** the fingerprint comparison SHALL return changed and the zone file SHALL be re-parsed

#### Scenario: Zone file with different size is detected as changed

- **WHEN** a zone file's size differs from the previously recorded size
- **THEN** the fingerprint comparison SHALL return changed without necessarily computing the content hash
- **THEN** the zone file SHALL be re-parsed


<!-- @trace
source: diff-based-reload
updated: 2026-04-17
code:
  - CHANGELOG.md
  - go.mod
  - internal/server/fingerprint.go
  - internal/server/server.go
  - testdata/integration/master/example.com_view-th.fwd
  - internal/server/build.go
  - .release-please-manifest.json
  - README.md
  - internal/alias/override.go
  - internal/zone/zone.go
  - Makefile
  - internal/server/handler.go
  - cmd/shadowdns/main.go
  - testdata/integration/master/example.com_view-other.fwd
tests:
  - internal/zone/zone_test.go
  - internal/zone/parser_test.go
  - internal/alias/override_test.go
  - test/integration/negative_test.go
  - test/integration/helpers_test.go
  - test/integration/listenon_test.go
  - internal/server/server_test.go
  - test/integration/reload_diff_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/axfr_test.go
  - test/integration/wildcard_test.go
  - test/integration/cname_following_test.go
  - internal/server/fingerprint_test.go
  - internal/server/build_test.go
  - test/integration/cname_synthesis_test.go
-->

---
### Requirement: Reload verify mode configuration

The server SHALL expose a CLI flag `--reload-verify` that accepts exactly one of the values `hash`, `size`, or `none`. The default value SHALL be `hash`. The value SHALL be read at process startup from `os.Args` and SHALL remain sticky across SIGHUP reloads for the entire process lifetime. The server SHALL reject startup with a non-zero exit code if `--reload-verify` is set to any value other than `hash`, `size`, or `none`. The fingerprint comparison behavior SHALL be selected by this flag as follows:

- `hash`: The server SHALL compute and compare both the size component and the xxhash64 content-hash component.
- `size`: The server SHALL compare only the size component and the file modification time (ns precision), and SHALL NOT read zone file contents for fingerprinting.
- `none`: The server SHALL NOT compute any fingerprint and SHALL re-parse every zone file unconditionally, matching the pre-optimization reload behavior.

#### Scenario: Default reload verify mode is hash

- **WHEN** the server is started without the `--reload-verify` flag
- **THEN** the effective verify mode SHALL be `hash`
- **THEN** subsequent reloads SHALL compute xxhash64 for zone files whose size matches

#### Scenario: Explicit size mode skips content hashing

- **WHEN** the server is started with `--reload-verify=size` and a reload is triggered
- **THEN** the server SHALL compare only `(mtime, size)` fingerprints and SHALL NOT read any zone file contents for fingerprinting purposes
- **THEN** zone files with identical `(mtime, size)` SHALL be treated as unchanged and their pointers reused

#### Scenario: None mode forces full rebuild

- **WHEN** the server is started with `--reload-verify=none` and a reload is triggered
- **THEN** the server SHALL re-parse every zone file referenced by the configuration regardless of any fingerprint
- **THEN** no zone `*zone.Zone` pointer SHALL be reused from the previous state

#### Scenario: Invalid reload verify value rejected at startup

- **WHEN** the server is started with `--reload-verify=foo` (any value other than `hash`, `size`, or `none`)
- **THEN** the server SHALL print an error identifying the invalid value and the set of accepted values
- **THEN** the server SHALL exit with a non-zero exit code before binding listeners


<!-- @trace
source: diff-based-reload
updated: 2026-04-17
code:
  - CHANGELOG.md
  - go.mod
  - internal/server/fingerprint.go
  - internal/server/server.go
  - testdata/integration/master/example.com_view-th.fwd
  - internal/server/build.go
  - .release-please-manifest.json
  - README.md
  - internal/alias/override.go
  - internal/zone/zone.go
  - Makefile
  - internal/server/handler.go
  - cmd/shadowdns/main.go
  - testdata/integration/master/example.com_view-other.fwd
tests:
  - internal/zone/zone_test.go
  - internal/zone/parser_test.go
  - internal/alias/override_test.go
  - test/integration/negative_test.go
  - test/integration/helpers_test.go
  - test/integration/listenon_test.go
  - internal/server/server_test.go
  - test/integration/reload_diff_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/axfr_test.go
  - test/integration/wildcard_test.go
  - test/integration/cname_following_test.go
  - internal/server/fingerprint_test.go
  - internal/server/build_test.go
  - test/integration/cname_synthesis_test.go
-->

---
### Requirement: Diff-based zone pointer reuse preserves immutability

When the server reuses a `*zone.Zone` pointer from the previous state in a newly built state, the server SHALL NOT mutate any field of the reused `*zone.Zone` object, including its `Records` map, `SOA`, `Role`, `Origin`, or `Path` fields. Any handler or state-building code that needs to modify zone data SHALL construct a new `*zone.Zone` rather than mutate a shared one.

#### Scenario: Reused zone is not mutated by new state construction

- **WHEN** a zone file's fingerprint is unchanged and its `*zone.Zone` pointer is reused in the new state
- **THEN** no field of the reused `*zone.Zone` SHALL be modified by the reload path
- **THEN** DNS queries served from the old state (in-flight during the swap) and queries served from the new state SHALL observe identical zone data for that zone


<!-- @trace
source: diff-based-reload
updated: 2026-04-17
code:
  - CHANGELOG.md
  - go.mod
  - internal/server/fingerprint.go
  - internal/server/server.go
  - testdata/integration/master/example.com_view-th.fwd
  - internal/server/build.go
  - .release-please-manifest.json
  - README.md
  - internal/alias/override.go
  - internal/zone/zone.go
  - Makefile
  - internal/server/handler.go
  - cmd/shadowdns/main.go
  - testdata/integration/master/example.com_view-other.fwd
tests:
  - internal/zone/zone_test.go
  - internal/zone/parser_test.go
  - internal/alias/override_test.go
  - test/integration/negative_test.go
  - test/integration/helpers_test.go
  - test/integration/listenon_test.go
  - internal/server/server_test.go
  - test/integration/reload_diff_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/axfr_test.go
  - test/integration/wildcard_test.go
  - test/integration/cname_following_test.go
  - internal/server/fingerprint_test.go
  - internal/server/build_test.go
  - test/integration/cname_synthesis_test.go
-->

---
### Requirement: Post-swap garbage collection

After `Server.SwapState` stores the new state pointer, the server SHALL invoke `runtime.GC()` followed by `runtime/debug.FreeOSMemory()` exactly once per successful state swap. This SHALL apply regardless of the `--reload-verify` mode.

#### Scenario: GC and memory release invoked after successful swap

- **WHEN** `SwapState` is called with a newly built state and the atomic pointer store completes
- **THEN** the server SHALL invoke `runtime.GC()` synchronously
- **THEN** the server SHALL invoke `debug.FreeOSMemory()` synchronously
- **THEN** the operating system resident set size SHALL begin decreasing toward the post-reload steady state without waiting for the runtime's background GC cycle

#### Scenario: GC is not invoked on failed reload

- **WHEN** reload fails before `SwapState` is called (e.g., due to a zone parse error)
- **THEN** `runtime.GC()` and `debug.FreeOSMemory()` SHALL NOT be invoked by the reload path
- **THEN** the previously loaded state SHALL continue serving queries


<!-- @trace
source: diff-based-reload
updated: 2026-04-17
code:
  - CHANGELOG.md
  - go.mod
  - internal/server/fingerprint.go
  - internal/server/server.go
  - testdata/integration/master/example.com_view-th.fwd
  - internal/server/build.go
  - .release-please-manifest.json
  - README.md
  - internal/alias/override.go
  - internal/zone/zone.go
  - Makefile
  - internal/server/handler.go
  - cmd/shadowdns/main.go
  - testdata/integration/master/example.com_view-other.fwd
tests:
  - internal/zone/zone_test.go
  - internal/zone/parser_test.go
  - internal/alias/override_test.go
  - test/integration/negative_test.go
  - test/integration/helpers_test.go
  - test/integration/listenon_test.go
  - internal/server/server_test.go
  - test/integration/reload_diff_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/axfr_test.go
  - test/integration/wildcard_test.go
  - test/integration/cname_following_test.go
  - internal/server/fingerprint_test.go
  - internal/server/build_test.go
  - test/integration/cname_synthesis_test.go
-->

---
### Requirement: Reload diff logging

On each successful reload, the server SHALL log at INFO level the count of zones that were reused by pointer and the count of zones that were re-parsed, per view. These counts SHALL allow operators to verify that the diff-based reload is reusing pointers as expected.

#### Scenario: Reload log reports reuse counts

- **WHEN** a reload completes successfully
- **THEN** the server SHALL emit an INFO log entry containing at minimum: the effective reload verify mode, the total number of zones reused, and the total number of zones re-parsed
- **THEN** when verify mode is `none`, the reused count SHALL be zero and the re-parsed count SHALL equal the total zone count

<!-- @trace
source: diff-based-reload
updated: 2026-04-17
code:
  - CHANGELOG.md
  - go.mod
  - internal/server/fingerprint.go
  - internal/server/server.go
  - testdata/integration/master/example.com_view-th.fwd
  - internal/server/build.go
  - .release-please-manifest.json
  - README.md
  - internal/alias/override.go
  - internal/zone/zone.go
  - Makefile
  - internal/server/handler.go
  - cmd/shadowdns/main.go
  - testdata/integration/master/example.com_view-other.fwd
tests:
  - internal/zone/zone_test.go
  - internal/zone/parser_test.go
  - internal/alias/override_test.go
  - test/integration/negative_test.go
  - test/integration/helpers_test.go
  - test/integration/listenon_test.go
  - internal/server/server_test.go
  - test/integration/reload_diff_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/axfr_test.go
  - test/integration/wildcard_test.go
  - test/integration/cname_following_test.go
  - internal/server/fingerprint_test.go
  - internal/server/build_test.go
  - test/integration/cname_synthesis_test.go
-->

---
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

<!-- @trace
source: reload-coverage-and-metrics
updated: 2026-06-11
-->


<!-- @trace
source: reload-coverage-and-metrics
updated: 2026-06-11
code:
  - README.md
  - internal/metrics/metrics.go
  - internal/logging/reopen.go
  - internal/server/server.go
  - internal/server/handler.go
  - cmd/shadowdns/main.go
tests:
  - cmd/shadowdns/main_test.go
  - cmd/shadowdns/querylog_test.go
  - cmd/shadowdns/main_reload_test.go
  - internal/metrics/metrics_reload_test.go
  - cmd/shadowdns/main_ephemeral_test.go
  - internal/server/handler_querylog_test.go
  - internal/logging/reopen_test.go
  - internal/metrics/metrics_test.go
  - internal/server/handler_ratelimit_test.go
-->

---
### Requirement: Last-reload-success timestamp is updated on successful reload

The server SHALL set the `shadowdns_config_last_reload_success_timestamp_seconds` gauge to the current Unix timestamp at the point where `reload()` is about to return nil, and SHALL NOT update it on any failure path. Gauge registration, naming, and the startup-initialisation semantics are owned by the `prometheus-metrics` capability; this capability owns only the reload-path update trigger.

#### Scenario: Gauge is updated after successful reload

- **WHEN** a SIGHUP reload completes successfully
- **THEN** `shadowdns_config_last_reload_success_timestamp_seconds` SHALL equal the Unix seconds of the time at which the reload function returned nil, within 2 seconds of wall time

#### Scenario: Gauge is not updated after failed reload

- **WHEN** a SIGHUP reload fails
- **THEN** `shadowdns_config_last_reload_success_timestamp_seconds` SHALL retain its previous value (the startup-initialisation value if no reload has succeeded yet)

<!-- @trace
source: reload-coverage-and-metrics
updated: 2026-06-11
-->


<!-- @trace
source: reload-coverage-and-metrics
updated: 2026-06-11
code:
  - README.md
  - internal/metrics/metrics.go
  - internal/logging/reopen.go
  - internal/server/server.go
  - internal/server/handler.go
  - cmd/shadowdns/main.go
tests:
  - cmd/shadowdns/main_test.go
  - cmd/shadowdns/querylog_test.go
  - cmd/shadowdns/main_reload_test.go
  - internal/metrics/metrics_reload_test.go
  - cmd/shadowdns/main_ephemeral_test.go
  - internal/server/handler_querylog_test.go
  - internal/logging/reopen_test.go
  - internal/metrics/metrics_test.go
  - internal/server/handler_ratelimit_test.go
-->

---
### Requirement: All fallible reload steps precede the state swap

The reload sequence SHALL perform every step that can fail — named.conf parse, ShadowDNS config load, GeoIP database open, server state build, rate-limiter construction, and query-log sink open — before `SwapState` is called. Steps executed after `SwapState` (installing the new rate limiter, query-log logger, and GeoIP handles; closing the superseded query-log sink; rotating superseded GeoIP handles into the deferred-close slot; clearing the ephemeral store; recording metrics) SHALL be infallible installation steps that cannot abort the reload. This guarantees a failed reload never leaves a partially applied configuration.

#### Scenario: Any fallible step failure preserves the full previous runtime state

- **WHEN** any fallible reload step returns an error (parse, GeoIP open, state build, limiter construction, or query-log sink open)
- **THEN** the previous server state SHALL remain active in full: zone data, view matching, rate limiter, query-log logger, and GeoIP handles are all unchanged
- **THEN** `shadowdns_reload_total{result="failure"}` SHALL increment and `reload()` SHALL return a non-nil error

<!-- @trace
source: reload-coverage-and-metrics
updated: 2026-06-11
-->


<!-- @trace
source: reload-coverage-and-metrics
updated: 2026-06-11
code:
  - README.md
  - internal/metrics/metrics.go
  - internal/logging/reopen.go
  - internal/server/server.go
  - internal/server/handler.go
  - cmd/shadowdns/main.go
tests:
  - cmd/shadowdns/main_test.go
  - cmd/shadowdns/querylog_test.go
  - cmd/shadowdns/main_reload_test.go
  - internal/metrics/metrics_reload_test.go
  - cmd/shadowdns/main_ephemeral_test.go
  - internal/server/handler_querylog_test.go
  - internal/logging/reopen_test.go
  - internal/metrics/metrics_test.go
  - internal/server/handler_ratelimit_test.go
-->

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

<!-- @trace
source: reload-coverage-and-metrics
updated: 2026-06-11
-->

<!-- @trace
source: reload-coverage-and-metrics
updated: 2026-06-11
code:
  - README.md
  - internal/metrics/metrics.go
  - internal/logging/reopen.go
  - internal/server/server.go
  - internal/server/handler.go
  - cmd/shadowdns/main.go
tests:
  - cmd/shadowdns/main_test.go
  - cmd/shadowdns/querylog_test.go
  - cmd/shadowdns/main_reload_test.go
  - internal/metrics/metrics_reload_test.go
  - cmd/shadowdns/main_ephemeral_test.go
  - internal/server/handler_querylog_test.go
  - internal/logging/reopen_test.go
  - internal/metrics/metrics_test.go
  - internal/server/handler_ratelimit_test.go
-->