## ADDED Requirements

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

### Requirement: Query log file participates in SIGUSR1 reopen

When query logging is enabled, the daemon SHALL register the SIGUSR1 handler (even when `--log-file` is not set) and on receipt of SIGUSR1 SHALL reopen the query log file in addition to the main log file. A reopen failure of either file SHALL keep that file's previous descriptor active, SHALL emit one error-level record through the main logger, and SHALL NOT affect the other file's reopen. This requirement extends — and does not replace — the `logging` capability's existing SIGUSR1 requirement (which mandates handler registration when `--log-file` is non-empty): the handler SHALL be registered when either condition holds.

#### Scenario: SIGUSR1 reopens query log after rename

- **WHEN** an external process renames the query log file and sends SIGUSR1
- **THEN** subsequent query log lines are written to a freshly created file at the configured path

### Requirement: Startup fails loudly when the query log file cannot be opened

When query logging is enabled and the configured file cannot be opened with `O_APPEND|O_CREATE` (mode 0640), the daemon SHALL abort startup with an error naming the path and the underlying failure instead of silently disabling query logging.

#### Scenario: Unwritable directory aborts startup

- **WHEN** the parsed query log path points into a non-existent directory
- **THEN** the daemon exits with a startup error naming that path

### Requirement: Warn at startup when BIND rotation parameters are ignored

When the parsed queries channel `file` clause carries `versions` or `size` parameters, the daemon SHALL emit one warning through the main logger at startup (including under `--dry-run`) stating that ShadowDNS does not implement BIND built-in rotation and that logrotate plus SIGUSR1 must be used instead. When neither parameter is present, no such warning SHALL be emitted.

#### Scenario: versions and size trigger the warning

- **WHEN** the channel declares `file "/var/log/shadowdns/queries.log" versions 3 size 5000m;`
- **THEN** startup output contains exactly one warning about ignored rotation parameters

### Requirement: Dry-run summary reports query log status

The `--dry-run` summary SHALL report whether query logging is enabled; when enabled it SHALL include the resolved file path and the effective print option values, and when disabled it SHALL include the reason (no `logging{}` block, no `queries` category, null or built-in channel, non-file channel, or severity stricter than info).

#### Scenario: Dry-run with query logging enabled

- **WHEN** `--dry-run` runs against a named.conf whose `logging{}` block wires `category queries` to a file channel
- **THEN** the summary reports query logging as enabled with the resolved file path

### Requirement: SIGHUP reload does not re-apply logging configuration

On SIGHUP reload the daemon SHALL keep the current query log sink unchanged: the open file descriptor SHALL remain untouched, in-flight writes SHALL complete normally, and any `logging{}` content parsed from the reloaded named.conf SHALL be discarded without being applied. A syntactically malformed `logging{}` block in the reloaded named.conf SHALL cause that reload to fail and retain the previous server state, consistent with existing reload semantics for `options{}` parse errors.

#### Scenario: Changed logging block is ignored on reload

- **WHEN** the operator edits the `logging{}` block to point at a different file path and sends SIGHUP
- **THEN** query log lines continue to be written to the originally configured file until the daemon is restarted

#### Scenario: Malformed logging block fails the reload

- **WHEN** the reloaded named.conf contains a `logging{}` block with unbalanced braces
- **THEN** the reload fails, the previous zone state and query log sink remain active

### Requirement: Disabled query logging leaves DNS behavior unchanged

When the parsed configuration yields no query log (any disable condition), the dns-server SHALL NOT create any file, SHALL NOT emit any query log line, and SHALL serve DNS responses identically to a build without this capability.

#### Scenario: Absent logging block changes nothing

- **WHEN** named.conf contains no `logging{}` block
- **THEN** no query log file is created and all DNS responses are byte-identical to prior behavior
