## ADDED Requirements

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

## MODIFIED Requirements

### Requirement: Query log file participates in SIGUSR1 reopen

In daemon mode the server SHALL register the SIGUSR1 handler unconditionally — registration SHALL NOT depend on a query log or a file-backed main log existing at startup, because a SIGHUP reload can introduce a query log at any time. On receipt of SIGUSR1 the daemon SHALL assemble the reopen list dynamically at signal time: the main log sink (fixed at startup when `--log-file` is non-empty) plus the currently active query-log sink read from the shared holder (skipped when nil). A reopen failure of either file SHALL keep that file's previous descriptor active, SHALL emit one error-level record through the main logger, and SHALL NOT affect the other file's reopen.

#### Scenario: SIGUSR1 reopens query log after rename

- **WHEN** an external process renames the query log file and sends SIGUSR1
- **THEN** subsequent query log lines are written to a freshly created file at the configured path

#### Scenario: Handler registration does not depend on startup sinks

- **WHEN** the daemon starts with neither `--log-file` nor a `logging { }` query log configured
- **THEN** the SIGUSR1 handler SHALL still be registered, so a query log introduced by a later SIGHUP reload is reopenable without a process restart

## REMOVED Requirements

### Requirement: SIGHUP reload does not re-apply logging configuration

**Reason**: This constraint is lifted by the reload-coverage-and-metrics change. The `logging {}` block (query log path and print options) is now fully re-evaluated on every SIGHUP, replacing the previous behaviour of discarding any `logging {}` changes after startup.

**Migration**: Operators who relied on the previous behavior (logging config fixed at startup) will find that a SIGHUP now activates `logging {}` changes from the reloaded named.conf. To retain static logging config, keep the `logging {}` block identical between reloads. SIGUSR1 reopen semantics for logrotate are unchanged.

#### Scenario: SIGHUP now re-applies logging configuration instead of discarding it

- **WHEN** the operator changes any field in the `logging { }` block of named.conf and sends SIGHUP
- **THEN** the server SHALL apply the updated query log configuration (this requirement is REMOVED — the old behavior of discarding logging config changes after startup SHALL NOT apply)
- **THEN** operators SHALL NOT need to restart the process to activate changes to query log path or print options
