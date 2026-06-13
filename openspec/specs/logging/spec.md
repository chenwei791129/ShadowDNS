## Requirements
<!-- @trace
source: migrate-logging-to-zap
updated: 2026-04-18
code:
  - internal/server/fingerprint.go
  - internal/zone/classify.go
  - internal/logging/logger.go
  - internal/view/geoip_country.go
  - docs/migration.md
  - internal/server/handler.go
  - internal/transfer/notify.go
  - scripts/smoke.sh
  - internal/server/listener.go
  - packaging/named.conf.example
  - internal/zone/zone.go
  - internal/config/zones.go
  - internal/config/options.go
  - docs/benchmark.md
  - Makefile
  - cmd/shadowdns/main.go
  - testdata/integration/db.backup.example.overrides
  - README.md
  - internal/transfer/axfr.go
  - go.sum
  - testdata/integration/db.backup.example-other
  - internal/view/geoip_asn.go
  - testdata/integration/named.conf.local
  - testdata/integration/db.example.com-other
  - scripts/test-deb.sh
  - testdata/integration/cnames/db.example.com.cname
  - internal/config/aliases.go
  - internal/view/loader.go
  - go.mod
  - internal/alias/override.go
  - CLAUDE.md
  - internal/zone/parser.go
  - testdata/integration/db.include-test.example
  - internal/server/server.go
  - internal/server/build.go
  - internal/server/listenaddr.go
  - testdata/integration/db.example.com-th
  - packaging/shadowdns.service
  - .release-please-manifest.json
  - CHANGELOG.md
tests:
  - test/integration/helpers_test.go
  - internal/server/addrset_test.go
  - internal/server/server_test.go
  - internal/server/fingerprint_test.go
  - test/integration/cname_following_test.go
  - test/integration/axfr_test.go
  - test/integration/query_test.go
  - internal/transfer/axfr_test.go
  - cmd/shadowdns/listenon_test.go
  - internal/zone/classify_test.go
  - test/integration/listenon_test.go
  - internal/config/zones_test.go
  - internal/alias/override_test.go
  - internal/logging/logger_test.go
  - cmd/shadowdns/main_test.go
  - internal/zone/zone_test.go
  - internal/transfer/notify_test.go
  - test/integration/negative_test.go
  - internal/server/build_test.go
  - test/integration/notify_test.go
  - internal/view/loader_test.go
  - test/integration/cname_synthesis_test.go
  - internal/config/options_test.go
  - test/integration/reload_diff_test.go
  - test/integration/wildcard_test.go
  - internal/server/bindmany_test.go
  - test/integration/backup_test.go
  - internal/zone/parser_test.go
  - internal/server/listenaddr_test.go
-->

### Requirement: Logger implementation uses zap

The ShadowDNS binary SHALL use `go.uber.org/zap` as the sole logging implementation in production code. The standard library `log/slog` package MUST NOT be imported from any file under `cmd/` or `internal/`, except transitively through third-party dependencies beyond project control.

#### Scenario: Production code contains no slog import

- **WHEN** the project is built with `make build`
- **THEN** no file under `cmd/` or `internal/` imports `log/slog`

#### Scenario: Logger factory returns a zap logger

- **WHEN** the binary starts and constructs its root logger
- **THEN** the returned logger is of type `*zap.Logger` and is passed to all subsystems

<!-- @trace
source: migrate-logging-to-zap
updated: 2026-04-18
code:
  - internal/server/fingerprint.go
  - internal/zone/classify.go
  - internal/logging/logger.go
  - internal/view/geoip_country.go
  - docs/migration.md
  - internal/server/handler.go
  - internal/transfer/notify.go
  - scripts/smoke.sh
  - internal/server/listener.go
  - packaging/named.conf.example
  - internal/zone/zone.go
  - internal/config/zones.go
  - internal/config/options.go
  - docs/benchmark.md
  - Makefile
  - cmd/shadowdns/main.go
  - testdata/integration/db.backup.example.overrides
  - README.md
  - internal/transfer/axfr.go
  - go.sum
  - testdata/integration/db.backup.example-other
  - internal/view/geoip_asn.go
  - testdata/integration/named.conf.local
  - testdata/integration/db.example.com-other
  - scripts/test-deb.sh
  - testdata/integration/cnames/db.example.com.cname
  - internal/config/aliases.go
  - internal/view/loader.go
  - go.mod
  - internal/alias/override.go
  - CLAUDE.md
  - internal/zone/parser.go
  - testdata/integration/db.include-test.example
  - internal/server/server.go
  - internal/server/build.go
  - internal/server/listenaddr.go
  - testdata/integration/db.example.com-th
  - packaging/shadowdns.service
  - .release-please-manifest.json
  - CHANGELOG.md
tests:
  - test/integration/helpers_test.go
  - internal/server/addrset_test.go
  - internal/server/server_test.go
  - internal/server/fingerprint_test.go
  - test/integration/cname_following_test.go
  - test/integration/axfr_test.go
  - test/integration/query_test.go
  - internal/transfer/axfr_test.go
  - cmd/shadowdns/listenon_test.go
  - internal/zone/classify_test.go
  - test/integration/listenon_test.go
  - internal/config/zones_test.go
  - internal/alias/override_test.go
  - internal/logging/logger_test.go
  - cmd/shadowdns/main_test.go
  - internal/zone/zone_test.go
  - internal/transfer/notify_test.go
  - test/integration/negative_test.go
  - internal/server/build_test.go
  - test/integration/notify_test.go
  - internal/view/loader_test.go
  - test/integration/cname_synthesis_test.go
  - internal/config/options_test.go
  - test/integration/reload_diff_test.go
  - test/integration/wildcard_test.go
  - internal/server/bindmany_test.go
  - test/integration/backup_test.go
  - internal/zone/parser_test.go
  - internal/server/listenaddr_test.go
-->

---
### Requirement: CLI flag -no-color forces uncolored output

The ShadowDNS binary SHALL accept a boolean command-line flag `-no-color` that, when set to `true`, forces all log output to be emitted without ANSI color escape sequences regardless of terminal detection or environment variables.

#### Scenario: Flag overrides a TTY environment

- **WHEN** the binary is started with `-no-color` in an interactive terminal
- **THEN** the stderr output contains no ANSI escape sequences

#### Scenario: Flag default is false

- **WHEN** the binary is started without `-no-color`
- **THEN** color output is enabled subject to the other decision layers (NO_COLOR env var and isatty detection)

<!-- @trace
source: migrate-logging-to-zap
updated: 2026-04-18
code:
  - internal/server/fingerprint.go
  - internal/zone/classify.go
  - internal/logging/logger.go
  - internal/view/geoip_country.go
  - docs/migration.md
  - internal/server/handler.go
  - internal/transfer/notify.go
  - scripts/smoke.sh
  - internal/server/listener.go
  - packaging/named.conf.example
  - internal/zone/zone.go
  - internal/config/zones.go
  - internal/config/options.go
  - docs/benchmark.md
  - Makefile
  - cmd/shadowdns/main.go
  - testdata/integration/db.backup.example.overrides
  - README.md
  - internal/transfer/axfr.go
  - go.sum
  - testdata/integration/db.backup.example-other
  - internal/view/geoip_asn.go
  - testdata/integration/named.conf.local
  - testdata/integration/db.example.com-other
  - scripts/test-deb.sh
  - testdata/integration/cnames/db.example.com.cname
  - internal/config/aliases.go
  - internal/view/loader.go
  - go.mod
  - internal/alias/override.go
  - CLAUDE.md
  - internal/zone/parser.go
  - testdata/integration/db.include-test.example
  - internal/server/server.go
  - internal/server/build.go
  - internal/server/listenaddr.go
  - testdata/integration/db.example.com-th
  - packaging/shadowdns.service
  - .release-please-manifest.json
  - CHANGELOG.md
tests:
  - test/integration/helpers_test.go
  - internal/server/addrset_test.go
  - internal/server/server_test.go
  - internal/server/fingerprint_test.go
  - test/integration/cname_following_test.go
  - test/integration/axfr_test.go
  - test/integration/query_test.go
  - internal/transfer/axfr_test.go
  - cmd/shadowdns/listenon_test.go
  - internal/zone/classify_test.go
  - test/integration/listenon_test.go
  - internal/config/zones_test.go
  - internal/alias/override_test.go
  - internal/logging/logger_test.go
  - cmd/shadowdns/main_test.go
  - internal/zone/zone_test.go
  - internal/transfer/notify_test.go
  - test/integration/negative_test.go
  - internal/server/build_test.go
  - test/integration/notify_test.go
  - internal/view/loader_test.go
  - test/integration/cname_synthesis_test.go
  - internal/config/options_test.go
  - test/integration/reload_diff_test.go
  - test/integration/wildcard_test.go
  - internal/server/bindmany_test.go
  - test/integration/backup_test.go
  - internal/zone/parser_test.go
  - internal/server/listenaddr_test.go
-->

---
### Requirement: NO_COLOR environment variable disables color

The ShadowDNS binary SHALL honor the `NO_COLOR` environment variable as specified by https://no-color.org: when `NO_COLOR` is set to any non-empty value, color output MUST be disabled even if stderr is a TTY. The `-no-color` flag takes precedence over this environment variable (i.e., explicit flag=false cannot re-enable color when NO_COLOR is set — the three layers are all disabling conditions).

#### Scenario: NO_COLOR set to non-empty value disables color

- **WHEN** the binary is started with `NO_COLOR=1` in an interactive terminal
- **THEN** the stderr output contains no ANSI escape sequences

#### Scenario: NO_COLOR set to empty string does not disable color

- **WHEN** the binary is started with `NO_COLOR=` (empty) in an interactive terminal
- **THEN** color output is enabled subject to isatty detection

<!-- @trace
source: migrate-logging-to-zap
updated: 2026-04-18
code:
  - internal/server/fingerprint.go
  - internal/zone/classify.go
  - internal/logging/logger.go
  - internal/view/geoip_country.go
  - docs/migration.md
  - internal/server/handler.go
  - internal/transfer/notify.go
  - scripts/smoke.sh
  - internal/server/listener.go
  - packaging/named.conf.example
  - internal/zone/zone.go
  - internal/config/zones.go
  - internal/config/options.go
  - docs/benchmark.md
  - Makefile
  - cmd/shadowdns/main.go
  - testdata/integration/db.backup.example.overrides
  - README.md
  - internal/transfer/axfr.go
  - go.sum
  - testdata/integration/db.backup.example-other
  - internal/view/geoip_asn.go
  - testdata/integration/named.conf.local
  - testdata/integration/db.example.com-other
  - scripts/test-deb.sh
  - testdata/integration/cnames/db.example.com.cname
  - internal/config/aliases.go
  - internal/view/loader.go
  - go.mod
  - internal/alias/override.go
  - CLAUDE.md
  - internal/zone/parser.go
  - testdata/integration/db.include-test.example
  - internal/server/server.go
  - internal/server/build.go
  - internal/server/listenaddr.go
  - testdata/integration/db.example.com-th
  - packaging/shadowdns.service
  - .release-please-manifest.json
  - CHANGELOG.md
tests:
  - test/integration/helpers_test.go
  - internal/server/addrset_test.go
  - internal/server/server_test.go
  - internal/server/fingerprint_test.go
  - test/integration/cname_following_test.go
  - test/integration/axfr_test.go
  - test/integration/query_test.go
  - internal/transfer/axfr_test.go
  - cmd/shadowdns/listenon_test.go
  - internal/zone/classify_test.go
  - test/integration/listenon_test.go
  - internal/config/zones_test.go
  - internal/alias/override_test.go
  - internal/logging/logger_test.go
  - cmd/shadowdns/main_test.go
  - internal/zone/zone_test.go
  - internal/transfer/notify_test.go
  - test/integration/negative_test.go
  - internal/server/build_test.go
  - test/integration/notify_test.go
  - internal/view/loader_test.go
  - test/integration/cname_synthesis_test.go
  - internal/config/options_test.go
  - test/integration/reload_diff_test.go
  - test/integration/wildcard_test.go
  - internal/server/bindmany_test.go
  - test/integration/backup_test.go
  - internal/zone/parser_test.go
  - internal/server/listenaddr_test.go
-->

---
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

<!-- @trace
source: migrate-logging-to-zap
updated: 2026-04-18
code:
  - internal/server/fingerprint.go
  - internal/zone/classify.go
  - internal/logging/logger.go
  - internal/view/geoip_country.go
  - docs/migration.md
  - internal/server/handler.go
  - internal/transfer/notify.go
  - scripts/smoke.sh
  - internal/server/listener.go
  - packaging/named.conf.example
  - internal/zone/zone.go
  - internal/config/zones.go
  - internal/config/options.go
  - docs/benchmark.md
  - Makefile
  - cmd/shadowdns/main.go
  - testdata/integration/db.backup.example.overrides
  - README.md
  - internal/transfer/axfr.go
  - go.sum
  - testdata/integration/db.backup.example-other
  - internal/view/geoip_asn.go
  - testdata/integration/named.conf.local
  - testdata/integration/db.example.com-other
  - scripts/test-deb.sh
  - testdata/integration/cnames/db.example.com.cname
  - internal/config/aliases.go
  - internal/view/loader.go
  - go.mod
  - internal/alias/override.go
  - CLAUDE.md
  - internal/zone/parser.go
  - testdata/integration/db.include-test.example
  - internal/server/server.go
  - internal/server/build.go
  - internal/server/listenaddr.go
  - testdata/integration/db.example.com-th
  - packaging/shadowdns.service
  - .release-please-manifest.json
  - CHANGELOG.md
tests:
  - test/integration/helpers_test.go
  - internal/server/addrset_test.go
  - internal/server/server_test.go
  - internal/server/fingerprint_test.go
  - test/integration/cname_following_test.go
  - test/integration/axfr_test.go
  - test/integration/query_test.go
  - internal/transfer/axfr_test.go
  - cmd/shadowdns/listenon_test.go
  - internal/zone/classify_test.go
  - test/integration/listenon_test.go
  - internal/config/zones_test.go
  - internal/alias/override_test.go
  - internal/logging/logger_test.go
  - cmd/shadowdns/main_test.go
  - internal/zone/zone_test.go
  - internal/transfer/notify_test.go
  - test/integration/negative_test.go
  - internal/server/build_test.go
  - test/integration/notify_test.go
  - internal/view/loader_test.go
  - test/integration/cname_synthesis_test.go
  - internal/config/options_test.go
  - test/integration/reload_diff_test.go
  - test/integration/wildcard_test.go
  - internal/server/bindmany_test.go
  - test/integration/backup_test.go
  - internal/zone/parser_test.go
  - internal/server/listenaddr_test.go
-->

---
### Requirement: Color is applied only to the level field

When color output is enabled, the ShadowDNS binary SHALL colorize only the level field (`INFO`, `WARN`, `ERROR`, `DEBUG`) using zap's `zapcore.CapitalColorLevelEncoder`. The timestamp, message, and key-value fields MUST remain uncolored.

#### Scenario: Colored level in TTY output

- **WHEN** the binary emits an INFO log in an interactive terminal with color enabled
- **THEN** the level token `INFO` is wrapped in ANSI color escape sequences
- **AND** the timestamp, message text, and any structured fields contain no ANSI escape sequences

<!-- @trace
source: migrate-logging-to-zap
updated: 2026-04-18
code:
  - internal/server/fingerprint.go
  - internal/zone/classify.go
  - internal/logging/logger.go
  - internal/view/geoip_country.go
  - docs/migration.md
  - internal/server/handler.go
  - internal/transfer/notify.go
  - scripts/smoke.sh
  - internal/server/listener.go
  - packaging/named.conf.example
  - internal/zone/zone.go
  - internal/config/zones.go
  - internal/config/options.go
  - docs/benchmark.md
  - Makefile
  - cmd/shadowdns/main.go
  - testdata/integration/db.backup.example.overrides
  - README.md
  - internal/transfer/axfr.go
  - go.sum
  - testdata/integration/db.backup.example-other
  - internal/view/geoip_asn.go
  - testdata/integration/named.conf.local
  - testdata/integration/db.example.com-other
  - scripts/test-deb.sh
  - testdata/integration/cnames/db.example.com.cname
  - internal/config/aliases.go
  - internal/view/loader.go
  - go.mod
  - internal/alias/override.go
  - CLAUDE.md
  - internal/zone/parser.go
  - testdata/integration/db.include-test.example
  - internal/server/server.go
  - internal/server/build.go
  - internal/server/listenaddr.go
  - testdata/integration/db.example.com-th
  - packaging/shadowdns.service
  - .release-please-manifest.json
  - CHANGELOG.md
tests:
  - test/integration/helpers_test.go
  - internal/server/addrset_test.go
  - internal/server/server_test.go
  - internal/server/fingerprint_test.go
  - test/integration/cname_following_test.go
  - test/integration/axfr_test.go
  - test/integration/query_test.go
  - internal/transfer/axfr_test.go
  - cmd/shadowdns/listenon_test.go
  - internal/zone/classify_test.go
  - test/integration/listenon_test.go
  - internal/config/zones_test.go
  - internal/alias/override_test.go
  - internal/logging/logger_test.go
  - cmd/shadowdns/main_test.go
  - internal/zone/zone_test.go
  - internal/transfer/notify_test.go
  - test/integration/negative_test.go
  - internal/server/build_test.go
  - test/integration/notify_test.go
  - internal/view/loader_test.go
  - test/integration/cname_synthesis_test.go
  - internal/config/options_test.go
  - test/integration/reload_diff_test.go
  - test/integration/wildcard_test.go
  - internal/server/bindmany_test.go
  - test/integration/backup_test.go
  - internal/zone/parser_test.go
  - internal/server/listenaddr_test.go
-->

---
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

<!-- @trace
source: migrate-logging-to-zap
updated: 2026-04-18
code:
  - internal/server/fingerprint.go
  - internal/zone/classify.go
  - internal/logging/logger.go
  - internal/view/geoip_country.go
  - docs/migration.md
  - internal/server/handler.go
  - internal/transfer/notify.go
  - scripts/smoke.sh
  - internal/server/listener.go
  - packaging/named.conf.example
  - internal/zone/zone.go
  - internal/config/zones.go
  - internal/config/options.go
  - docs/benchmark.md
  - Makefile
  - cmd/shadowdns/main.go
  - testdata/integration/db.backup.example.overrides
  - README.md
  - internal/transfer/axfr.go
  - go.sum
  - testdata/integration/db.backup.example-other
  - internal/view/geoip_asn.go
  - testdata/integration/named.conf.local
  - testdata/integration/db.example.com-other
  - scripts/test-deb.sh
  - testdata/integration/cnames/db.example.com.cname
  - internal/config/aliases.go
  - internal/view/loader.go
  - go.mod
  - internal/alias/override.go
  - CLAUDE.md
  - internal/zone/parser.go
  - testdata/integration/db.include-test.example
  - internal/server/server.go
  - internal/server/build.go
  - internal/server/listenaddr.go
  - testdata/integration/db.example.com-th
  - packaging/shadowdns.service
  - .release-please-manifest.json
  - CHANGELOG.md
tests:
  - test/integration/helpers_test.go
  - internal/server/addrset_test.go
  - internal/server/server_test.go
  - internal/server/fingerprint_test.go
  - test/integration/cname_following_test.go
  - test/integration/axfr_test.go
  - test/integration/query_test.go
  - internal/transfer/axfr_test.go
  - cmd/shadowdns/listenon_test.go
  - internal/zone/classify_test.go
  - test/integration/listenon_test.go
  - internal/config/zones_test.go
  - internal/alias/override_test.go
  - internal/logging/logger_test.go
  - cmd/shadowdns/main_test.go
  - internal/zone/zone_test.go
  - internal/transfer/notify_test.go
  - test/integration/negative_test.go
  - internal/server/build_test.go
  - test/integration/notify_test.go
  - internal/view/loader_test.go
  - test/integration/cname_synthesis_test.go
  - internal/config/options_test.go
  - test/integration/reload_diff_test.go
  - test/integration/wildcard_test.go
  - internal/server/bindmany_test.go
  - test/integration/backup_test.go
  - internal/zone/parser_test.go
  - internal/server/listenaddr_test.go
-->

---
### Requirement: Decision is fixed at logger construction

The color-enablement decision SHALL be evaluated exactly once, at the time the root logger is constructed. Subsequent changes to the environment (e.g., `NO_COLOR` being set after startup) MUST NOT affect the running process's log output format.

#### Scenario: Mid-run env var change has no effect

- **WHEN** the binary is started in a TTY with color enabled, and `NO_COLOR=1` is exported into the process environment mid-run via another mechanism
- **THEN** subsequent log lines continue to emit ANSI color escape sequences

---
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

<!-- @trace
source: daemon-log-to-file-with-rotation
updated: 2026-05-05
code:
  - packaging/shadowdns.service
  - cmd/shadowdns/prune_backup.go
  - cmd/shadowdns/main.go
  - internal/logging/logger.go
  - scripts/test-deb.sh
  - internal/logging/reopen.go
  - nfpm.yaml
  - packaging/logrotate.shadowdns
tests:
  - internal/logging/reopen_test.go
  - cmd/shadowdns/main_test.go
  - internal/logging/logger_test.go
-->

---
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

<!-- @trace
source: daemon-log-to-file-with-rotation
updated: 2026-05-05
code:
  - packaging/shadowdns.service
  - cmd/shadowdns/prune_backup.go
  - cmd/shadowdns/main.go
  - internal/logging/logger.go
  - scripts/test-deb.sh
  - internal/logging/reopen.go
  - nfpm.yaml
  - packaging/logrotate.shadowdns
tests:
  - internal/logging/reopen_test.go
  - cmd/shadowdns/main_test.go
  - internal/logging/logger_test.go
-->
