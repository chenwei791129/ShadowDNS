# Project Build Commands

- `make help` — List all annotated targets with colored output. Descriptions come from the trailing `## ...` comment on each target line, so new targets with a `##` comment appear automatically.
- `make build` — Build the ShadowDNS binary for the host platform at `bin/shadowdns-$(go env GOOS)-$(go env GOARCH)` (e.g., `bin/shadowdns-darwin-arm64`). Intended for local dev + unit tests on macOS/Linux.
- `make build-linux` — Cross-compile a linux/amd64 binary (`bin/shadowdns-linux-amd64`). Required input for `make deb`; on linux/amd64 hosts produces the same artefact as `make build`.
- `make test` — Run unit tests with the race detector enabled (`go test -race -count=1`)
- `make lint` — Run golangci-lint
- `make smoke` — Smoke test with `--dry-run`
- `make deb` — Build `.deb` package (implicitly runs `make build-linux` and `make completions`; requires nfpm via `go tool`)
- `make completions` — Generate bash/zsh/fish completion files at `bin/shadowdns.{bash,zsh,fish}` via `go run ./cmd/shadowdns completion <shell>`. Single source of truth for supported shells; consumed by `make deb` and `scripts/test-deb.sh`.
- `make test-deb` — End-to-end container test of `.deb` package (requires podman or docker)
- `make docs-serve` — Live-reload preview of the MkDocs manual site at http://127.0.0.1:8000 (requires uv; runs mkdocs-material + mkdocs-static-i18n via `uvx`, no global install)
- `make docs-build` — Render the manual site into `site/` (gitignored) with `--strict` (warnings fail the build, same as CI)

# Project Structure

- `packaging/` — Debian packaging assets (systemd service, example configs, install scripts)
- `scripts/` — Build and test helper scripts
- `nfpm.yaml` — nfpm configuration for `.deb` packaging
- `mkdocs.yml` + `docs/` — Bilingual MkDocs Material manual site via mkdocs-static-i18n (suffix structure): `page.md` is English (default, served at site root), `page.zh.md` is Traditional Chinese (served under `/zh/`). Every new page needs BOTH language files, an English `nav:` entry in `mkdocs.yml`, and a matching `nav_translations` entry under the i18n plugin's `zh` language. Published to GitHub Pages at https://chenwei791129.github.io/ShadowDNS/ by `.github/workflows/docs.yml` on push to main touching `docs/**` or `mkdocs.yml` (Pages source: GitHub Actions).

# Manual Site Maintenance

**Every change that alters user-observable behavior MUST be reviewed against the MkDocs manual before it is considered complete** — fire this review when a spectra change finishes implementation (alongside the CLAUDE.md review that runs before commit), and for any ad-hoc change touching the surfaces below.

Surfaces that require a manual update when touched:

- New or changed `shadowdns.yaml` fields → `docs/configuration/shadowdns-yaml.md` (field tables + example)
- New or changed CLI flags → `docs/reference/cli.md`
- New user-facing feature or changed query/response behavior → a Feature Guide under `docs/guides/` (new page or update to the relevant existing one)
- Feature availability changes → the comparison table in `docs/index.md` and the README features/planned lists
- Operational behavior (reload, logging, packaging, migration) → the relevant page under Operations

Rules:

- Internal-only refactors with no observable behavior change need no manual update — but state that conclusion explicitly instead of skipping the review silently.
- Bilingual invariant: never update one language file without the other; new pages need both files plus the two `mkdocs.yml` entries (see the structure note above). In `.zh.md` files, link to the base `.md` path (the i18n plugin localizes it) and use the target page's Chinese heading anchors.
- Verify with `make docs-build` (strict — broken links and nav mismatches fail the build) before committing.
- Docs examples are subject to the same sanitization rules as all committed files: RFC 2606 domains and RFC 5737 IPs only.

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
