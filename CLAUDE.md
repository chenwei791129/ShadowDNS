# Project Build Commands

- `make build` — Build the ShadowDNS binary to `bin/shadowdns`
- `make test` — Run unit tests
- `make lint` — Run golangci-lint
- `make smoke` — Smoke test with `-dry-run`
- `make deb` — Build `.deb` package (requires nfpm via `go tool`)
- `make test-deb` — End-to-end container test of `.deb` package (requires podman or docker)

# Project Structure

- `packaging/` — Debian packaging assets (systemd service, example configs, install scripts)
- `scripts/` — Build and test helper scripts
- `nfpm.yaml` — nfpm configuration for `.deb` packaging

<!-- SPECTRA:START v1.0.1 -->

# Spectra Instructions

This project uses Spectra for Spec-Driven Development(SDD). Specs live in `openspec/specs/`, change proposals in `openspec/changes/`.

## Use `/spectra:*` skills when:

- A discussion needs structure before coding → `/spectra:discuss`
- User wants to plan, propose, or design a change → `/spectra:propose`
- Tasks are ready to implement → `/spectra:apply`
- There's an in-progress change to continue → `/spectra:ingest`
- User asks about specs or how something works → `/spectra:ask`
- Implementation is done → `/spectra:archive`

## Workflow

discuss? → propose → apply ⇄ ingest → archive

- `discuss` is optional — skip if requirements are clear
- Requirements change mid-work? Plan mode → `ingest` → resume `apply`

## Parked Changes

Changes can be parked（暫存）— temporarily moved out of `openspec/changes/`. Parked changes won't appear in `spectra list` but can be found with `spectra list --parked`. To restore: `spectra unpark <name>`. The `/spectra:apply` and `/spectra:ingest` skills handle parked changes automatically.

<!-- SPECTRA:END -->
