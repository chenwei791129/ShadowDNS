## Requirements

### Requirement: Emit BIND9-compatible query log lines

When query logging is enabled via the parsed `logging{}` configuration, the dns-server SHALL append one line to the configured query log file for every DNS query whose client IP resolves to a view. With all three print options enabled, the line SHALL have the exact form:

```
<dd-Mmm-yyyy HH:MM:SS.mmm> queries: info: client @0x<hex> <client-ip>#<port> (<qname>): view <view-name>: query: <qname> <class> <qtype> <flags> (<local-ip>)
```

where `<qname>` is the on-wire (case-preserved) query name in DNS presentation format (label special characters escaped per RFC 1035 master-file conventions, matching BIND's rendering) with the trailing dot removed (a root query SHALL render as `.`), `<class>` and `<qtype>` use the standard DNS mnemonics with RFC 3597 fallback (`CLASS<n>` / `TYPE<n>`), `@0x<hex>` is a synthetic lowercase-hex token without zero-padding derived from a process-local atomic counter, and `<local-ip>` is the IP (without port) of the local address the query was received on. Segments SHALL be separated by single spaces with no residual whitespace when optional segments are omitted.

#### Scenario: View-matched query produces one log line

- **WHEN** a client in view `view-eu` sends a UDP query for `www.example.com` type A with RD=0, EDNS version 0, DO=1, CD=1
- **THEN** exactly one line is appended to the query log file

##### Example: full line with all print options enabled

- **GIVEN** local time `07-Jun-2026 05:59:41.389`, client `192.0.2.10#16361`, local address `198.51.100.7`, synthetic token `@0x2a`
- **WHEN** the query above is received
- **THEN** the appended line is exactly `07-Jun-2026 05:59:41.389 queries: info: client @0x2a 192.0.2.10#16361 (www.example.com): view view-eu: query: www.example.com IN A -E(0)DC (198.51.100.7)`

#### Scenario: Query name case and trailing dot handling

- **WHEN** a client sends a query for `WwW.ExAmPlE.cOm.` (on-wire mixed case)
- **THEN** both `<qname>` occurrences in the line render as `WwW.ExAmPlE.cOm` (case preserved, trailing dot removed)


<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
-->


<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
code:
  - packaging/named.conf.example
  - internal/config/zones.go
  - NOTES.md
  - cmd/shadowdns/main.go
  - internal/config/logging.go
  - README.md
  - internal/querylog/querylog.go
  - internal/server/server.go
  - internal/server/handler.go
tests:
  - internal/server/handler_querylog_test.go
  - internal/config/zones_test.go
  - cmd/shadowdns/querylog_test.go
  - internal/querylog/querylog_test.go
  - internal/config/logging_test.go
-->

---
### Requirement: Honor print-time, print-category, and print-severity

The query logger SHALL include or omit the timestamp, the `queries: ` category segment, and the `info: ` severity segment according to the parsed channel's `print-time`, `print-category`, and `print-severity` values. `print-time` values `yes` and `local` SHALL render local time as `dd-Mmm-yyyy HH:MM:SS.mmm`; `iso8601` SHALL render local time as `yyyy-MM-ddTHH:MM:SS.mmm`; `iso8601-utc` SHALL render UTC time in the same ISO 8601 layout; `no` SHALL omit the timestamp segment.

#### Scenario: All print options disabled

- **WHEN** the channel declares `print-time no; print-category no; print-severity no;` and a view-matched query is received
- **THEN** the line begins directly with `client @0x`

##### Example: print option combinations

| print-time | print-category | print-severity | Line prefix before `client @0x` |
| ---------- | -------------- | -------------- | ------------------------------- |
| yes | yes | yes | `07-Jun-2026 05:59:41.389 queries: info: ` |
| no | yes | yes | `queries: info: ` |
| yes | no | no | `07-Jun-2026 05:59:41.389 ` |
| no | no | no | (empty) |


<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
-->


<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
code:
  - packaging/named.conf.example
  - internal/config/zones.go
  - NOTES.md
  - cmd/shadowdns/main.go
  - internal/config/logging.go
  - README.md
  - internal/querylog/querylog.go
  - internal/server/server.go
  - internal/server/handler.go
tests:
  - internal/server/handler_querylog_test.go
  - internal/config/zones_test.go
  - cmd/shadowdns/querylog_test.go
  - internal/querylog/querylog_test.go
  - internal/config/logging_test.go
-->

---
### Requirement: Render the query flags field as the supported BIND9 subset

The flags field SHALL begin with `+` when the request has RD=1 and `-` when RD=0, followed in order and without separators by: `E(<version>)` when the request carries an EDNS OPT record, `T` when the query arrived over TCP, `D` when the DO bit is set, `C` when the CD bit is set, and `K` when the request carries an EDNS COOKIE option. The logger SHALL NOT emit `S` (TSIG) or `V` (valid server cookie) because ShadowDNS implements neither TSIG verification nor DNS COOKIE validation.

#### Scenario: Flag rendering across request shapes

- **WHEN** queries with differing header bits and EDNS contents are received
- **THEN** the flags field renders the corresponding subset

##### Example: flag field values

| Request shape | Flags |
| ------------- | ----- |
| RD=0, EDNS v0, UDP, DO=1, CD=1 | `-E(0)DC` |
| RD=0, EDNS v0, UDP, DO=1, CD=1, COOKIE option present | `-E(0)DCK` |
| RD=1, no EDNS, TCP | `+T` |
| RD=0, no EDNS, UDP | `-` |


<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
-->


<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
code:
  - packaging/named.conf.example
  - internal/config/zones.go
  - NOTES.md
  - cmd/shadowdns/main.go
  - internal/config/logging.go
  - README.md
  - internal/querylog/querylog.go
  - internal/server/server.go
  - internal/server/handler.go
tests:
  - internal/server/handler_querylog_test.go
  - internal/config/zones_test.go
  - cmd/shadowdns/querylog_test.go
  - internal/querylog/querylog_test.go
  - internal/config/logging_test.go
-->

---
### Requirement: Log emission point matches BIND9 semantics

The dns-server SHALL emit the query log line as soon as the client's view is resolved and before zone matching, so that queries subsequently refused (e.g., qname outside all loaded zones) are still logged. Queries rejected before view resolution — no matching view, CHAOS class, malformed question count (FORMERR), unsupported opcode (NOTIMP) — SHALL NOT produce a query log line. AXFR/IXFR requests SHALL produce a query log line after the transfer path's internal view resolution succeeds; because the allow-transfer ACL check precedes view resolution in the transfer path, ACL-refused requests SHALL NOT produce a query log line, and the existing ACL-before-view ordering SHALL NOT be changed for logging purposes.

#### Scenario: Refused query inside a view is logged

- **WHEN** a client in view `view-eu` queries a name outside all loaded zones and receives REFUSED
- **THEN** the query log still contains one line for that query

#### Scenario: No-view client is not logged

- **WHEN** a client whose IP matches no view sends a query and receives REFUSED
- **THEN** no line is appended to the query log file

#### Scenario: AXFR request is logged with qtype AXFR

- **WHEN** a client in view `view-eu` sends an AXFR request for `example.com` over TCP
- **THEN** the query log contains a line whose query section reads `query: example.com IN AXFR` with `T` present in the flags field

#### Scenario: ACL-refused transfer request is not logged

- **WHEN** a client outside the allow-transfer ACL sends an AXFR request and receives REFUSED
- **THEN** no line is appended to the query log file


<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
-->


<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
code:
  - packaging/named.conf.example
  - internal/config/zones.go
  - NOTES.md
  - cmd/shadowdns/main.go
  - internal/config/logging.go
  - README.md
  - internal/querylog/querylog.go
  - internal/server/server.go
  - internal/server/handler.go
tests:
  - internal/server/handler_querylog_test.go
  - internal/config/zones_test.go
  - cmd/shadowdns/querylog_test.go
  - internal/querylog/querylog_test.go
  - internal/config/logging_test.go
-->

---
### Requirement: Query log file participates in SIGUSR1 reopen

In daemon mode the server SHALL register the SIGUSR1 handler unconditionally — registration SHALL NOT depend on a query log or a file-backed main log existing at startup, because a SIGHUP reload can introduce a query log at any time. On receipt of SIGUSR1 the daemon SHALL assemble the reopen list dynamically at signal time: the main log sink (fixed at startup when `--log-file` is non-empty) plus the currently active query-log sink read from the shared holder (skipped when nil). A reopen failure of either file SHALL keep that file's previous descriptor active, SHALL emit one error-level record through the main logger, and SHALL NOT affect the other file's reopen.

#### Scenario: SIGUSR1 reopens query log after rename

- **WHEN** an external process renames the query log file and sends SIGUSR1
- **THEN** subsequent query log lines are written to a freshly created file at the configured path

#### Scenario: Handler registration does not depend on startup sinks

- **WHEN** the daemon starts with neither `--log-file` nor a `logging { }` query log configured
- **THEN** the SIGUSR1 handler SHALL still be registered, so a query log introduced by a later SIGHUP reload is reopenable without a process restart


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
### Requirement: Startup fails loudly when the query log file cannot be opened

When query logging is enabled and the configured file cannot be opened with `O_APPEND|O_CREATE` (mode 0640), the daemon SHALL abort startup with an error naming the path and the underlying failure instead of silently disabling query logging.

#### Scenario: Unwritable directory aborts startup

- **WHEN** the parsed query log path points into a non-existent directory
- **THEN** the daemon exits with a startup error naming that path


<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
-->


<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
code:
  - packaging/named.conf.example
  - internal/config/zones.go
  - NOTES.md
  - cmd/shadowdns/main.go
  - internal/config/logging.go
  - README.md
  - internal/querylog/querylog.go
  - internal/server/server.go
  - internal/server/handler.go
tests:
  - internal/server/handler_querylog_test.go
  - internal/config/zones_test.go
  - cmd/shadowdns/querylog_test.go
  - internal/querylog/querylog_test.go
  - internal/config/logging_test.go
-->

---
### Requirement: Warn at startup when BIND rotation parameters are ignored

When the parsed queries channel `file` clause carries `versions` or `size` parameters, the daemon SHALL emit one warning through the main logger at startup (including under `--dry-run`) stating that ShadowDNS does not implement BIND built-in rotation and that logrotate plus SIGUSR1 must be used instead. When neither parameter is present, no such warning SHALL be emitted.

#### Scenario: versions and size trigger the warning

- **WHEN** the channel declares `file "/var/log/shadowdns/queries.log" versions 3 size 5000m;`
- **THEN** startup output contains exactly one warning about ignored rotation parameters


<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
-->


<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
code:
  - packaging/named.conf.example
  - internal/config/zones.go
  - NOTES.md
  - cmd/shadowdns/main.go
  - internal/config/logging.go
  - README.md
  - internal/querylog/querylog.go
  - internal/server/server.go
  - internal/server/handler.go
tests:
  - internal/server/handler_querylog_test.go
  - internal/config/zones_test.go
  - cmd/shadowdns/querylog_test.go
  - internal/querylog/querylog_test.go
  - internal/config/logging_test.go
-->

---
### Requirement: Dry-run summary reports query log status

The `--dry-run` summary SHALL report whether query logging is enabled; when enabled it SHALL include the resolved file path and the effective print option values, and when disabled it SHALL include the reason (no `logging{}` block, no `queries` category, null or built-in channel, non-file channel, or severity stricter than info).

#### Scenario: Dry-run with query logging enabled

- **WHEN** `--dry-run` runs against a named.conf whose `logging{}` block wires `category queries` to a file channel
- **THEN** the summary reports query logging as enabled with the resolved file path


<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
-->


<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
code:
  - packaging/named.conf.example
  - internal/config/zones.go
  - NOTES.md
  - cmd/shadowdns/main.go
  - internal/config/logging.go
  - README.md
  - internal/querylog/querylog.go
  - internal/server/server.go
  - internal/server/handler.go
tests:
  - internal/server/handler_querylog_test.go
  - internal/config/zones_test.go
  - cmd/shadowdns/querylog_test.go
  - internal/querylog/querylog_test.go
  - internal/config/logging_test.go
-->

---
### Requirement: Query log configuration is re-applied on SIGHUP

On SIGHUP reload the server SHALL compare the query log configuration in the reloaded named.conf with the currently active query log configuration and apply the minimal required changes according to the following rules:

- If the `FilePath`, all print options (`PrintTime`, `PrintCategory`, `PrintSeverity`), and the rotation-parameters marker (`RotationIgnored`) are identical, the existing `*querylog.Logger` and its underlying file handle SHALL be kept unchanged (no open or close operations are performed).
- If any of `FilePath`, `PrintTime`, `PrintCategory`, `PrintSeverity`, or `RotationIgnored` differ, the server SHALL open a new sink at the newly configured path (which is permitted to be identical to the previous path), construct a replacement `*querylog.Logger` with the new options, atomically replace the active logger reference, and close the old sink. A change consisting solely of adding or removing BIND rotation parameters (`versions`/`size`) flips `RotationIgnored` and SHALL therefore be treated as a configuration change, not as unchanged.
- If the reloaded config has no `logging { }` block (query log absent) and a logger was previously active, the server SHALL close the old sink and set the active logger reference to nil.
- If the reloaded config introduces a query log path for the first time (no logger was previously active), the server SHALL open the new sink and set the active logger reference.

The comparison SHALL cover every field of the query-log configuration value — as of this change those are exactly the five fields above — so that a configuration field added in the future cannot be silently excluded from change detection (whole-value equality is the recommended implementation).

A failure to open the new query-log sink SHALL cause `reload()` to return an error, leaving the previously active logger unchanged. The SIGUSR1 "reopen" behaviour for logrotate SHALL remain unaffected and orthogonal to SIGHUP reconfigure behaviour.

#### Scenario: Query log path change takes effect on reload

- **WHEN** the operator changes the `file` path in the `logging { channel }` block and sends SIGHUP
- **THEN** subsequent query log lines SHALL be written to the new file path
- **THEN** the old file handle SHALL be closed after the swap

#### Scenario: Unchanged query log config causes no file operation

- **WHEN** the operator sends SIGHUP and the query log FilePath, all print options, and the rotation-parameters marker are identical to the currently active configuration
- **THEN** the existing `*querylog.Logger` and its file descriptor SHALL remain unchanged
- **THEN** no open or close call SHALL be made on the query log sink

#### Scenario: Query log removed in reload

- **WHEN** the operator removes the `logging { }` block from named.conf and sends SIGHUP
- **THEN** the active logger reference SHALL be nil after the reload
- **THEN** the previously active file handle SHALL be closed
- **THEN** subsequent queries SHALL not produce any query log output

#### Scenario: Failed sink open preserves existing logger

- **WHEN** the reloaded named.conf specifies a new query log path in a non-existent directory and SIGHUP is received
- **THEN** `reload()` SHALL return an error
- **THEN** the previously active `*querylog.Logger` SHALL remain in use
- **THEN** `shadowdns_reload_total{result="failure"}` SHALL increment

#### Scenario: SIGUSR1 reopen is unaffected by SIGHUP reconfigure

- **WHEN** a SIGUSR1 is received after a SIGHUP that changed the query log path
- **THEN** the SIGUSR1 SHALL reopen the currently active sink (at the new path set by SIGHUP)
- **THEN** the logrotate rename-and-recreate workflow SHALL function correctly on the new path

#### Scenario: Query log introduced by reload is reopenable via SIGUSR1

- **WHEN** the server started without a `logging { }` block, a later SIGHUP reload introduces a query log, and a SIGUSR1 is subsequently received
- **THEN** the SIGUSR1 handler SHALL reopen the query-log sink created by the reload (the reopen capability SHALL NOT depend on a query log having existed at startup)

#### Scenario: Rotation-parameters warning is re-emitted when reload applies a changed config

- **WHEN** a SIGHUP reload applies a changed `logging { }` configuration whose `file` clause carries BIND rotation parameters (`versions` or `size`) — including a change consisting solely of adding those parameters to an otherwise identical configuration
- **THEN** the server SHALL emit the same rotation-ignored warning as the startup path (rotation parameters are ignored; use an external rotation tool with SIGUSR1)
- **THEN** the warning SHALL NOT be emitted when the configuration is unchanged (the reuse path performs no file operations and no re-warning)

#### Scenario: Reopen of a sink closed by reload returns an error instead of resurrecting it

- **WHEN** a SIGUSR1 handler holds a reference to a query-log sink that a concurrent SIGHUP reload has just closed, and calls `Reopen` on it
- **THEN** `Reopen` SHALL return `os.ErrClosed` without opening any file descriptor (close is terminal; a closed sink is never resurrected)
- **THEN** the currently active sink installed by the reload SHALL be unaffected


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
### Requirement: Disabled query logging leaves DNS behavior unchanged

When the parsed configuration yields no query log (any disable condition), the dns-server SHALL NOT create any file, SHALL NOT emit any query log line, and SHALL serve DNS responses identically to a build without this capability.

#### Scenario: Absent logging block changes nothing

- **WHEN** named.conf contains no `logging{}` block
- **THEN** no query log file is created and all DNS responses are byte-identical to prior behavior


<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
-->

<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
code:
  - packaging/named.conf.example
  - internal/config/zones.go
  - NOTES.md
  - cmd/shadowdns/main.go
  - internal/config/logging.go
  - README.md
  - internal/querylog/querylog.go
  - internal/server/server.go
  - internal/server/handler.go
tests:
  - internal/server/handler_querylog_test.go
  - internal/config/zones_test.go
  - cmd/shadowdns/querylog_test.go
  - internal/querylog/querylog_test.go
  - internal/config/logging_test.go
-->