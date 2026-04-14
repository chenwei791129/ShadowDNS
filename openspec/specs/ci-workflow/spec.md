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