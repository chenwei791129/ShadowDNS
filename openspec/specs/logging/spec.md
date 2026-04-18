## ADDED Requirements

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
  - testdata/integration/master/backup.example_overrides
  - README.md
  - internal/transfer/axfr.go
  - go.sum
  - testdata/integration/master/backup.example_view-other.fwd
  - internal/view/geoip_asn.go
  - testdata/integration/master.zones
  - testdata/integration/master/example.com_view-other.fwd
  - scripts/test-deb.sh
  - testdata/integration/master/cnames/example.com_cname
  - internal/config/aliases.go
  - internal/view/loader.go
  - go.mod
  - internal/alias/override.go
  - CLAUDE.md
  - internal/zone/parser.go
  - testdata/integration/master/example.com_include.fwd
  - internal/server/server.go
  - internal/server/build.go
  - internal/server/listenaddr.go
  - testdata/integration/master/example.com_view-th.fwd
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
  - testdata/integration/master/backup.example_overrides
  - README.md
  - internal/transfer/axfr.go
  - go.sum
  - testdata/integration/master/backup.example_view-other.fwd
  - internal/view/geoip_asn.go
  - testdata/integration/master.zones
  - testdata/integration/master/example.com_view-other.fwd
  - scripts/test-deb.sh
  - testdata/integration/master/cnames/example.com_cname
  - internal/config/aliases.go
  - internal/view/loader.go
  - go.mod
  - internal/alias/override.go
  - CLAUDE.md
  - internal/zone/parser.go
  - testdata/integration/master/example.com_include.fwd
  - internal/server/server.go
  - internal/server/build.go
  - internal/server/listenaddr.go
  - testdata/integration/master/example.com_view-th.fwd
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
  - testdata/integration/master/backup.example_overrides
  - README.md
  - internal/transfer/axfr.go
  - go.sum
  - testdata/integration/master/backup.example_view-other.fwd
  - internal/view/geoip_asn.go
  - testdata/integration/master.zones
  - testdata/integration/master/example.com_view-other.fwd
  - scripts/test-deb.sh
  - testdata/integration/master/cnames/example.com_cname
  - internal/config/aliases.go
  - internal/view/loader.go
  - go.mod
  - internal/alias/override.go
  - CLAUDE.md
  - internal/zone/parser.go
  - testdata/integration/master/example.com_include.fwd
  - internal/server/server.go
  - internal/server/build.go
  - internal/server/listenaddr.go
  - testdata/integration/master/example.com_view-th.fwd
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
  - testdata/integration/master/backup.example_overrides
  - README.md
  - internal/transfer/axfr.go
  - go.sum
  - testdata/integration/master/backup.example_view-other.fwd
  - internal/view/geoip_asn.go
  - testdata/integration/master.zones
  - testdata/integration/master/example.com_view-other.fwd
  - scripts/test-deb.sh
  - testdata/integration/master/cnames/example.com_cname
  - internal/config/aliases.go
  - internal/view/loader.go
  - go.mod
  - internal/alias/override.go
  - CLAUDE.md
  - internal/zone/parser.go
  - testdata/integration/master/example.com_include.fwd
  - internal/server/server.go
  - internal/server/build.go
  - internal/server/listenaddr.go
  - testdata/integration/master/example.com_view-th.fwd
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
  - testdata/integration/master/backup.example_overrides
  - README.md
  - internal/transfer/axfr.go
  - go.sum
  - testdata/integration/master/backup.example_view-other.fwd
  - internal/view/geoip_asn.go
  - testdata/integration/master.zones
  - testdata/integration/master/example.com_view-other.fwd
  - scripts/test-deb.sh
  - testdata/integration/master/cnames/example.com_cname
  - internal/config/aliases.go
  - internal/view/loader.go
  - go.mod
  - internal/alias/override.go
  - CLAUDE.md
  - internal/zone/parser.go
  - testdata/integration/master/example.com_include.fwd
  - internal/server/server.go
  - internal/server/build.go
  - internal/server/listenaddr.go
  - testdata/integration/master/example.com_view-th.fwd
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
  - testdata/integration/master/backup.example_overrides
  - README.md
  - internal/transfer/axfr.go
  - go.sum
  - testdata/integration/master/backup.example_view-other.fwd
  - internal/view/geoip_asn.go
  - testdata/integration/master.zones
  - testdata/integration/master/example.com_view-other.fwd
  - scripts/test-deb.sh
  - testdata/integration/master/cnames/example.com_cname
  - internal/config/aliases.go
  - internal/view/loader.go
  - go.mod
  - internal/alias/override.go
  - CLAUDE.md
  - internal/zone/parser.go
  - testdata/integration/master/example.com_include.fwd
  - internal/server/server.go
  - internal/server/build.go
  - internal/server/listenaddr.go
  - testdata/integration/master/example.com_view-th.fwd
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

### Requirement: Decision is fixed at logger construction

The color-enablement decision SHALL be evaluated exactly once, at the time the root logger is constructed. Subsequent changes to the environment (e.g., `NO_COLOR` being set after startup) MUST NOT affect the running process's log output format.

#### Scenario: Mid-run env var change has no effect

- **WHEN** the binary is started in a TTY with color enabled, and `NO_COLOR=1` is exported into the process environment mid-run via another mechanism
- **THEN** subsequent log lines continue to emit ANSI color escape sequences

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
  - testdata/integration/master/backup.example_overrides
  - README.md
  - internal/transfer/axfr.go
  - go.sum
  - testdata/integration/master/backup.example_view-other.fwd
  - internal/view/geoip_asn.go
  - testdata/integration/master.zones
  - testdata/integration/master/example.com_view-other.fwd
  - scripts/test-deb.sh
  - testdata/integration/master/cnames/example.com_cname
  - internal/config/aliases.go
  - internal/view/loader.go
  - go.mod
  - internal/alias/override.go
  - CLAUDE.md
  - internal/zone/parser.go
  - testdata/integration/master/example.com_include.fwd
  - internal/server/server.go
  - internal/server/build.go
  - internal/server/listenaddr.go
  - testdata/integration/master/example.com_view-th.fwd
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

---
### Requirement: CLI flag -no-color forces uncolored output

The ShadowDNS binary SHALL accept a boolean command-line flag `-no-color` that, when set to `true`, forces all log output to be emitted without ANSI color escape sequences regardless of terminal detection or environment variables.

#### Scenario: Flag overrides a TTY environment

- **WHEN** the binary is started with `-no-color` in an interactive terminal
- **THEN** the stderr output contains no ANSI escape sequences

#### Scenario: Flag default is false

- **WHEN** the binary is started without `-no-color`
- **THEN** color output is enabled subject to the other decision layers (NO_COLOR env var and isatty detection)

---
### Requirement: NO_COLOR environment variable disables color

The ShadowDNS binary SHALL honor the `NO_COLOR` environment variable as specified by https://no-color.org: when `NO_COLOR` is set to any non-empty value, color output MUST be disabled even if stderr is a TTY. The `-no-color` flag takes precedence over this environment variable (i.e., explicit flag=false cannot re-enable color when NO_COLOR is set — the three layers are all disabling conditions).

#### Scenario: NO_COLOR set to non-empty value disables color

- **WHEN** the binary is started with `NO_COLOR=1` in an interactive terminal
- **THEN** the stderr output contains no ANSI escape sequences

#### Scenario: NO_COLOR set to empty string does not disable color

- **WHEN** the binary is started with `NO_COLOR=` (empty) in an interactive terminal
- **THEN** color output is enabled subject to isatty detection

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

---
### Requirement: Color is applied only to the level field

When color output is enabled, the ShadowDNS binary SHALL colorize only the level field (`INFO`, `WARN`, `ERROR`, `DEBUG`) using zap's `zapcore.CapitalColorLevelEncoder`. The timestamp, message, and key-value fields MUST remain uncolored.

#### Scenario: Colored level in TTY output

- **WHEN** the binary emits an INFO log in an interactive terminal with color enabled
- **THEN** the level token `INFO` is wrapped in ANSI color escape sequences
- **AND** the timestamp, message text, and any structured fields contain no ANSI escape sequences

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

---
### Requirement: Decision is fixed at logger construction

The color-enablement decision SHALL be evaluated exactly once, at the time the root logger is constructed. Subsequent changes to the environment (e.g., `NO_COLOR` being set after startup) MUST NOT affect the running process's log output format.

#### Scenario: Mid-run env var change has no effect

- **WHEN** the binary is started in a TTY with color enabled, and `NO_COLOR=1` is exported into the process environment mid-run via another mechanism
- **THEN** subsequent log lines continue to emit ANSI color escape sequences