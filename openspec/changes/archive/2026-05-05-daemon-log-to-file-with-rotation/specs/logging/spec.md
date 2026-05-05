## ADDED Requirements

### Requirement: Daemon SHALL support file-backed log output

The `shadowdns` daemon SHALL accept a `--log-file <path>` CLI flag. When the flag value is non-empty, the daemon SHALL open the file with `O_APPEND|O_CREATE` and mode `0640`, and route all zap logger output to that file instead of `os.Stderr`. When the flag is empty (the default), the daemon SHALL continue routing zap logger output to `os.Stderr` so that existing development workflows and unit tests are unaffected.

The daemon SHALL NOT tee output to both stderr and the file simultaneously: exactly one sink is active per process lifetime decision, determined at startup from the flag value.

If the file cannot be opened (path does not exist, permission denied, etc.), the daemon SHALL fail to start with a non-zero exit code and SHALL print the error to `os.Stderr` so the failure is visible regardless of sink configuration.

#### Scenario: Empty --log-file uses stderr

- **WHEN** the daemon is started without `--log-file` (or with `--log-file ""`)
- **THEN** all log records are written to `os.Stderr` and no file is opened or created

#### Scenario: Non-empty --log-file routes output to that file

- **GIVEN** `--log-file /var/log/shadowdns/shadowdns.log` and the directory exists with appropriate permissions
- **WHEN** the daemon emits any log record
- **THEN** the record is appended to `/var/log/shadowdns/shadowdns.log` and is NOT written to `os.Stderr`

#### Scenario: Unopenable log file fails startup loudly

- **GIVEN** `--log-file /nonexistent/dir/shadowdns.log` where the parent directory does not exist
- **WHEN** the daemon starts
- **THEN** the process exits with a non-zero status and the underlying `os.OpenFile` error is printed to `os.Stderr`

### Requirement: Daemon SHALL reopen log file on SIGUSR1

When `--log-file` is non-empty, the daemon SHALL register a signal handler for `SIGUSR1`. On receipt of `SIGUSR1` the daemon SHALL close the current log file descriptor and reopen the same path with `O_APPEND|O_CREATE` (mode `0640`), atomically replacing the file handle used by the logger sink.

If the reopen call fails (the file path was removed, parent directory is missing, etc.), the daemon SHALL keep the previous file descriptor active so subsequent log writes are not lost, and SHALL emit a single error-level log record describing the reopen failure through the still-active sink.

The reopen handler SHALL be idempotent and safe to invoke concurrently with ongoing log writes; concurrent log writers SHALL observe the swap atomically (one record goes to the old fd or to the new fd, never split).

`SIGHUP` SHALL NOT trigger log reopen; `SIGHUP` semantics remain governed by the existing `sighup-reload` capability.

#### Scenario: SIGUSR1 reopens after rename

- **GIVEN** daemon running with `--log-file /var/log/shadowdns/shadowdns.log` and the file currently exists
- **WHEN** an external process renames `shadowdns.log` to `shadowdns.log.1` and then sends `SIGUSR1` to the daemon
- **THEN** subsequent log records appear in a newly created `shadowdns.log` (a different inode from `shadowdns.log.1`), and `shadowdns.log.1` is no longer being written to

#### Scenario: SIGUSR1 reopen failure preserves previous fd

- **GIVEN** daemon running with `--log-file /var/log/shadowdns/shadowdns.log` and the parent directory `/var/log/shadowdns` has been deleted between rotations
- **WHEN** the daemon receives `SIGUSR1`
- **THEN** the daemon does NOT crash, the previous file descriptor remains open and continues to receive log writes, and a single error-level record describing the failed reopen is appended

#### Scenario: SIGHUP does not affect log file

- **GIVEN** daemon running with `--log-file /var/log/shadowdns/shadowdns.log`
- **WHEN** the daemon receives `SIGHUP`
- **THEN** zone reload behavior runs as defined in the `sighup-reload` capability and the log file descriptor is NOT reopened or closed
