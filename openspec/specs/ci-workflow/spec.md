# ci-workflow Specification

## Purpose

TBD - created by archiving change 'github-actions'. Update Purpose after archive.

## Requirements

### Requirement: CI triggers on non-main push and pull request to main

The CI workflow SHALL trigger on `push` events to all branches except `main`, and on `pull_request` events targeting the `main` branch.

#### Scenario: Push to feature branch triggers CI

- **WHEN** a developer pushes commits to a branch other than `main`
- **THEN** the CI workflow SHALL execute

#### Scenario: Pull request to main triggers CI

- **WHEN** a pull request is opened or updated targeting the `main` branch
- **THEN** the CI workflow SHALL execute

#### Scenario: Push to main does not trigger CI

- **WHEN** a commit is pushed directly to the `main` branch
- **THEN** the CI workflow SHALL NOT execute


<!-- @trace
source: github-actions
updated: 2026-04-14
code:
  - .github/workflows/release-please.yml
  - scripts/gen-container-testdata.go
  - internal/server/listener.go
  - packaging/aliases.yaml.example
  - internal/config/options.go
  - packaging/shadowdns.service
  - nfpm.yaml
  - cmd/shadowdns/main.go
  - scripts/test-deb.sh
  - go.sum
  - go.mod
  - CLAUDE.md
  - packaging/named.conf.example
  - .github/workflows/ci.yml
  - Makefile
  - packaging/postinstall.sh
tests:
  - internal/config/options_test.go
  - cmd/shadowdns/main_test.go
-->

---
### Requirement: CI runs test, lint, and smoke in sequence

The CI workflow SHALL execute `make test`, `make lint`, and `make smoke` in that order. If any step fails, the workflow SHALL fail and subsequent steps SHALL NOT execute.

#### Scenario: All checks pass

- **WHEN** the CI workflow executes and all three steps (`test`, `lint`, `smoke`) succeed
- **THEN** the workflow SHALL report success

#### Scenario: Test failure stops pipeline

- **WHEN** `make test` fails
- **THEN** the workflow SHALL fail and SHALL NOT execute `make lint` or `make smoke`

#### Scenario: Lint failure stops pipeline

- **WHEN** `make test` succeeds but `make lint` fails
- **THEN** the workflow SHALL fail and SHALL NOT execute `make smoke`


<!-- @trace
source: github-actions
updated: 2026-04-14
code:
  - .github/workflows/release-please.yml
  - scripts/gen-container-testdata.go
  - internal/server/listener.go
  - packaging/aliases.yaml.example
  - internal/config/options.go
  - packaging/shadowdns.service
  - nfpm.yaml
  - cmd/shadowdns/main.go
  - scripts/test-deb.sh
  - go.sum
  - go.mod
  - CLAUDE.md
  - packaging/named.conf.example
  - .github/workflows/ci.yml
  - Makefile
  - packaging/postinstall.sh
tests:
  - internal/config/options_test.go
  - cmd/shadowdns/main_test.go
-->

---
### Requirement: CI uses Go version from go.mod

The CI workflow SHALL use `go-version-file: go.mod` with the `actions/setup-go` action to ensure the CI Go version matches the project's declared version.

#### Scenario: Go version matches go.mod

- **WHEN** the CI workflow sets up Go
- **THEN** the installed Go version SHALL match the version specified in `go.mod`


<!-- @trace
source: github-actions
updated: 2026-04-14
code:
  - .github/workflows/release-please.yml
  - scripts/gen-container-testdata.go
  - internal/server/listener.go
  - packaging/aliases.yaml.example
  - internal/config/options.go
  - packaging/shadowdns.service
  - nfpm.yaml
  - cmd/shadowdns/main.go
  - scripts/test-deb.sh
  - go.sum
  - go.mod
  - CLAUDE.md
  - packaging/named.conf.example
  - .github/workflows/ci.yml
  - Makefile
  - packaging/postinstall.sh
tests:
  - internal/config/options_test.go
  - cmd/shadowdns/main_test.go
-->

---
### Requirement: CI has minimal permissions and no secrets

The CI workflow SHALL set `permissions: contents: read` and SHALL NOT reference any secrets (including `MY_RELEASE_PLEASE_TOKEN` and `GITHUB_TOKEN` for write operations).

#### Scenario: Fork PR cannot access secrets

- **WHEN** an external contributor submits a pull request from a forked repository
- **THEN** the CI workflow SHALL execute without access to any repository secrets


<!-- @trace
source: github-actions
updated: 2026-04-14
code:
  - .github/workflows/release-please.yml
  - scripts/gen-container-testdata.go
  - internal/server/listener.go
  - packaging/aliases.yaml.example
  - internal/config/options.go
  - packaging/shadowdns.service
  - nfpm.yaml
  - cmd/shadowdns/main.go
  - scripts/test-deb.sh
  - go.sum
  - go.mod
  - CLAUDE.md
  - packaging/named.conf.example
  - .github/workflows/ci.yml
  - Makefile
  - packaging/postinstall.sh
tests:
  - internal/config/options_test.go
  - cmd/shadowdns/main_test.go
-->

---
### Requirement: CI uses pull_request event not pull_request_target

The CI workflow SHALL use the `pull_request` event trigger. The workflow SHALL NOT use `pull_request_target`.

#### Scenario: Workflow file uses correct event

- **WHEN** the CI workflow file is inspected
- **THEN** the PR trigger SHALL be `pull_request` and `pull_request_target` SHALL NOT appear in the file

<!-- @trace
source: github-actions
updated: 2026-04-14
code:
  - .github/workflows/release-please.yml
  - scripts/gen-container-testdata.go
  - internal/server/listener.go
  - packaging/aliases.yaml.example
  - internal/config/options.go
  - packaging/shadowdns.service
  - nfpm.yaml
  - cmd/shadowdns/main.go
  - scripts/test-deb.sh
  - go.sum
  - go.mod
  - CLAUDE.md
  - packaging/named.conf.example
  - .github/workflows/ci.yml
  - Makefile
  - packaging/postinstall.sh
tests:
  - internal/config/options_test.go
  - cmd/shadowdns/main_test.go
-->

---
### Requirement: Pull Request CI validates the container image build

The CI workflow SHALL build the project Dockerfile for `linux/amd64` on every pull request targeting `main`. The build SHALL use the development version input and SHALL fail CI if the image cannot be built. This validation SHALL NOT log in to any container registry, reference a package-write credential, push an image, or run an image-level vulnerability scan.

#### Scenario: Pull Request image build succeeds

- **WHEN** a pull request targeting `main` contains a valid Dockerfile and build context
- **THEN** CI SHALL successfully build a local `linux/amd64` image with the development version input and SHALL NOT push it

#### Scenario: Pull Request image build fails

- **WHEN** the Dockerfile or build context cannot produce the `linux/amd64` image
- **THEN** the container build validation SHALL fail the CI workflow

#### Scenario: Fork Pull Request has no registry access

- **WHEN** an external contributor opens a pull request from a fork
- **THEN** the image build SHALL run using only repository read access and SHALL perform no registry login or package write


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
### Requirement: Pull Request CI verifies the image runtime contract

After building the local image, CI SHALL invoke a repository-owned verification script shared with local development, and SHALL do so for **both** supported container runtimes (podman and docker) so that the runtime used by the release workflow is exercised before merge. The script SHALL verify the image contract: architecture `amd64`, UID/GID `65532`, the ShadowDNS exec-form entrypoint, the complete default command including `0.0.0.0:5353`, exposed `5353/udp`, `5353/tcp`, and `9153/tcp` ports, absence of a Docker health check, and development version output. The checks SHALL fail when any inspected value differs from the container-image specification.

#### Scenario: Image contract matches

- **WHEN** the Pull Request image build completes
- **THEN** CI SHALL inspect the image and confirm every required runtime configuration value and SHALL run `--version` to confirm output `dev`

#### Scenario: Image contract drifts

- **WHEN** the built image has a root user, altered default command, missing port, configured health check, wrong architecture, wrong entrypoint, or non-dev default version
- **THEN** the runtime contract verification SHALL fail CI

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