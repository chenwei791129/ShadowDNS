## Why

Changes `bind-config-tolerant-parsing` (A) and `bind-named-acl-match-clients` (B) made ShadowDNS load any valid BIND named.conf and correctly serve ACL-based split-horizon configurations — but that capability is currently undocumented and undiscoverable. This is the third and final change on the BIND-compatibility roadmap.

Operators migrating from BIND need explicit guidance: that they can point `--named-conf` at an existing `/etc/bind/named.conf`, which BIND constructs ShadowDNS tolerates versus ignores, and how ShadowDNS's view-selection access-control model differs from BIND's `allow-query` (which ShadowDNS does not enforce). The tiered-tolerance and fail-closed contract introduced by A and B needs to be written down so the behavior is predictable rather than discovered by trial. Greenfield users who want a BIND-style authoritative setup need a viewless example to copy.

## What Changes

- **Extend the existing migration guide** (`docs/migration.md` + `.zh.md`, the "Migrating from BIND" page): add a drop-in section covering `--named-conf /etc/bind/named.conf`, what is tolerated/ignored on load, and the access-control model difference. The guide MUST state the scope distinction precisely: ShadowDNS selects views by `match-clients` and honors **options-scope** `allow-transfer` as the AXFR ACL (existing zone-transfer behavior, already relied on elsewhere in this guide), but does **not** enforce `allow-query`/`allow-recursion`/`allow-transfer` declared at **view or zone scope** — those are logged as ignored. This section must not contradict the existing Prerequisites/troubleshooting content that depends on options-scope `allow-transfer`.
- **Extend the existing compatibility reference** (`docs/configuration/named-conf.md` + `.zh.md`, the "named.conf Compatibility" page): document the tiered-tolerance contract (silent/INFO/WARN/fail-closed), the fail-closed doctrine (unevaluable access control never fails open), and the supported `acl` / `match-clients` element forms and built-in ACLs delivered by B.
- **Feature/comparison updates**: note BIND drop-in compatibility in `README.md` features and the `docs/index.md` + `.zh.md` comparison table.
- **New deb example**: ship a self-contained Debian-style viewless example (`packaging/named.conf.viewless.example`) demonstrating the viewless layout (top-level `type master` zones with an `options` block), with a commented note pointing to the migration guide for BIND default-zones compatibility; add the corresponding `nfpm.yaml` install entry; add a `deb-packaging` spec scenario.

## Non-Goals

- Any runtime or parser behavior change — delivered by A and B; this change is documentation and packaging only.
- Reshaping the existing `packaging/named.conf.example` into the Debian three-file split — that is owned by the parked `debian-named-conf-layout` change. This change only ADDS a new `named.conf.viewless.example` and appends an `nfpm.yaml` entry; the only shared file is `nfpm.yaml` (append-only, see cross-change note in the plan).
- New manual pages or nav entries — the migration and compatibility content extends the two existing pages, so `mkdocs.yml` nav is unchanged.
- Running perf-guard — skipped: every touched file is documentation or packaging metadata, none compiled into the shipped binary.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `deb-packaging`: the `.deb` additionally installs a viewless BIND-style example configuration file under `/etc/shadowdns/`.

## Impact

- Affected specs: deb-packaging
- Affected code:
  - New:
    - packaging/named.conf.viewless.example
  - Modified:
    - docs/migration.md
    - docs/migration.zh.md
    - docs/configuration/named-conf.md
    - docs/configuration/named-conf.zh.md
    - docs/index.md
    - docs/index.zh.md
    - README.md
    - nfpm.yaml
  - Removed:
    - (none)
