## ADDED Requirements

### Requirement: Parse named.conf logging block for query logging

The config-loader SHALL parse the top-level `logging { ... };` block (replacing the previous silent skip) and extract: each `channel <name> { ... }` declaration's `file` path with optional `versions` and `size` parameters, `severity`, `print-time`, `print-category`, and `print-severity`; and the `category queries { ... };` channel list. The result SHALL be exposed on the loaded configuration as an optional query-log record carrying the resolved file path (relative paths joined to the `options { directory }` value, consistent with zone file path resolution), the three print option values, and a flag indicating whether `versions` or `size` were present. Channel parameters and categories other than those listed SHALL be ignored with a warning log entry rather than causing a parse failure. A syntactically malformed `logging{}` block (e.g., unbalanced braces) SHALL produce a fatal error naming the file and line, consistent with `options{}` parsing.

#### Scenario: Production-shaped logging block parses successfully

- **WHEN** named.conf contains `logging { channel queries_log { file "/var/log/shadowdns/queries.log" versions 3 size 5000m; severity debug; print-severity yes; print-time yes; print-category yes; }; category queries { queries_log; }; };`
- **THEN** the loader produces a query-log record with file path `/var/log/shadowdns/queries.log`, all three print options enabled, and the rotation-parameters-present flag set

#### Scenario: Relative file path resolves against options directory

- **WHEN** the channel declares `file "queries.log";` and `options { directory "/etc/namedb"; };` is present
- **THEN** the query-log record's file path is `/etc/namedb/queries.log`

#### Scenario: Multiple channels in category queries

- **WHEN** `category queries { chan_a; chan_b; };` lists two file channels
- **THEN** the loader uses the first file channel and emits a warning that the remaining channels are ignored

### Requirement: Disable query logging for unsupported logging configurations

The config-loader SHALL yield no query-log record (query logging disabled) without failing startup when any of the following holds: the `logging{}` block is absent; no `category queries` is declared; the channel referenced by `category queries` is `null` or another built-in channel (`default_syslog`, `default_stderr`, `default_debug`); the referenced channel is a user-defined non-file channel (e.g., `syslog` or `stderr` destination); or the channel's `severity` is stricter than `info` (`notice`, `warning`, `error`, or `critical`). For the user-defined-non-file-channel and severity-stricter-than-info cases the loader SHALL emit a warning naming the reason; the remaining cases — including built-in channels, which deliberately target non-file destinations and are not configuration errors — SHALL disable silently. Severity values `info`, `debug` (with optional level), and `dynamic` SHALL enable query logging.

#### Scenario: Disable conditions yield no query-log record

- **WHEN** named.conf matches any disable condition
- **THEN** the loaded configuration carries no query-log record and startup proceeds

##### Example: disable condition matrix

| logging block content | Result | Warning emitted |
| --------------------- | ------ | --------------- |
| (no `logging{}` block at all) | disabled | no |
| `logging { category queries { null; }; };` | disabled | no |
| channel `queries_log` declared but no `category queries` | disabled | no |
| `category queries { default_syslog; };` | disabled | no |
| file channel with `severity warning;` | disabled | yes |
| `channel q { syslog daemon; }; category queries { q; };` | disabled | yes |
| file channel with `severity debug 3;` | enabled | no |
| file channel with `severity dynamic;` | enabled | no |
