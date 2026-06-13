## ADDED Requirements

### Requirement: Parse and store named acl definitions

The config-loader SHALL parse top-level `acl "<name>" { <address-match-list>; };` blocks and store each definition in a named registry on the loaded configuration, keyed by acl name. The `<address-match-list>` SHALL be parsed with the same element parser used for `match-clients` (see Parse match-clients rule syntax), producing an ordered element list. When the same acl name is defined more than once, the last definition SHALL take effect and the loader SHALL log a WARN naming the acl. After all files are loaded, the config-loader SHALL resolve every named reference (in any acl body or any view's `match-clients`) to the element list of the named acl; a reference to an undefined name SHALL be dropped and treated as never-matching (fail-closed) with a WARN, and a reference cycle SHALL be broken with a WARN.

#### Scenario: acl definition is stored and referenced

- **WHEN** `named.conf` contains `acl "internal" { 192.0.2.0/24; };` and a `view` with `match-clients { internal; };`
- **THEN** the loader stores the `internal` acl AND resolves the view's `match-clients` reference to the `192.0.2.0/24` element

#### Scenario: Duplicate acl name keeps the last definition

- **WHEN** `named.conf` defines `acl "x"` twice with different contents
- **THEN** the loader keeps the last definition AND logs a WARN naming `x`

#### Scenario: Reference to undefined acl is dropped fail-closed

- **WHEN** a `match-clients` references `nosuchacl;` and no `acl "nosuchacl"` is defined
- **THEN** the loader drops that element AND logs a WARN AND the element never matches any client

#### Scenario: Reference cycle is broken

- **WHEN** `acl "a" { b; };` and `acl "b" { a; };` are both defined
- **THEN** the loader breaks the cycle AND logs a WARN AND does not recurse without bound

## MODIFIED Requirements

### Requirement: Parse match-clients rule syntax

The config-loader SHALL recognize the following element forms inside `match-clients { ... };` and inside any `acl` body, producing an ordered address-match-list:

- `geoip country <ISO-2>;` — country code match
- `geoip asnum "AS<number> <description>";` — AS number extracted from the leading numeric portion, description ignored
- `<IPv4-address>;` — single IPv4 address
- `<IPv4-prefix>/<bits>;` — CIDR prefix
- `any;` — catch-all (always matches); `none;` — never matches
- `localhost;` — the server's own addresses; `localnets;` — the networks directly attached to the server's interfaces
- `<acl-name>;` — a reference to a named acl, resolved to that acl's element list
- `{ <address-match-list> };` — a nested group evaluated as its own ordered list
- a leading `!` on any of the above — negation; when a negated element matches, the enclosing list rejects (see view-matcher)

The loader SHALL accept elements written either one per line or as multiple elements on the same line separated by `;`.

A token that does not resolve to any recognized element form — including a bare word that is neither `any`, `none`, `localhost`, `localnets`, a `geoip` form, an IP, a CIDR prefix, nor a defined acl name — SHALL be dropped rather than causing a fatal error. The loader SHALL log a WARN entry naming the dropped token and the enclosing view (or acl), and the dropped element SHALL be treated as never-matching (fail-closed). A malformed instance of a recognized form (for example `geoip asnum` whose value carries no leading AS number) SHALL remain a fatal error.

#### Scenario: Country code rule is recognized

- **WHEN** a rule reads `geoip country TH;`
- **THEN** the loader produces an element with type=country and value="TH"

#### Scenario: ASN rule extracts numeric AS

- **WHEN** a rule reads `geoip asnum "AS4134 Chinanet";`
- **THEN** the loader produces an element with type=asn and value=4134

#### Scenario: ASN rule with unparseable description fails loudly

- **WHEN** a rule reads `geoip asnum "Chinanet";` (no leading AS number)
- **THEN** the loader returns an error citing the file path and line number

#### Scenario: CIDR and single-IP rules are distinguished

- **WHEN** rules include `192.0.2.8;` and `198.51.100.0/26;`
- **THEN** the loader produces an IP element for the first and a CIDR element with prefix length 26 for the second

#### Scenario: Negated and nested elements are parsed

- **WHEN** a `match-clients` block reads `! 192.0.2.0/24; { 198.51.100.0/24; 203.0.113.0/24; }; any;`
- **THEN** the loader produces a negated CIDR element, a nested group of two CIDR elements, and an `any` element, in that order

#### Scenario: Built-in acl names are recognized

- **WHEN** a `match-clients` block reads `localhost;` or `localnets;` or `none;`
- **THEN** the loader produces the corresponding built-in element rather than dropping it as unknown

#### Scenario: Named reference resolves to its acl element list

- **WHEN** `acl "internal" { 10.0.0.0/8; };` is defined and a `match-clients` reads `internal;`
- **THEN** the loader produces a reference element resolved to the `internal` acl's element list
