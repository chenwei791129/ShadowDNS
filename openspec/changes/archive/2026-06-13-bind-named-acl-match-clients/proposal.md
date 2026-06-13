## Why

This is the second change on the BIND-compatibility roadmap (after `bind-config-tolerant-parsing`, before `bind-migration-docs-examples`). Change A made any valid BIND named.conf load without a fatal error, but it deliberately stopped short of understanding `acl` definitions and the richer `match-clients` grammar: top-level `acl` blocks are skipped, and a `match-clients` rule that references a named acl, uses `!` negation, or nests a `{ ... }` group is dropped and treated fail-closed (the view serves nothing).

Real-world BIND split-horizon configurations almost always express their client groups with named ACLs (`acl "internal" { 10.0.0.0/8; ... }; view "internal" { match-clients { internal; }; ... }`) and negation (`match-clients { ! 192.0.2.0/24; any; }`). Under Change A those views serve nothing — a config that loads but does not function. To make ShadowDNS a true drop-in replacement, the view-matcher must actually understand named ACLs, negation, nested groups, and the BIND built-in ACLs, evaluating them with BIND's ordered address-match-list semantics.

## What Changes

- **Top-level `acl` is parsed and stored** (instead of skipped): `acl "<name>" { <address-match-list>; };` is parsed into a named registry on the loaded config. The `<address-match-list>` accepts the same element forms as `match-clients`.
- **`match-clients` grammar is extended** to the full BIND address-match-list element set: a named-acl reference (a bare word resolving to a defined `acl`), a `!` negation prefix on any element, a nested `{ ... }` group, and the built-in ACLs `any`, `none`, `localhost`, `localnets` — in addition to the existing `geoip`, IP, CIDR, and `any` forms.
- **Ordered first-match + negation evaluation**: within a view's `match-clients` (and within any acl), elements are evaluated in declaration order; the first element that matches the client decides the outcome — a positive element selects the view, a negated element that matches rejects the view (evaluation falls through to the next view). This replaces the prior "any positive rule matches" behavior, which had no way to express negation.
- **Named-acl references resolve recursively**: a reference is evaluated against the referenced acl's own ordered address-match-list; negation may be applied to a reference or nested group as a whole.
- **fail-closed retained for undefined references**: a `match-clients` (or acl) element referencing a name with no `acl` definition is dropped and treated as never-matching (fail-closed), logged at WARN — the same safe-side rule established in Change A.

## Non-Goals

- Tolerant skip-unknown parsing posture, non-master zone-type skip, and the fail-closed-on-unevaluable doctrine — delivered by `bind-config-tolerant-parsing` (this change builds on it).
- Enforcing `allow-query` / `allow-transfer` ACLs — still only WARNed as ignored (those are access control on operations, not view selection; out of scope here).
- TSIG `key` elements inside an address-match-list — parsed-and-skipped, not evaluated.
- Migration guide and deb examples — deferred to `bind-migration-docs-examples`.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `config-loader`: top-level `acl` blocks are parsed into a named registry instead of skipped; `match-clients` rule syntax is extended to named references, `!` negation, nested groups, and built-in ACLs.
- `view-matcher`: address-match-list evaluation becomes ordered first-match with negation (positive selects, negated-match rejects); named references resolve recursively against their acl; built-in ACLs `any`/`none`/`localhost`/`localnets` are evaluated; undefined references remain fail-closed.

## Impact

- Affected specs: config-loader, view-matcher
- Affected code:
  - Modified:
    - internal/config/match.go
    - internal/config/zones.go
    - internal/view/matcher.go
    - internal/view/netmatch.go (localhost/localnets interface enumeration + prefix-set helper)
    - internal/server/build.go
    - internal/config/match_test.go
    - internal/config/zones_test.go
    - internal/view/matcher_test.go
    - test/integration/bind_compat_test.go
    - internal/server/build_test.go (MatchClients type change: []MatchRule → []Element)
    - internal/server/handler_ecs_test.go (NamedRuleSet.Rules type change)
    - internal/server/server_test.go (NamedRuleSet.Rules type change)
    - test/integration/alias_rdata_rewrite_test.go (NamedRuleSet.Rules type change)
  - New:
    - (none)
  - Removed:
    - (none)
