## ADDED Requirements

### Requirement: Logger implementation uses zap

The ShadowDNS binary SHALL use `go.uber.org/zap` as the sole logging implementation in production code. The standard library `log/slog` package MUST NOT be imported from any file under `cmd/` or `internal/`, except transitively through third-party dependencies beyond project control.

#### Scenario: Production code contains no slog import

- **WHEN** the project is built with `make build`
- **THEN** no file under `cmd/` or `internal/` imports `log/slog`

#### Scenario: Logger factory returns a zap logger

- **WHEN** the binary starts and constructs its root logger
- **THEN** the returned logger is of type `*zap.Logger` and is passed to all subsystems

### Requirement: CLI flag -no-color forces uncolored output

The ShadowDNS binary SHALL accept a boolean command-line flag `-no-color` that, when set to `true`, forces all log output to be emitted without ANSI color escape sequences regardless of terminal detection or environment variables.

#### Scenario: Flag overrides a TTY environment

- **WHEN** the binary is started with `-no-color` in an interactive terminal
- **THEN** the stderr output contains no ANSI escape sequences

#### Scenario: Flag default is false

- **WHEN** the binary is started without `-no-color`
- **THEN** color output is enabled subject to the other decision layers (NO_COLOR env var and isatty detection)

### Requirement: NO_COLOR environment variable disables color

The ShadowDNS binary SHALL honor the `NO_COLOR` environment variable as specified by https://no-color.org: when `NO_COLOR` is set to any non-empty value, color output MUST be disabled even if stderr is a TTY. The `-no-color` flag takes precedence over this environment variable (i.e., explicit flag=false cannot re-enable color when NO_COLOR is set — the three layers are all disabling conditions).

#### Scenario: NO_COLOR set to non-empty value disables color

- **WHEN** the binary is started with `NO_COLOR=1` in an interactive terminal
- **THEN** the stderr output contains no ANSI escape sequences

#### Scenario: NO_COLOR set to empty string does not disable color

- **WHEN** the binary is started with `NO_COLOR=` (empty) in an interactive terminal
- **THEN** color output is enabled subject to isatty detection

### Requirement: Automatic TTY detection disables color in non-interactive environments

The ShadowDNS binary SHALL detect whether stderr is connected to a terminal using `isatty` at logger construction time. When stderr is not a TTY (systemd journald, pipe, file redirection), color output MUST be disabled automatically without requiring `-no-color` or `NO_COLOR` to be set.

#### Scenario: Output redirected to file has no ANSI codes

- **WHEN** the binary is invoked as `shadowdns ... 2> /tmp/log.txt`
- **THEN** the file `/tmp/log.txt` contains no ANSI escape sequences

#### Scenario: Running under systemd has no ANSI codes in journal

- **WHEN** the binary is started by a systemd unit with `Type=simple`
- **THEN** `journalctl -u shadowdns` output contains no ANSI escape sequences

#### Scenario: Output piped to another process has no ANSI codes

- **WHEN** the binary is invoked as `shadowdns ... 2>&1 | grep ERROR`
- **THEN** the piped input to `grep` contains no ANSI escape sequences

### Requirement: Color is applied only to the level field

When color output is enabled, the ShadowDNS binary SHALL colorize only the level field (`INFO`, `WARN`, `ERROR`, `DEBUG`) using zap's `zapcore.CapitalColorLevelEncoder`. The timestamp, message, and key-value fields MUST remain uncolored.

#### Scenario: Colored level in TTY output

- **WHEN** the binary emits an INFO log in an interactive terminal with color enabled
- **THEN** the level token `INFO` is wrapped in ANSI color escape sequences
- **AND** the timestamp, message text, and any structured fields contain no ANSI escape sequences

### Requirement: Decision precedence

The logger factory SHALL evaluate the color-enablement decision using the following precedence, from highest to lowest; any layer that indicates "disabled" produces a final decision of "disabled":

1. `-no-color` flag set to true → disabled
2. `NO_COLOR` environment variable is a non-empty string → disabled
3. `isatty(stderr)` returns false → disabled

Otherwise, color is enabled.

#### Scenario: Flag takes precedence over TTY

- **WHEN** the binary is started with `-no-color` in a TTY environment with `NO_COLOR` unset
- **THEN** color is disabled

#### Scenario: Env var takes precedence over TTY

- **WHEN** the binary is started without `-no-color` in a TTY environment with `NO_COLOR=1`
- **THEN** color is disabled

#### Scenario: All layers permit color

- **WHEN** the binary is started without `-no-color`, with `NO_COLOR` unset, in a TTY environment
- **THEN** color is enabled

### Requirement: Decision is fixed at logger construction

The color-enablement decision SHALL be evaluated exactly once, at the time the root logger is constructed. Subsequent changes to the environment (e.g., `NO_COLOR` being set after startup) MUST NOT affect the running process's log output format.

#### Scenario: Mid-run env var change has no effect

- **WHEN** the binary is started in a TTY with color enabled, and `NO_COLOR=1` is exported into the process environment mid-run via another mechanism
- **THEN** subsequent log lines continue to emit ANSI color escape sequences
