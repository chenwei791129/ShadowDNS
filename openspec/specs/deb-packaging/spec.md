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
- **THEN** `/etc/shadowdns/named.conf.example` SHALL exist and contain a valid `named.conf` skeleton with `options`, `geoip-directory`, and `view` blocks

#### Scenario: Example aliases.yaml is installed

- **WHEN** the package is installed
- **THEN** `/etc/shadowdns/aliases.yaml.example` SHALL exist and contain a valid aliases mapping with at least one root-to-backup example

#### Scenario: Example files are not overwritten on upgrade

- **WHEN** the package is upgraded to a newer version
- **THEN** the example files SHALL be replaced (they are examples, not user config), and no user confirmation SHALL be required


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
- **THEN** the output directory SHALL contain `named.conf`, `aliases.yaml`, `master.zones`, `master/*.fwd` zone files, and `geoip/GeoLite2-Country.mmdb` and `geoip/GeoLite2-ASN.mmdb` with all path placeholders replaced by the target path


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