## Why

ShadowDNS is a public open-source project, but every `named.conf` example it ships and tests against uses a FreeBSD-flavoured layout inherited from a private production nameserver: `directory "/etc/namedb"`, an `include "master.zones"` file, a `master/` subdirectory, and a `_view-th.fwd` zone-file naming scheme. Contributors and operators on Linux — where ShadowDNS ships as a `.deb` and is deployed — do not recognise this layout, so the examples teach an unfamiliar convention. Aligning the examples, fixtures, and docs to the Debian/Ubuntu BIND convention makes the project legible to its actual audience without changing any runtime behaviour.

## Summary

Re-shell every outward-facing `named.conf` example, the shared integration fixture, and the documentation snippets from the FreeBSD-style layout to the full Debian/Ubuntu BIND split (`named.conf` → `named.conf.options` + `named.conf.local`), while preserving the same views, GeoIP rules, and zone content.

## Motivation

The parser hard-codes no paths — it follows whatever `include` and `file` directives the config text contains. Verified against the shipped binary: zone classification keys on the zone **origin** (`internal/server/build.go`: `origin := z.Name + "."`) and on alias membership by zone name from `shadowdns.yaml`; no code in `cmd/` or `internal/` matches on filenames. The only place that hard-codes fixture filenames is `scripts/gen-container-testdata.go` (a build/test helper, not the binary). So the FreeBSD layout is purely a documentation/fixture convention, not a behavioural constraint. Because that convention is the first thing a contributor reads in `packaging/named.conf.example`, the README, and the manual, it should reflect the platform ShadowDNS targets. A half-migration would leave the shipped example, the manual, and the `deb-packaging` spec contradicting the fixtures, so the migration must sweep every surface that carries the convention.

**Parser exception discovered during implementation.** The path-resolution claim above holds, but there is one exception the original plan missed: `internal/config/zones.go` deliberately applied an `options { ... }` block **only when it appeared in the root `named.conf`** (`if cfg.Path == path`), silently dropping any options block reached via `include`. The Debian split moves `options {}` into the included `named.conf.options`, so without a parser change every options field (`directory`, `geoip-directory`, `listen-on`, `rate-limit`, `allow-transfer`) would be silently dropped — `make smoke` fails with `geoip-directory is not set`, and the shipped `named.conf.options.example` would mislead operators into a broken config. The integration suite did not catch this because it injects GeoIP handles directly and colocates zone files with their declaring file (masking both the dropped `geoip-directory` and the dropped `directory`). This change therefore extends the loader to honor an `options {}` block from any file in the include tree (textual-inclusion semantics, matching BIND), with last-block-wins + a warning on duplicates. See the `config-loader` delta.

## Proposed Solution

Adopt the full Debian/Ubuntu BIND skeleton across all example/fixture/doc surfaces:

- **Include split**: `named.conf` holds only `include "named.conf.options";` and `include "named.conf.local";`. The `options {}` (and `logging {}`) block moves to `named.conf.options`; the `view` blocks move to `named.conf.local`. This is the idiomatic Debian structure and gives the integration tests two include levels of path-resolution coverage (up from one).
- **directory**: examples use `directory "/etc/bind"` — the Debian-idiomatic location for authoritative zone files alongside config (this is the convention used by community guides, not the literal package default `/var/cache/bind`, which is a writable cache dir). The shared fixture keeps `TESTDATA_DIR_PLACEHOLDER` in `named.conf.options` (substituted to `t.TempDir()` by the loader helper) — examples differ from the fixture only in this value.
- **Rename `master.zones` → `named.conf.local`**, remove the `master/` subdirectory, and move zone files beside the config.
- **Zone-file naming**: split-horizon zones use `db.<zone>-<view>` (hyphen separating the view tag from the dotted zone name, so the zone boundary is unambiguous): `db.example.com-th`, `db.example.com-other`, `db.backup.example-th`, `db.backup.example-other`. Single-view zones use plain `db.<zone>`: `db.include-test.example`. `$INCLUDE` fragments keep descriptive names: `db.backup.example.overrides` (relative include) and `cnames/db.example.com.cname` (nested subdir, exercising the quoted + bare `$INCLUDE` variants).
- View blocks, `match-clients` GeoIP rules, `recursion no`, and all zone record content are carried over unchanged — this is a re-shell, not a content rewrite.

Apply the split consistently to: the shared fixture (`testdata/integration/`) and its loader helper, the container-testdata generator script, the packaging examples (now three files — `named.conf.example` + `named.conf.options.example` + `named.conf.local.example`) and their `nfpm.yaml` entries, the `deb-packaging` spec scenarios that name these files, the bilingual manual pages (including getting-started), and the README example. The shared fixture remains a **parser test fixture** (it retains test-coverage artifacts — the `cnames/` subdir and the quoted+bare `$INCLUDE` forms — that a minimal recommended layout would not have); its README states this explicitly so it is not mistaken for the smallest viable config.

Inline test-config fixtures that build their own `master.zones` / `master/` configs in a `t.TempDir()` (across `cmd/shadowdns/*_test.go`, `internal/config/*_test.go`, `internal/prunebackup/*_test.go`, and the inline-building integration tests) are **out of scope** — see Non-Goals.

## Non-Goals

- **Parser change limited to options-include scoping.** The only shipped-binary change is `internal/config/zones.go` honoring an `options {}` block declared in an included file (see Motivation + the `config-loader` delta); this is required for the Debian split to function at all. Zone classification, alias resolution, and path resolution are otherwise unchanged. Because a hot-path file (`internal/config`) is modified, **perf-guard runs** for this change rather than being skipped.
- **Not changing the views, GeoIP rules, or DNS record content.** Only file names, the include structure, and the `directory` value change.
- **Not adopting RHEL or FreeBSD conventions.** Debian/Ubuntu is chosen because ShadowDNS ships a `.deb` and deploys on Debian-family hosts.
- **Not normalising the `aliases.yaml` / `shadowdns.yaml` naming** surfaced by the generator — out of scope for this layout change.
- **Not migrating inline test-config fixtures.** Tests that synthesize their own `master.zones` / `master/*.fwd` config inside a `t.TempDir()` (in `cmd/shadowdns/*_test.go`, `internal/config/zones_test.go` and siblings, `internal/prunebackup/*_test.go`, and the inline-building integration tests) keep that naming: there the filenames are arbitrary scaffolding exercising parser/server/path-resolution mechanics, not a user-facing deployment layout. Renaming them would be churn and risk without serving the legibility goal. Only the **shared** `testdata/integration/` fixture and outward-facing examples/docs migrate.

## Alternatives Considered

- **Only change `testdata/integration/`** — rejected: leaves `packaging/named.conf.example`, the manual, and the `deb-packaging` spec contradicting the fixtures.
- **Keep FreeBSD `/etc/namedb` + `master/`, only rename `.fwd`** — rejected: still an unfamiliar layout for the Linux audience.
- **RHEL convention (`/var/named`, `named.<zone>`, `named.rfc1912.zones`)** — rejected: inconsistent with the project's `.deb` packaging and Debian deployment target.
- **Self-contained single-file example (no include split)** — rejected: easiest to copy, but the primary user-facing artifact would not demonstrate the Debian include idiom and would diverge structurally from the fixture.
- **Dotted view suffix `db.example.com.view-th`** — rejected: the dot conflates the view tag with the dotted zone name, making the zone boundary ambiguous on sight; the hyphen form is clearer.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `deb-packaging`: two requirements change. **Example configuration files** — `named.conf.example` becomes an include skeleton, and `named.conf.options.example` + `named.conf.local.example` are newly installed. **Container testdata generator** — expected output filenames change from `master.zones` + `master/*.fwd` to `named.conf` + `named.conf.options` + `named.conf.local` + `db.<zone>[-<view>]`.
- `config-loader`: one requirement added — **Honor options block from included files** (the loader applies an `options {}` block declared in any included file, with last-block-wins + a warning on duplicates). Discovered necessary during implementation; without it the Debian split silently drops every options field.

## Impact

- Affected specs:
  - `deb-packaging` (Example configuration files + Container testdata generator scenarios — proper delta)
  - `config-loader` (ADDED requirement "Honor options block from included files" — proper delta)
  - `@trace` hygiene only (no requirement change): `alias-resolver`, `config-loader`, `dns-server`, `ephemeral-api`, `ephemeral-record-store`, `logging`, `prune-backup-cli`, `shadowdns-config`, `sighup-reload`, `view-matcher`, `zone-parser`, `zone-transfer` — these reference the renamed/removed shared-fixture files in `@trace code:` blocks and must be updated so they do not dangle (archive reconciles the deltaed specs: `deb-packaging` + `config-loader`).
- Affected code:
  - Modified:
    - internal/config/zones.go (parser: honor options{} from included files)
    - internal/config/zones_test.go (tests for included-options behavior)
    - testdata/integration/named.conf
    - testdata/integration/README.md
    - test/integration/helpers_test.go
    - test/integration/query_test.go
    - test/integration/listenon_test.go
    - test/integration/prune_backup_test.go
    - internal/prunebackup/lexer_test.go
    - scripts/gen-container-testdata.go
    - scripts/smoke.sh
    - scripts/test-deb.sh
    - packaging/named.conf.example
    - nfpm.yaml
    - docs/configuration/named-conf.md
    - docs/configuration/named-conf.zh.md
    - docs/migration.md
    - docs/migration.zh.md
    - docs/getting-started.md
    - docs/getting-started.zh.md
    - README.md
  - New:
    - testdata/integration/named.conf.options
    - testdata/integration/named.conf.local
    - testdata/integration/db.example.com-th
    - testdata/integration/db.example.com-other
    - testdata/integration/db.backup.example-th
    - testdata/integration/db.backup.example-other
    - testdata/integration/db.backup.example.overrides
    - testdata/integration/db.include-test.example
    - testdata/integration/cnames/db.example.com.cname
    - packaging/named.conf.options.example
    - packaging/named.conf.local.example
  - Removed:
    - testdata/integration/master.zones
    - testdata/integration/master/example.com_view-th.fwd
    - testdata/integration/master/example.com_view-other.fwd
    - testdata/integration/master/example.com_include.fwd
    - testdata/integration/master/backup.example_view-th.fwd
    - testdata/integration/master/backup.example_view-other.fwd
    - testdata/integration/master/backup.example_overrides
    - testdata/integration/master/cnames/example.com_cname
