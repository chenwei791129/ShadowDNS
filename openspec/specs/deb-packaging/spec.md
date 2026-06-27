# deb-packaging Specification

## Purpose

TBD - created by archiving change 'deb-packaging'. Update Purpose after archive.

## Requirements

### Requirement: nfpm configuration file

The project SHALL include an `nfpm.yaml` configuration file at the project root that defines the `.deb` package metadata, contents, and scripts for nfpm to produce a Debian package.

#### Scenario: nfpm configuration is valid

- **WHEN** `nfpm package --packager deb` is executed in the project root
- **THEN** nfpm SHALL produce a `.deb` file without errors

#### Scenario: Package metadata is complete

- **WHEN** the `.deb` file is inspected with `dpkg-deb --info`
- **THEN** the output SHALL include package name `shadowdns`, architecture `amd64`, a maintainer field, and a description field


<!-- @trace
source: deb-packaging
updated: 2026-04-14
code:
  - packaging/named.conf.example
  - Makefile
  - cmd/shadowdns/main.go
  - packaging/postinstall.sh
  - nfpm.yaml
  - scripts/gen-container-testdata.go
  - go.sum
  - scripts/test-deb.sh
  - go.mod
  - packaging/aliases.yaml.example
  - internal/config/options.go
  - packaging/shadowdns.service
  - internal/server/listener.go
tests:
  - cmd/shadowdns/main_test.go
  - internal/config/options_test.go
-->

---
### Requirement: Binary installation path

The `.deb` package SHALL install the `shadowdns` binary to `/usr/bin/shadowdns` with executable permissions (0755).

#### Scenario: Binary is available on PATH after install

- **WHEN** the `.deb` package is installed via `dpkg -i`
- **THEN** running `which shadowdns` SHALL return `/usr/bin/shadowdns`

#### Scenario: Binary is executable

- **WHEN** the `.deb` package is installed via `dpkg -i`
- **THEN** running `shadowdns --help` SHALL produce usage output (exit code 0) without `Permission denied` errors


<!-- @trace
source: deb-packaging
updated: 2026-04-14
code:
  - packaging/named.conf.example
  - Makefile
  - cmd/shadowdns/main.go
  - packaging/postinstall.sh
  - nfpm.yaml
  - scripts/gen-container-testdata.go
  - go.sum
  - scripts/test-deb.sh
  - go.mod
  - packaging/aliases.yaml.example
  - internal/config/options.go
  - packaging/shadowdns.service
  - internal/server/listener.go
tests:
  - cmd/shadowdns/main_test.go
  - internal/config/options_test.go
-->

---
### Requirement: systemd service unit

The `.deb` package SHALL install a systemd service unit file at `/lib/systemd/system/shadowdns.service` that manages the ShadowDNS daemon lifecycle.

#### Scenario: Service can be started

- **WHEN** the package is installed and valid configuration files exist
- **THEN** `systemctl start shadowdns` SHALL start the ShadowDNS process

#### Scenario: Service supports reload via SIGHUP

- **WHEN** `systemctl reload shadowdns` is executed
- **THEN** the service manager SHALL send SIGHUP to the ShadowDNS process, triggering a configuration reload without restart

#### Scenario: Service runs with least privilege

- **WHEN** the service is running
- **THEN** the process SHALL run as a non-root dynamic user with `CAP_NET_BIND_SERVICE` capability to bind port 53


<!-- @trace
source: deb-packaging
updated: 2026-04-14
code:
  - packaging/named.conf.example
  - Makefile
  - cmd/shadowdns/main.go
  - packaging/postinstall.sh
  - nfpm.yaml
  - scripts/gen-container-testdata.go
  - go.sum
  - scripts/test-deb.sh
  - go.mod
  - packaging/aliases.yaml.example
  - internal/config/options.go
  - packaging/shadowdns.service
  - internal/server/listener.go
tests:
  - cmd/shadowdns/main_test.go
  - internal/config/options_test.go
-->

---
### Requirement: Example configuration files

The `.deb` package SHALL install example configuration files under `/etc/shadowdns/` to assist first-time setup.

#### Scenario: Example named.conf is installed

- **WHEN** the package is installed
- **THEN** `/etc/shadowdns/named.conf.example` SHALL exist and contain a valid `named.conf` skeleton consisting of `include "named.conf.options";` and `include "named.conf.local";` directives (Debian/Ubuntu include split)

#### Scenario: Example named.conf.options is installed

- **WHEN** the package is installed
- **THEN** `/etc/shadowdns/named.conf.options.example` SHALL exist and contain the `options` block including a `directory` and `geoip-directory` setting

#### Scenario: Example named.conf.local is installed

- **WHEN** the package is installed
- **THEN** `/etc/shadowdns/named.conf.local.example` SHALL exist and contain at least one `view` block with `match-clients` and a `zone` declaration

#### Scenario: Example shadowdns.yaml is installed

- **WHEN** the package is installed
- **THEN** `/etc/shadowdns/shadowdns.yaml.example` SHALL exist and contain a valid unified config skeleton including the `aliases:` section (one-to-many `root: [backups]` format) and the `ephemeral_api:` section


<!-- @trace
source: debian-named-conf-layout
updated: 2026-06-13
code:
  - nfpm.yaml
  - packaging/named.conf.local.example
  - testdata/integration/cnames/db.example.com.cname
  - scripts/test-deb.sh
  - internal/config/zones.go
  - testdata/integration/db.example.com-other
  - README.md
  - docs/getting-started.zh.md
  - docs/configuration/named-conf.zh.md
  - testdata/integration/db.include-test.example
  - docs/migration.md
  - testdata/integration/db.example.com-th
  - docs/configuration/named-conf.md
  - testdata/integration/named.conf
  - testdata/integration/named.conf.local
  - packaging/named.conf.options.example
  - docs/getting-started.md
  - docs/migration.zh.md
  - testdata/integration/named.conf.options
  - scripts/gen-container-testdata.go
  - scripts/smoke.sh
  - testdata/integration/db.backup.example-other
  - testdata/integration/db.backup.example-th
  - testdata/integration/db.backup.example.overrides
  - packaging/named.conf.example
  - testdata/integration/README.md
tests:
  - test/integration/helpers_test.go
  - internal/config/zones_test.go
  - test/integration/listenon_test.go
  - test/integration/query_test.go
  - test/integration/prune_backup_test.go
  - internal/prunebackup/lexer_test.go
-->

---
### Requirement: Makefile deb target

The project Makefile SHALL include a `deb` target that builds the binary and produces a `.deb` package in a single command.

#### Scenario: make deb produces a deb file

- **WHEN** `make deb` is executed
- **THEN** a `.deb` file SHALL be created in the project root directory

#### Scenario: make deb builds the binary first

- **WHEN** `make deb` is executed without a pre-existing binary
- **THEN** the binary SHALL be compiled before packaging begins (the `deb` target depends on `build`)


<!-- @trace
source: deb-packaging
updated: 2026-04-14
code:
  - packaging/named.conf.example
  - Makefile
  - cmd/shadowdns/main.go
  - packaging/postinstall.sh
  - nfpm.yaml
  - scripts/gen-container-testdata.go
  - go.sum
  - scripts/test-deb.sh
  - go.mod
  - packaging/aliases.yaml.example
  - internal/config/options.go
  - packaging/shadowdns.service
  - internal/server/listener.go
tests:
  - cmd/shadowdns/main_test.go
  - internal/config/options_test.go
-->

---
### Requirement: Container integration test

The project SHALL include a `make test-deb` target that performs end-to-end validation of the `.deb` package inside an Ubuntu container using podman.

#### Scenario: make test-deb validates installation

- **WHEN** `make test-deb` is executed on a machine with podman installed
- **THEN** the target SHALL cross-compile the binary for `linux/amd64`, build a `.deb`, start an `ubuntu:24.04` container, install the package via `dpkg -i`, and verify that all expected files, the `shadowdns` system user, and the log directory exist

#### Scenario: make test-deb validates binary execution

- **WHEN** `make test-deb` is executed
- **THEN** the target SHALL run `shadowdns -dry-run` inside the container and confirm it exits with code 0

#### Scenario: make test-deb validates DNS query

- **WHEN** `make test-deb` is executed
- **THEN** the target SHALL start the ShadowDNS server inside the container with integration test fixtures, send a DNS query for `example.com A`, and verify a non-empty response is returned

#### Scenario: make test-deb cleans up

- **WHEN** `make test-deb` completes (success or failure)
- **THEN** the podman container SHALL be removed automatically


<!-- @trace
source: deb-packaging
updated: 2026-04-14
code:
  - packaging/named.conf.example
  - Makefile
  - cmd/shadowdns/main.go
  - packaging/postinstall.sh
  - nfpm.yaml
  - scripts/gen-container-testdata.go
  - go.sum
  - scripts/test-deb.sh
  - go.mod
  - packaging/aliases.yaml.example
  - internal/config/options.go
  - packaging/shadowdns.service
  - internal/server/listener.go
tests:
  - cmd/shadowdns/main_test.go
  - internal/config/options_test.go
-->

---
### Requirement: Container testdata generator

The project SHALL include `scripts/gen-container-testdata.go` that prepares a ready-to-use ShadowDNS configuration directory with mock GeoIP mmdb files for container testing.

#### Scenario: Generator produces complete testdata

- **WHEN** `go run scripts/gen-container-testdata.go -out <dir> -target <container-path>` is executed
- **THEN** the output directory SHALL contain `named.conf`, `named.conf.options`, `named.conf.local`, `aliases.yaml`, `db.<zone>` / `db.<zone>-<view>` zone files plus any nested `$INCLUDE` fragments under `cnames/` (Debian/Ubuntu naming, no `master/` subdirectory and no `master.zones` file), and `geoip/GeoLite2-Country.mmdb` and `geoip/GeoLite2-ASN.mmdb` with all path placeholders replaced by the target path


<!-- @trace
source: debian-named-conf-layout
updated: 2026-06-13
code:
  - nfpm.yaml
  - packaging/named.conf.local.example
  - testdata/integration/cnames/db.example.com.cname
  - scripts/test-deb.sh
  - internal/config/zones.go
  - testdata/integration/db.example.com-other
  - README.md
  - docs/getting-started.zh.md
  - docs/configuration/named-conf.zh.md
  - testdata/integration/db.include-test.example
  - docs/migration.md
  - testdata/integration/db.example.com-th
  - docs/configuration/named-conf.md
  - testdata/integration/named.conf
  - testdata/integration/named.conf.local
  - packaging/named.conf.options.example
  - docs/getting-started.md
  - docs/migration.zh.md
  - testdata/integration/named.conf.options
  - scripts/gen-container-testdata.go
  - scripts/smoke.sh
  - testdata/integration/db.backup.example-other
  - testdata/integration/db.backup.example-th
  - testdata/integration/db.backup.example.overrides
  - packaging/named.conf.example
  - testdata/integration/README.md
tests:
  - test/integration/helpers_test.go
  - internal/config/zones_test.go
  - test/integration/listenon_test.go
  - test/integration/query_test.go
  - test/integration/prune_backup_test.go
  - internal/prunebackup/lexer_test.go
-->

---
### Requirement: Shell completion files

The `.deb` package SHALL install shell completion files for bash, zsh, and fish at the standard vendor paths for each shell, so that tab-completion for the `shadowdns` command is available to users who have installed the corresponding shell.

The install paths SHALL be:

| Shell | Path                                                    |
| ----- | ------------------------------------------------------- |
| bash  | `/usr/share/bash-completion/completions/shadowdns`      |
| zsh   | `/usr/share/zsh/vendor-completions/_shadowdns`          |
| fish  | `/usr/share/fish/vendor_completions.d/shadowdns.fish`   |

The completion files SHALL be owned by the `shadowdns` Debian package (tracked by dpkg) so that `apt remove shadowdns` removes them and `apt upgrade` replaces them atomically with the binary.

#### Scenario: Bash completion file is installed

- **WHEN** the `.deb` package is installed via `dpkg -i`
- **THEN** the file `/usr/share/bash-completion/completions/shadowdns` SHALL exist and be non-empty

#### Scenario: Zsh completion file is installed

- **WHEN** the `.deb` package is installed via `dpkg -i`
- **THEN** the file `/usr/share/zsh/vendor-completions/_shadowdns` SHALL exist and be non-empty

#### Scenario: Fish completion file is installed

- **WHEN** the `.deb` package is installed via `dpkg -i`
- **THEN** the file `/usr/share/fish/vendor_completions.d/shadowdns.fish` SHALL exist and be non-empty

#### Scenario: Completion files are owned by dpkg

- **WHEN** `dpkg -L shadowdns` is executed after install
- **THEN** the output SHALL list all three completion file paths

#### Scenario: Completion files are removed on package removal

- **WHEN** the package is removed via `apt remove shadowdns` or `dpkg -r shadowdns`
- **THEN** all three completion file paths SHALL NOT exist on the filesystem


<!-- @trace
source: add-shell-completion
updated: 2026-04-23
-->


<!-- @trace
source: add-shell-completion
updated: 2026-04-23
code:
  - Makefile
  - nfpm.yaml
  - scripts/test-deb.sh
  - CLAUDE.md
-->

---
### Requirement: Makefile deb target generates shell completion files

The Makefile `deb` target SHALL generate the bash, zsh, and fish completion files into the `bin/` directory before invoking nfpm, using the Cobra `completion` subcommand of the `shadowdns` CLI (invoked via `go run ./cmd/shadowdns`) so that the generated content tracks the current command tree without requiring a pre-built host binary.

#### Scenario: make deb produces completion files alongside the binary

- **WHEN** `make deb` is executed from a clean `bin/` directory
- **THEN** `bin/` SHALL contain three generated completion files (one for each of bash, zsh, fish) before nfpm is invoked

#### Scenario: Completion generation works on a macOS host

- **WHEN** `make deb` is executed on a macOS host (where the target `linux/amd64` binary cannot be executed)
- **THEN** the completion files SHALL still be generated successfully via `go run ./cmd/shadowdns completion <shell>`


<!-- @trace
source: add-shell-completion
updated: 2026-04-23
-->


<!-- @trace
source: add-shell-completion
updated: 2026-04-23
code:
  - Makefile
  - nfpm.yaml
  - scripts/test-deb.sh
  - CLAUDE.md
-->

---
### Requirement: Container integration test validates shell completion

The `make test-deb` target SHALL verify that the installed `.deb` package places all three shell completion files at their expected paths and that each is non-empty and owned by dpkg.

#### Scenario: test-deb asserts all completion files exist

- **WHEN** `make test-deb` is executed
- **THEN** the test SHALL fail if any of the three completion file paths is missing, empty, or not listed by `dpkg -L shadowdns`


<!-- @trace
source: add-shell-completion
updated: 2026-04-23
-->


<!-- @trace
source: add-shell-completion
updated: 2026-04-23
code:
  - Makefile
  - nfpm.yaml
  - scripts/test-deb.sh
  - CLAUDE.md
-->

---
### Requirement: Build artifacts excluded from version control

The `.gitignore` file SHALL exclude `.deb` package files from version control.

#### Scenario: deb files are ignored by git

- **WHEN** a `.deb` file exists in the project root
- **THEN** `git status` SHALL NOT list the `.deb` file as untracked

<!-- @trace
source: deb-packaging
updated: 2026-04-14
code:
  - packaging/named.conf.example
  - Makefile
  - cmd/shadowdns/main.go
  - packaging/postinstall.sh
  - nfpm.yaml
  - scripts/gen-container-testdata.go
  - go.sum
  - scripts/test-deb.sh
  - go.mod
  - packaging/aliases.yaml.example
  - internal/config/options.go
  - packaging/shadowdns.service
  - internal/server/listener.go
tests:
  - cmd/shadowdns/main_test.go
  - internal/config/options_test.go
-->

---
### Requirement: deb package SHALL install a logrotate configuration

The `shadowdns_<version>_amd64.deb` package SHALL install a logrotate configuration file at `/etc/logrotate.d/shadowdns` (owned by `root:root`, mode `0644`). The configuration file SHALL declare daily rotation of `/var/log/shadowdns/*.log`, retain 14 rotated copies, compress rotated files with `delaycompress`, tolerate missing files (`missingok`), skip rotation for empty files (`notifempty`), and recreate the active log file with mode `0640` owned by `shadowdns:shadowdns` after rotation.

The configuration SHALL include a `postrotate` script that sends `SIGUSR1` to the running daemon so the in-process file descriptor reopens onto the freshly created file. Because the daemon's pid-file path is configured in `named.conf` (operator-controlled, e.g. `/var/run/named/pid`) and is therefore not predictable from packaging, the script SHALL resolve the running PID via `systemctl show --property MainPID --value shadowdns.service` so only the systemd-managed instance is signalled. The script SHALL guard the signal-send so an inactive unit (`MainPID=0`), an environment without systemd available (the `systemctl` invocation fails), or a missing target process does not produce an error exit.

The logrotate configuration MUST be declared in `nfpm.yaml` so it is included in every produced `.deb` artifact (verifiable via `dpkg -L shadowdns | grep logrotate.d`).

#### Scenario: Installed package contains the logrotate config

- **WHEN** `shadowdns_<version>_amd64.deb` is installed via `dpkg -i`
- **THEN** `/etc/logrotate.d/shadowdns` exists, is owned by `root:root` with mode `0644`, and contains a `/var/log/shadowdns/*.log { ... }` block declaring `daily`, `rotate 14`, `compress`, `delaycompress`, `missingok`, `notifempty`, `create 0640 shadowdns shadowdns`, and a `postrotate` script that resolves the daemon PID via `systemctl show --property MainPID --value shadowdns.service` and sends `SIGUSR1` to that PID

#### Scenario: postrotate tolerates absent daemon

- **GIVEN** no `shadowdns` process is running on the host (or systemd is not available, e.g. inside a non-init container)
- **WHEN** `logrotate` runs against `/etc/logrotate.d/shadowdns`
- **THEN** the rotation completes with exit code 0 and no error is emitted from the `postrotate` block (the `systemctl show` invocation either returns `MainPID=0` for an inactive unit or exits non-zero in environments without systemd; both branches are absorbed by the `|| true` and the `[ "$pid" != "0" ]` guard around the `kill`)


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
### Requirement: systemd unit SHALL pass --log-file flag by default

The `packaging/shadowdns.service` unit's `ExecStart=` line SHALL include `--log-file /var/log/shadowdns/shadowdns.log` so that a fresh deb installation produces a daemon that writes to the rotated log file path without requiring operator changes to `override.conf`.

The unit's existing directives that grant write access to `/var/log/shadowdns` (`ReadWritePaths=/var/log/shadowdns`) and the `postinstall.sh` step that creates the directory with `shadowdns:shadowdns` ownership SHALL remain in place.

Operators MAY override the flag through a drop-in at `/etc/systemd/system/shadowdns.service.d/override.conf` to disable file logging (e.g., revert to stderr/journal) without modifying the packaged unit file.

#### Scenario: Default ExecStart writes to file

- **GIVEN** a freshly installed `shadowdns` deb with no override drop-in
- **WHEN** the daemon is started via `systemctl start shadowdns`
- **THEN** the running process command line (visible via `ps -p $MAINPID -o args=`) contains `--log-file /var/log/shadowdns/shadowdns.log` and log records appear in that file

#### Scenario: Operator can override via drop-in

- **GIVEN** an operator-supplied `/etc/systemd/system/shadowdns.service.d/override.conf` that resets `ExecStart=` and sets a new `ExecStart=` line without `--log-file`
- **WHEN** the daemon is restarted
- **THEN** the daemon runs without the `--log-file` flag and routes log output to `os.Stderr` (and therefore systemd-journal), demonstrating the package default is overridable

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
### Requirement: Viewless BIND-style example configuration file

The `.deb` package SHALL install a viewless BIND-style example configuration file at `/etc/shadowdns/named.conf.viewless.example` to help operators who want a Debian-style authoritative setup without GeoIP views. The file SHALL be a self-contained, valid viewless `named.conf` skeleton consisting of an `options` block and one or more top-level `zone "<domain>" { type master; file "<path>"; };` declarations (no `view` block), and SHALL carry a comment pointing to the migration guide for BIND `named.conf.default-zones` compatibility. This file is installed in addition to, and does not replace, the existing `named.conf.example`.

#### Scenario: Viewless example is installed

- **WHEN** the package is installed
- **THEN** `/etc/shadowdns/named.conf.viewless.example` SHALL exist AND contain a valid viewless `named.conf` skeleton with an `options` block and at least one top-level `zone` declaration of `type master` and no `view` block

#### Scenario: Viewless example loads without a fatal error

- **WHEN** ShadowDNS is started with `--named-conf` pointed at a copy of the installed `named.conf.viewless.example` whose zone `file` paths resolve to present zone files
- **THEN** the configuration loads without a fatal error AND the top-level zones are served via the synthesized default view

<!-- @trace
source: bind-migration-docs-examples
updated: 2026-06-13
code:
  - docs/index.md
  - testdata/integration/master/cnames/example.com_cname
  - testdata/integration/bindcompat/named.conf
  - internal/view/matcher.go
  - docs/migration.zh.md
  - nfpm.yaml
  - scripts/smoke.sh
  - testdata/integration/README.md
  - internal/config/match.go
  - testdata/integration/bindcompat/db.0
  - testdata/integration/named.conf.options
  - testdata/integration/bindcompat/named.conf.default-zones
  - testdata/integration/bindcompat/named.conf.local
  - testdata/integration/db.example.com-th
  - docs/configuration/named-conf.md
  - testdata/integration/master.zones
  - testdata/integration/named.conf
  - testdata/integration/bindcompat/db.local
  - testdata/integration/bindcompat/db.255
  - testdata/integration/bindcompat/shadowdns.yaml
  - testdata/integration/master/backup.example_view-other.fwd
  - packaging/named.conf.local.example
  - docs/getting-started.md
  - scripts/gen-container-testdata.go
  - docs/getting-started.zh.md
  - docs/configuration/named-conf.zh.md
  - testdata/integration/master/backup.example_view-th.fwd
  - testdata/integration/master/example.com_include.fwd
  - internal/config/options.go
  - scripts/test-deb.sh
  - testdata/integration/cnames/db.example.com.cname
  - docs/index.zh.md
  - testdata/integration/db.include-test.example
  - README.md
  - packaging/named.conf.options.example
  - docs/migration.md
  - testdata/integration/bindcompat/named.conf.options
  - testdata/integration/named.conf.local
  - internal/server/build.go
  - internal/view/netmatch.go
  - testdata/integration/db.backup.example.overrides
  - testdata/integration/bindcompat/README.md
  - testdata/integration/master/example.com_view-other.fwd
  - packaging/named.conf.example
  - testdata/integration/master/backup.example_overrides
  - internal/config/zones.go
  - testdata/integration/bindcompat/db.127
  - testdata/integration/master/example.com_view-th.fwd
  - testdata/integration/db.backup.example-th
  - testdata/integration/db.backup.example-other
  - testdata/integration/db.example.com-other
  - packaging/named.conf.viewless.example
tests:
  - internal/config/match_test.go
  - internal/server/handler_ecs_test.go
  - internal/server/build_test.go
  - internal/prunebackup/lexer_test.go
  - test/integration/alias_rdata_rewrite_test.go
  - test/integration/helpers_test.go
  - test/integration/query_test.go
  - test/integration/listenon_test.go
  - test/integration/prune_backup_test.go
  - internal/server/server_test.go
  - internal/config/zones_test.go
  - test/integration/bind_compat_test.go
  - internal/view/matcher_test.go
-->

---
### Requirement: systemd unit provides a writable state directory for the ACME account key

The packaged systemd service unit SHALL declare `StateDirectory=shadowdns` so that systemd creates `/var/lib/shadowdns` owned by the `shadowdns` user with mode `0700` on every start. This SHALL provide a writable, persistent location under the `ProtectSystem=strict` sandbox for the DoH ACME account key. The existing `ReadWritePaths=/var/log/shadowdns` directive and the runtime directory directive SHALL remain in place.

The account-key persistence guarantee depends on the unit running as a static service user (`User=shadowdns`). The unit SHALL NOT use `DynamicUser=yes`, because a per-boot dynamic UID would change `StateDirectory` ownership and render a previously persisted key unreadable on the next boot, silently reintroducing new-account churn.

#### Scenario: State directory exists for the service user

- **WHEN** the `shadowdns` service is started from the packaged unit
- **THEN** `/var/lib/shadowdns` SHALL exist, be owned by `shadowdns:shadowdns`, and be writable by the service so the DoH ACME account key can be persisted there

#### Scenario: Service uses a stable user so the persisted key survives reboots

- **WHEN** the packaged unit is inspected
- **THEN** it SHALL run as `User=shadowdns` and SHALL NOT set `DynamicUser=yes`, so the persisted account key written under `/var/lib/shadowdns` remains readable by the same UID across reboots


<!-- @trace
source: persist-acme-account-key
updated: 2026-06-27
code:
  - docs/configuration/shadowdns-yaml.md
  - internal/shadowdnscfg/config.go
  - internal/doh/acme_key.go
  - docs/configuration/shadowdns-yaml.zh.md
  - docs/guides/doh.zh.md
  - docs/guides/doh.md
  - packaging/shadowdns.yaml.example
  - packaging/shadowdns.service
  - internal/doh/acme.go
tests:
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/acme_integration_test.go
  - internal/doh/acme_key_test.go
  - internal/doh/helpers_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/shadowdnscfg/doh_test.go
-->

---
### Requirement: Example configuration pre-fills the ACME account key path

The packaged example configuration file SHALL include a `doh.acme.account_key_file` entry within its `doh.acme` section set to `/var/lib/shadowdns/acme/account.key`, so that an operator copying the example obtains a working default that aligns with the unit's state directory. The package SHALL NOT programmatically modify an operator's live configuration file to inject this value.

#### Scenario: Operator copying the example gets a valid account key path

- **WHEN** an operator copies the packaged example configuration and enables the `doh` section
- **THEN** the `doh.acme.account_key_file` value SHALL already point to `/var/lib/shadowdns/acme/account.key`, an absolute path under the unit's state directory

<!-- @trace
source: persist-acme-account-key
updated: 2026-06-27
code:
  - docs/configuration/shadowdns-yaml.md
  - internal/shadowdnscfg/config.go
  - internal/doh/acme_key.go
  - docs/configuration/shadowdns-yaml.zh.md
  - docs/guides/doh.zh.md
  - docs/guides/doh.md
  - packaging/shadowdns.yaml.example
  - packaging/shadowdns.service
  - internal/doh/acme.go
tests:
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/acme_integration_test.go
  - internal/doh/acme_key_test.go
  - internal/doh/helpers_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/shadowdnscfg/doh_test.go
-->