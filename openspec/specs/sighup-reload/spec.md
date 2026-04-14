# sighup-reload Specification

## Purpose

TBD - created by archiving change 'sighup-reload'. Update Purpose after archive.

## Requirements

### Requirement: SIGHUP triggers configuration reload

The server process SHALL listen for the SIGHUP signal. Upon receiving SIGHUP, the server SHALL re-read the named.conf configuration file, the aliases file, and all zone files referenced by the configuration, then replace the in-memory server state atomically.

#### Scenario: Successful reload after zone file update

- **WHEN** a zone file on disk is modified and the operator sends SIGHUP to the server process
- **THEN** the server SHALL parse the updated configuration and zone files, build a new server state, and atomically replace the current state
- **THEN** subsequent DNS queries SHALL be answered using the new state

#### Scenario: Successful reload after aliases file update

- **WHEN** the aliases file is modified and the operator sends SIGHUP to the server process
- **THEN** the server SHALL reload the aliases file and rebuild the server state with the updated alias map

#### Scenario: Successful reload after named.conf update

- **WHEN** named.conf is modified (e.g., a new zone is added or a view's match-clients is changed) and the operator sends SIGHUP to the server process
- **THEN** the server SHALL reload named.conf and rebuild the server state reflecting the new configuration


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
### Requirement: GeoIP databases are not reloaded

The server SHALL NOT reload GeoIP mmdb databases during a SIGHUP reload. The GeoIP database handles loaded at startup SHALL be reused for building the new server state.

#### Scenario: GeoIP handles reused after reload

- **WHEN** a reload is triggered via SIGHUP
- **THEN** the server SHALL use the same GeoIP country and ASN database handles that were loaded at startup
- **THEN** the server SHALL NOT attempt to re-open or re-read the mmdb files


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