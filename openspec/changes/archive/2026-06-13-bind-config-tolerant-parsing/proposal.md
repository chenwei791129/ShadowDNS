## Why

ShadowDNS aims to be a drop-in replacement for BIND: an operator migrates by pointing `--named-conf` at an existing `/etc/bind/named.conf` (with its `include` of `named.conf.options`, `named.conf.local`, `named.conf.default-zones`). Today the named.conf parser uses a whitelist posture — unknown top-level directives (`acl`, `key`, `controls`, `server`, `statistics-channels`), unknown view-scope directives (`allow-query`, `allow-recursion`), and any non-`master` zone type (`hint`, `forward`, `stub`, `slave`) each produce a fatal startup error. Real BIND configurations routinely contain these constructs, so a typical BIND config fails to load and migration is blocked.

This change is the first of three on the BIND-compatibility roadmap (this change, then `bind-named-acl-match-clients`, then `bind-migration-docs-examples`).

## What Changes

Flip the named.conf parser from "whitelist-known, fatal-on-unknown" to a tolerant "skip-unknown" posture at every scope, so any syntactically valid BIND named.conf loads without a fatal error. Only genuine syntax errors (unbalanced braces, missing semicolons) remain fatal.

- **Top level**: unknown directives — including `acl`, `key`, `controls`, `server`, `statistics-channels`, `trusted-keys`, `managed-keys` — are skipped instead of fatal. A skip helper handles the `keyword [name|IP] { ... };` and `keyword value;` shapes, reusing the existing balanced-brace skipper.
- **View scope**: unknown view directives (e.g. `allow-query`, `allow-recursion`) are skipped instead of fatal.
- **Zone type**: a zone whose `type` is not `master` (`hint`, `forward`, `stub`, `slave`, `secondary`, `redirect`, `mirror`, etc.) is skipped — the zone is dropped and never served — instead of producing the unsupported-type fatal error. Existing tolerance of unknown zone-body directives is retained.
- **match-clients**: a rule ShadowDNS cannot evaluate (a named-acl reference, `!` negation, a nested list) is dropped and treated **fail-closed** (it never matches), instead of being a fatal parse error.

Two cross-cutting policies accompany the posture flip:

- **Tiered logging**: skipped access-control directives (`allow-query`, `allow-recursion`, `allow-transfer`, `allow-update`, `allow-notify`, `blackhole`) are logged at WARN ("ShadowDNS does not enforce this ACL"); recursion-family directives (`recursion`, `forwarders`, `dnssec-validation`) and skipped zone types are logged at INFO (expected, avoids WARN noise on blessed configs); other unknown directives are silent or DEBUG.
- **Fail-closed safety doctrine**: when ShadowDNS cannot evaluate an access-control construct it never fails open. An unevaluable `match-clients` rule never matches; if a view's entire `match-clients` set becomes unevaluable, that view matches no clients (serves nothing) rather than serving its zones to every client.

## Non-Goals

- Parsing `acl` block contents or resolving named-acl references inside `match-clients` — deferred to change `bind-named-acl-match-clients`. Until then, a `match-clients` that references a named acl is fail-closed (the view serves nothing), which is the deliberate safe-side choice.
- Adopting BIND's ordered first-match address-match-list semantics with `!` negation — deferred to `bind-named-acl-match-clients`.
- Changing how the named.conf path is located — the existing required `--named-conf` flag already accepts `/etc/bind/named.conf`, and `include` directives with absolute paths are already followed.
- Writing the migration guide or deb example variants — deferred to `bind-migration-docs-examples`.
- Enforcing `allow-query` / `allow-transfer` ACLs — this change only WARNs that they are ignored; it does not implement BIND ACL enforcement.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `config-loader`: top-level and view-scope directive parsing changes from whitelist+fatal to skip-unknown; non-`master` zone types are skipped instead of fatal; tiered logging classifies skipped directives.
- `view-matcher`: an unevaluable `match-clients` rule is dropped and treated as never-matching (fail-closed) instead of producing a fatal config error.

## Impact

- Affected specs: config-loader, view-matcher
- Affected code:
  - Modified:
    - internal/config/zones.go
    - internal/config/match.go
    - internal/config/zones_test.go
    - internal/config/match_test.go
    - internal/view/matcher_test.go
    - test/integration/helpers_test.go
  - New:
    - test/integration/bind_compat_test.go
    - testdata/integration/bindcompat/named.conf
    - testdata/integration/bindcompat/named.conf.options
    - testdata/integration/bindcompat/named.conf.local
    - testdata/integration/bindcompat/named.conf.default-zones
    - testdata/integration/bindcompat/db.local
    - testdata/integration/bindcompat/db.127
    - testdata/integration/bindcompat/db.0
    - testdata/integration/bindcompat/db.255
    - testdata/integration/bindcompat/README.md
  - Removed:
    - (none)
