# Project Build Commands

- `make build` — Build the ShadowDNS binary for the host platform at `bin/shadowdns-$(go env GOOS)-$(go env GOARCH)` (e.g., `bin/shadowdns-darwin-arm64`). Intended for local dev + unit tests on macOS/Linux.
- `make build-linux` — Cross-compile a linux/amd64 binary (`bin/shadowdns-linux-amd64`). Required input for `make deb`; on linux/amd64 hosts produces the same artefact as `make build`.
- `make test` — Run unit tests with the race detector enabled (`go test -race -count=1`)
- `make lint` — Run golangci-lint
- `make smoke` — Smoke test with `--dry-run`
- `make deb` — Build `.deb` package (implicitly runs `make build-linux` and `make completions`; requires nfpm via `go tool`)
- `make completions` — Generate bash/zsh/fish completion files at `bin/shadowdns.{bash,zsh,fish}` via `go run ./cmd/shadowdns completion <shell>`. Single source of truth for supported shells; consumed by `make deb` and `scripts/test-deb.sh`.
- `make test-deb` — End-to-end container test of `.deb` package (requires podman or docker)
- `make docs-serve` — Live-reload preview of the MkDocs manual site at http://127.0.0.1:8000 (requires uv; runs mkdocs-material via `uvx`, no global install)
- `make docs-build` — Render the manual site into `site/` (gitignored)

# Project Structure

- `packaging/` — Debian packaging assets (systemd service, example configs, install scripts)
- `scripts/` — Build and test helper scripts
- `nfpm.yaml` — nfpm configuration for `.deb` packaging
- `mkdocs.yml` + `docs/` — MkDocs Material manual site (Traditional Chinese). `docs/` doubles as the MkDocs `docs_dir`; every page must be registered in the `nav:` section of `mkdocs.yml`. Intended for GitHub Pages once the repo goes public; for now local preview only via `make docs-serve`.

<!-- SPECTRA:START v1.0.2 -->

# Spectra Instructions

This project uses Spectra for Spec-Driven Development(SDD). Specs live in `openspec/specs/`, change proposals in `openspec/changes/`.

## Use `/spectra-*` skills when:

- A discussion needs structure before coding → `/spectra-discuss`
- User wants to plan, propose, or design a change → `/spectra-propose`
- Tasks are ready to implement → `/spectra-apply`
- There's an in-progress change to continue → `/spectra-ingest`
- User asks about specs or how something works → `/spectra-ask`
- Implementation is done → `/spectra-archive`
- Commit only files related to a specific change → `/spectra-commit`

## Workflow

discuss? → propose → apply ⇄ ingest → archive

- `discuss` is optional — skip if requirements are clear
- Requirements change mid-work? Plan mode → `ingest` → resume `apply`

## Parked Changes

Changes can be parked（暫存）— temporarily moved out of `openspec/changes/`. Parked changes won't appear in `spectra list` but can be found with `spectra list --parked`. To restore: `spectra unpark <name>`. The `/spectra-apply` and `/spectra-ingest` skills handle parked changes automatically.

<!-- SPECTRA:END -->
