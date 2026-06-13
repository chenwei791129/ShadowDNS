## ADDED Requirements

### Requirement: Tolerate unrecognized directives at top level and view scope

The config-loader SHALL skip any directive it does not recognize at the top level of `named.conf` (or any included file) and inside a `view { ... }` block, rather than failing. Only genuine syntax errors — an unbalanced brace, a missing terminating `;`, or an unterminated block — SHALL remain fatal. A skipped directive that opens a `{ ... }` block SHALL have its entire balanced block consumed; a skipped single-value directive SHALL be consumed up to and including its terminating `;`. Some top-level block directives carry a name or address token between the keyword and the opening brace (e.g. `acl "internal" { ... };`, `key "rndc-key" { ... };`, `controls { ... };`, `server 192.0.2.1 { ... };`, `masters myset { ... };`); the loader SHALL consume any such tokens before consuming the block.

The config-loader SHALL classify each skipped directive for logging:

- Access-control directives — `allow-query`, `allow-recursion`, `allow-transfer`, `allow-update`, `allow-notify`, `blackhole` — SHALL be logged at WARN with a message stating that ShadowDNS does not enforce the directive.
- Recursion-related directives — `recursion`, `forwarders`, `dnssec-validation` — SHALL be logged at INFO.
- All other skipped directives SHALL be logged at DEBUG or not logged.

#### Scenario: Top-level acl block is skipped, not fatal

- **WHEN** `named.conf` contains `acl "internal" { 192.0.2.0/24; };` at the top level followed by valid `view` declarations
- **THEN** the loader skips the `acl` block AND loads the views successfully AND does not return a fatal error

#### Scenario: Top-level controls and key blocks are skipped

- **WHEN** `named.conf` contains `key "rndc-key" { algorithm hmac-sha256; secret "..."; };` and `controls { inet 127.0.0.1 allow { localhost; }; };` at the top level
- **THEN** the loader skips both blocks AND continues parsing without a fatal error

#### Scenario: View-scope allow-query is skipped and logged at WARN

- **WHEN** a `view "internal"` block contains `allow-query { any; };` alongside `match-clients` and `zone` declarations
- **THEN** the loader skips the `allow-query` directive AND logs a WARN entry naming `allow-query` AND loads the view's zones

#### Scenario: Recursion directive at top level is skipped and logged at INFO

- **WHEN** `named.conf` or an included file contains a top-level directive in the recursion family that ShadowDNS does not act on
- **THEN** the loader skips it AND logs an INFO entry rather than WARN

#### Scenario: Unbalanced brace remains fatal

- **WHEN** a skipped top-level block has an unbalanced `{` with no matching `}` before end of file
- **THEN** the loader returns a fatal error citing the file path and line number

## MODIFIED Requirements

### Requirement: Parse view and zone declarations from master.zones

The config-loader SHALL parse `view "<name>" { match-clients { ... }; zone "<domain>" { type master; file "<path>"; }; ... };` blocks from any file included by `named.conf` (e.g., `master.zones`). For each view, it SHALL preserve the declaration order of `match-clients` rules and of the zones within that view. The config-loader SHALL also accept `zone "<domain>" { type master; file "<path>"; };` blocks declared at the top level (outside any view block) of `named.conf` or any included file, applying the same zone-body rules as zones inside views: a declared `type` other than `master` SHALL cause that zone to be skipped — dropped from its view (or from the top-level set), not served, and its `file` not opened — and logged at INFO, rather than failing; relative `file` paths SHALL be resolved with the same parse-time semantics as in-view zones (against `options.directory` when the `options` block precedes the zone declaration, otherwise against the directory of the declaring file); and a zone body that omits `type` or `file` SHALL be tolerated exactly as the same omission is tolerated inside a view block. Duplicate zone names among top-level zones SHALL be tolerated identically to duplicate zone names declared within a single view — no new fatal validation is introduced; the implicit-view synthesis additionally logs a warning for top-level duplicates (see the Synthesize requirement).

#### Scenario: Multiple views with ordered rules

- **WHEN** `master.zones` declares `view "view-th"` before `view "view-other"` where `view-other` has `match-clients { any; }`
- **THEN** the loader returns views in that exact order AND the rules within each view are preserved in declaration order

#### Scenario: Zone file path is resolved relative to options.directory

- **WHEN** a zone declares `file "master/group-a/example.com_view-th.fwd"` and options.directory is `/etc/namedb`
- **THEN** the loader resolves the absolute path as `/etc/namedb/master/group-a/example.com_view-th.fwd`

#### Scenario: Same zone name across different views produces independent entries

- **WHEN** both `view "view-th"` and `view "view-other"` declare a zone `"example.com"` with different file paths
- **THEN** the loader returns two separate zone entries, one per view, each with its own file path

#### Scenario: Top-level zone file path resolves like an in-view zone

- **WHEN** a viewless `named.conf` declares an `options` block with `directory "/etc/namedb";` followed by a top-level zone with `file "master/example.com.fwd"`
- **THEN** the loader resolves the zone file path as `/etc/namedb/master/example.com.fwd`

#### Scenario: Top-level zone with unsupported type is skipped, not fatal

- **WHEN** a viewless `named.conf` declares a top-level zone with `type hint;` (for example the root zone in `named.conf.default-zones`) or `type slave;`
- **THEN** the loader skips that zone — it is dropped, not served, and its `file` is not opened — AND logs an INFO entry AND continues loading without a fatal error

#### Scenario: In-view zone with unsupported type is skipped

- **WHEN** a `view` block declares one zone with `type master;` and another with `type forward;`
- **THEN** the loader retains the `master` zone AND skips the `forward` zone AND does not return a fatal error

### Requirement: Parse match-clients rule syntax

The config-loader SHALL recognize the following rule forms inside `match-clients { ... };`:

- `geoip country <ISO-2>;` — country code match
- `geoip asnum "AS<number> <description>";` — AS number extracted from the leading numeric portion, description ignored
- `<IPv4-address>;` — single IPv4 address
- `<IPv4-prefix>/<bits>;` — CIDR prefix
- `any;` — catch-all

The loader SHALL accept rules written either one per line or as multiple rules on the same line separated by `;`.

A token that does not match any recognized rule form — including a named-acl reference (a bare word that is neither `any`, a `geoip` form, an IP, nor a CIDR prefix), a `!` negation prefix, or a nested `{ ... }` group — SHALL be dropped rather than causing a fatal error. The loader SHALL log a WARN entry naming the dropped token and the enclosing view, and the dropped rule SHALL be treated as never-matching by the view-matcher (fail-closed). A malformed instance of a recognized form (for example `geoip asnum` whose value carries no leading AS number) SHALL remain a fatal error, because it is a recognized form written incorrectly rather than an unsupported construct.

#### Scenario: Country code rule is recognized

- **WHEN** a rule reads `geoip country TH;`
- **THEN** the loader produces a rule with type=country and value="TH"

#### Scenario: ASN rule extracts numeric AS

- **WHEN** a rule reads `geoip asnum "AS4134 Chinanet";`
- **THEN** the loader produces a rule with type=asn and value=4134

#### Scenario: ASN rule with unparseable description fails loudly

- **WHEN** a rule reads `geoip asnum "Chinanet";` (no leading AS number)
- **THEN** the loader returns an error citing the file path and line number

#### Scenario: CIDR and single-IP rules are distinguished

- **WHEN** rules include `192.0.2.8;` and `198.51.100.0/26;`
- **THEN** the loader produces an IPRule for the first and a CIDRRule with prefix length 26 for the second

#### Scenario: Multiple rules on a single line

- **WHEN** a line reads `geoip country CN; geoip country HK; geoip country MO;`
- **THEN** the loader produces three separate country rules in left-to-right order

#### Scenario: Named-acl reference is dropped, not fatal

- **WHEN** a `match-clients` block contains `internal-net;` where `internal-net` is a bare word that is neither `any`, a `geoip` form, an IP, nor a CIDR prefix
- **THEN** the loader drops that rule AND logs a WARN entry naming the token and the view AND does not return a fatal error AND the dropped rule never matches any client
