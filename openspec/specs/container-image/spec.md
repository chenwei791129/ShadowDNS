# container-image Specification

## Purpose

TBD - created by archiving change 'add-container-image'. Update Purpose after archive.

## Requirements

### Requirement: Multi-stage linux/amd64 image build

The project SHALL provide a multi-stage Dockerfile that builds `./cmd/shadowdns` with `CGO_ENABLED=0`, `GOOS=linux`, and `GOARCH=amd64`, then copies only the resulting binary into a Distroless static Debian 13 nonroot runtime image. The Go builder version SHALL match the version declared in `go.mod`. Both builder and runtime base images MUST be pinned by immutable digest. The final image architecture SHALL be `linux/amd64`.

#### Scenario: Local development image build

- **WHEN** the Dockerfile is built for `linux/amd64` without a VERSION build argument
- **THEN** the build SHALL succeed and running the image with `--version` SHALL print `dev`

#### Scenario: Versioned release image build

- **WHEN** the Dockerfile is built for `linux/amd64` with VERSION set to `v0.9.0`
- **THEN** the binary SHALL be linked with `-s -w -X main.version=v0.9.0` and running the image with `--version` SHALL print `v0.9.0`

#### Scenario: Final image contains only runtime requirements

- **WHEN** the final image is inspected
- **THEN** it SHALL use the pinned Distroless static Debian 13 nonroot base and SHALL NOT install a shell, package manager, DNS client, HTTP client, configuration fixture, zone data, or GeoIP database


<!-- @trace
source: add-container-image
updated: 2026-08-26
code:
  - docs/operations/reloading.zh.md
  - docs/guides/environment-variables.zh.md
  - internal/shadowdnscfg/envexpand.go
  - docs/configuration/shadowdns-yaml.md
  - docs/configuration/shadowdns-yaml.zh.md
  - docs/operations/reloading.md
  - docs/guides/environment-variables.md
  - internal/shadowdnscfg/config.go
  - mkdocs.yml
tests:
  - cmd/shadowdns/main_reload_test.go
  - cmd/shadowdns/listenon_test.go
  - cmd/shadowdns/main_test.go
  - internal/shadowdnscfg/config_test.go
  - internal/shadowdnscfg/envexpand_test.go
  - cmd/shadowdns/prune_backup_test.go
-->

---
### Requirement: Non-root container execution contract

The final image SHALL run as numeric UID and GID `65532`, SHALL NOT require Linux capabilities, and SHALL use only unprivileged default listener ports. It SHALL declare `/usr/local/bin/shadowdns` as its exec-form entrypoint and SHALL pass signals directly to ShadowDNS as PID 1 without an entrypoint wrapper.

#### Scenario: Runtime identity is non-root

- **WHEN** the final image configuration is inspected
- **THEN** its user SHALL be `65532:65532` or the Distroless equivalent resolving to UID and GID `65532`

#### Scenario: Container stops gracefully

- **WHEN** a running container receives SIGTERM
- **THEN** ShadowDNS SHALL receive SIGTERM directly as PID 1 and the container SHALL exit through the existing graceful shutdown path

#### Scenario: Container reloads configuration

- **WHEN** a running container receives SIGHUP
- **THEN** ShadowDNS SHALL receive SIGHUP directly as PID 1 and SHALL execute its existing atomic configuration reload path


<!-- @trace
source: add-container-image
updated: 2026-08-26
code:
  - docs/operations/reloading.zh.md
  - docs/guides/environment-variables.zh.md
  - internal/shadowdnscfg/envexpand.go
  - docs/configuration/shadowdns-yaml.md
  - docs/configuration/shadowdns-yaml.zh.md
  - docs/operations/reloading.md
  - docs/guides/environment-variables.md
  - internal/shadowdnscfg/config.go
  - mkdocs.yml
tests:
  - cmd/shadowdns/main_reload_test.go
  - cmd/shadowdns/listenon_test.go
  - cmd/shadowdns/main_test.go
  - internal/shadowdnscfg/config_test.go
  - internal/shadowdnscfg/envexpand_test.go
  - cmd/shadowdns/prune_backup_test.go
-->

---
### Requirement: Containerized default command and ports

The image SHALL provide an exec-form default command equivalent to `--named-conf /etc/shadowdns/named.conf --config /etc/shadowdns/shadowdns.yaml --listen 0.0.0.0:5353 --no-color`. The explicit IPv4 wildcard host SHALL override `listen-on` and `listen-on-v6` addresses from mounted host-oriented BIND configuration, making Docker IPv4 port publishing reachable. The official default SHALL be IPv4-only; first-class dual-stack container listening is outside this change. The default command SHALL remain fully replaceable by a user-supplied container command. The image SHALL expose `5353/udp`, `5353/tcp`, and `9153/tcp` as metadata.

#### Scenario: Image starts with mounted configuration

- **WHEN** valid `named.conf` and `shadowdns.yaml` files and their referenced zone data are mounted under `/etc/shadowdns`
- **THEN** the default command SHALL load those files, ignore their host-specific `listen-on` and `listen-on-v6` addresses, bind IPv4 DNS on `0.0.0.0:5353` over UDP and TCP, and expose Prometheus metrics on container port 9153

#### Scenario: Required configuration is absent

- **WHEN** the image is started with its default command and `/etc/shadowdns/named.conf` or `/etc/shadowdns/shadowdns.yaml` is absent
- **THEN** ShadowDNS SHALL report its native configuration load error and the container SHALL exit with a non-zero status rather than start with embedded fallback data

#### Scenario: User replaces default command

- **WHEN** the operator supplies `--version` as the container command
- **THEN** the image SHALL execute `/usr/local/bin/shadowdns --version` without retaining any default server flags


<!-- @trace
source: add-container-image
updated: 2026-08-26
code:
  - docs/operations/reloading.zh.md
  - docs/guides/environment-variables.zh.md
  - internal/shadowdnscfg/envexpand.go
  - docs/configuration/shadowdns-yaml.md
  - docs/configuration/shadowdns-yaml.zh.md
  - docs/operations/reloading.md
  - docs/guides/environment-variables.md
  - internal/shadowdnscfg/config.go
  - mkdocs.yml
tests:
  - cmd/shadowdns/main_reload_test.go
  - cmd/shadowdns/listenon_test.go
  - cmd/shadowdns/main_test.go
  - internal/shadowdnscfg/config_test.go
  - internal/shadowdnscfg/envexpand_test.go
  - cmd/shadowdns/prune_backup_test.go
-->

---
### Requirement: Deployment-owned configuration and state

The image SHALL NOT embed deployment configuration or declare Docker-managed anonymous volumes. Documentation SHALL define `/etc/shadowdns` as the conventional read-only configuration and zone-data mount and `/var/lib/shadowdns` as the conventional optional persistent state mount for ACME account keys. Any writable state or file-backed log mount MUST be writable by UID `65532`; any additional absolute path referenced by `named.conf` or `shadowdns.yaml` MUST be mounted explicitly.

#### Scenario: Read-only configuration mount

- **WHEN** an operator mounts complete configuration and referenced read-only data at `/etc/shadowdns`
- **THEN** ShadowDNS SHALL be able to load that data without requiring write access to the mount

#### Scenario: Persisted ACME state

- **WHEN** DoH ACME is enabled with its account key under `/var/lib/shadowdns` and that path is backed by a persistent volume writable by UID `65532`
- **THEN** ShadowDNS SHALL preserve and reuse the account key across container replacement

#### Scenario: Writable mount has incompatible ownership

- **WHEN** ShadowDNS needs to create an ACME key or file-backed log on a mount that UID `65532` cannot write
- **THEN** the operation SHALL fail with the existing ShadowDNS file error and the image SHALL NOT retry as root


<!-- @trace
source: add-container-image
updated: 2026-08-26
code:
  - docs/operations/reloading.zh.md
  - docs/guides/environment-variables.zh.md
  - internal/shadowdnscfg/envexpand.go
  - docs/configuration/shadowdns-yaml.md
  - docs/configuration/shadowdns-yaml.zh.md
  - docs/operations/reloading.md
  - docs/guides/environment-variables.md
  - internal/shadowdnscfg/config.go
  - mkdocs.yml
tests:
  - cmd/shadowdns/main_reload_test.go
  - cmd/shadowdns/listenon_test.go
  - cmd/shadowdns/main_test.go
  - internal/shadowdnscfg/config_test.go
  - internal/shadowdnscfg/envexpand_test.go
  - cmd/shadowdns/prune_backup_test.go
-->

---
### Requirement: Container logging, probes, and optional listeners

The default container command SHALL leave operational logging on stderr. The image SHALL NOT define a Docker `HEALTHCHECK`. Documentation SHALL require deployment-owned readiness and liveness probes based on an operator-selected authoritative DNS record and/or the metrics endpoint. DoH HTTPS, ACME HTTP-01, and the ephemeral API SHALL use operator-configured unprivileged container ports and external port mapping when enabled.

#### Scenario: Runtime collects default logs

- **WHEN** the image runs without a user-supplied `--log-file`
- **THEN** ShadowDNS operational logs SHALL be emitted to stderr for collection by the container runtime

#### Scenario: Image health metadata is inspected

- **WHEN** the final image configuration is inspected
- **THEN** no Docker health check SHALL be configured

#### Scenario: Deployment configures a DNS readiness probe

- **WHEN** an operator chooses a record known to exist in their mounted authoritative zone
- **THEN** the deployment SHALL be able to probe that record through container port 5353 without requiring a DNS client inside the image

#### Scenario: DoH and ACME are enabled

- **WHEN** the operator enables DoH and ACME HTTP-01 in `shadowdns.yaml`
- **THEN** their configured container listeners MUST use ports greater than 1023 and the deployment SHALL map or route external ports 443 and 80 to those unprivileged ports


<!-- @trace
source: add-container-image
updated: 2026-08-26
code:
  - docs/operations/reloading.zh.md
  - docs/guides/environment-variables.zh.md
  - internal/shadowdnscfg/envexpand.go
  - docs/configuration/shadowdns-yaml.md
  - docs/configuration/shadowdns-yaml.zh.md
  - docs/operations/reloading.md
  - docs/guides/environment-variables.md
  - internal/shadowdnscfg/config.go
  - mkdocs.yml
tests:
  - cmd/shadowdns/main_reload_test.go
  - cmd/shadowdns/listenon_test.go
  - cmd/shadowdns/main_test.go
  - internal/shadowdnscfg/config_test.go
  - internal/shadowdnscfg/envexpand_test.go
  - cmd/shadowdns/prune_backup_test.go
-->

---
### Requirement: Repository-owned container runtime verification

The project SHALL provide a repository-owned entry point, shared between local development and future maintenance, that runs the built image and verifies its runtime contract end to end — distinct from the static image-configuration verification. The entry point SHALL reuse the existing shared test-fixture generator instead of a Docker-specific fixture format, SHALL mount the generated configuration read-only, and SHALL verify authoritative answers over UDP and TCP, the metrics endpoint, stderr-only operational logging, SIGHUP reload, and bounded SIGTERM graceful shutdown. It SHALL exit non-zero on any failing check.

#### Scenario: Maintainer re-runs the runtime contract

- **WHEN** a maintainer changes the Dockerfile, entrypoint, or default command and runs the runtime verification entry point against the rebuilt image
- **THEN** it SHALL start the image with a read-only generated configuration mount and SHALL confirm each runtime behavior without the maintainer reconstructing the fixture, query, and signal sequence by hand

#### Scenario: Runtime behavior regresses

- **WHEN** the running container fails to answer over UDP or TCP, does not serve metrics, writes operational logs to stdout, ignores SIGHUP, or does not exit cleanly on SIGTERM
- **THEN** the entry point SHALL report the failing check and SHALL exit non-zero


<!-- @trace
source: add-container-image
updated: 2026-08-26
code:
  - docs/operations/reloading.zh.md
  - docs/guides/environment-variables.zh.md
  - internal/shadowdnscfg/envexpand.go
  - docs/configuration/shadowdns-yaml.md
  - docs/configuration/shadowdns-yaml.zh.md
  - docs/operations/reloading.md
  - docs/guides/environment-variables.md
  - internal/shadowdnscfg/config.go
  - mkdocs.yml
tests:
  - cmd/shadowdns/main_reload_test.go
  - cmd/shadowdns/listenon_test.go
  - cmd/shadowdns/main_test.go
  - internal/shadowdnscfg/config_test.go
  - internal/shadowdnscfg/envexpand_test.go
  - cmd/shadowdns/prune_backup_test.go
-->

---
### Requirement: OCI image metadata and build context

The Docker build SHALL accept source URL, source revision, version, and a stable source creation time as metadata inputs and SHALL emit the corresponding standard OCI labels on the final image. The source label SHALL identify the public ShadowDNS source repository so GHCR can associate the package with the repository. The `.dockerignore` file SHALL use a default-deny allowlist so the build context contains only `go.mod`, `go.sum`, `cmd/**/*.go`, `internal/**/*.go`, and the canonical `scripts/build-linux.sh` compiler recipe, plus Docker control files when required by the builder. It SHALL NOT rely on an open-ended denylist of currently known local files.

#### Scenario: Release image metadata is inspectable

- **WHEN** a release image is inspected
- **THEN** it SHALL contain non-empty `org.opencontainers.image.source`, `org.opencontainers.image.revision`, `org.opencontainers.image.version`, and `org.opencontainers.image.created` labels matching that release build

#### Scenario: Build context contains only compiler inputs

- **WHEN** Docker prepares the project build context
- **THEN** the allowlist SHALL include `go.mod`, `go.sum`, non-test `cmd/**/*.go`, non-test `internal/**/*.go`, and `scripts/build-linux.sh` and SHALL exclude every other project path by default, including `.git`, `.local`, `*.local.md` (including `CLAUDE.local.md`), `bin`, `site`, documentation, tests, fixtures, packaging assets, and future unlisted files


<!-- @trace
source: add-container-image
updated: 2026-08-26
code:
  - docs/operations/reloading.zh.md
  - docs/guides/environment-variables.zh.md
  - internal/shadowdnscfg/envexpand.go
  - docs/configuration/shadowdns-yaml.md
  - docs/configuration/shadowdns-yaml.zh.md
  - docs/operations/reloading.md
  - docs/guides/environment-variables.md
  - internal/shadowdnscfg/config.go
  - mkdocs.yml
tests:
  - cmd/shadowdns/main_reload_test.go
  - cmd/shadowdns/listenon_test.go
  - cmd/shadowdns/main_test.go
  - internal/shadowdnscfg/config_test.go
  - internal/shadowdnscfg/envexpand_test.go
  - cmd/shadowdns/prune_backup_test.go
-->

---
### Requirement: Container installation documentation

The English README and bilingual manual SHALL document anonymous pull from the public GHCR package, exact-version and `latest` tags, linux/amd64-only support, required mounts, UID `65532` ownership, UDP and TCP port mappings, stderr logging, deployment-owned probes, signal-based reload and shutdown, unprivileged optional service ports, and the one-time package-owner step to make the first GHCR package public. Production examples SHALL use an exact version tag rather than `latest`.

#### Scenario: Operator follows the documented run example

- **WHEN** an operator prepares complete configuration under `/etc/shadowdns`, mounts it read-only, and follows the documented `docker run` example
- **THEN** the container SHALL publish host DNS port 53 to container port 5353 over both UDP and TCP and SHALL publish metrics port 9153 over TCP

#### Scenario: Package owner completes first publication

- **WHEN** the first release workflow has pushed the GHCR package
- **THEN** the documentation SHALL direct the package owner to change visibility to Public in GitHub package settings, confirm repository association, and verify an unauthenticated pull

<!-- @trace
source: add-container-image
updated: 2026-08-26
code:
  - docs/operations/reloading.zh.md
  - docs/guides/environment-variables.zh.md
  - internal/shadowdnscfg/envexpand.go
  - docs/configuration/shadowdns-yaml.md
  - docs/configuration/shadowdns-yaml.zh.md
  - docs/operations/reloading.md
  - docs/guides/environment-variables.md
  - internal/shadowdnscfg/config.go
  - mkdocs.yml
tests:
  - cmd/shadowdns/main_reload_test.go
  - cmd/shadowdns/listenon_test.go
  - cmd/shadowdns/main_test.go
  - internal/shadowdnscfg/config_test.go
  - internal/shadowdnscfg/envexpand_test.go
  - cmd/shadowdns/prune_backup_test.go
-->