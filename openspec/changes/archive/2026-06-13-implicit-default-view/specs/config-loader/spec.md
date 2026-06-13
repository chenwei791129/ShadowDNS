## MODIFIED Requirements

### Requirement: Parse view and zone declarations from master.zones

The config-loader SHALL parse `view "<name>" { match-clients { ... }; zone "<domain>" { type master; file "<path>"; }; ... };` blocks from any file included by `named.conf` (e.g., `master.zones`). For each view, it SHALL preserve the declaration order of `match-clients` rules and of the zones within that view. The config-loader SHALL also accept `zone "<domain>" { type master; file "<path>"; };` blocks declared at the top level (outside any view block) of `named.conf` or any included file, applying the same zone-body rules as zones inside views: a declared `type` other than `master` SHALL fail with the existing unsupported-type error; relative `file` paths SHALL be resolved with the same parse-time semantics as in-view zones (against `options.directory` when the `options` block precedes the zone declaration, otherwise against the directory of the declaring file); and a zone body that omits `type` or `file` SHALL be tolerated exactly as the same omission is tolerated inside a view block. Duplicate zone names among top-level zones SHALL be tolerated identically to duplicate zone names declared within a single view — no new fatal validation is introduced; the implicit-view synthesis additionally logs a warning for top-level duplicates (see the Synthesize requirement).

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

#### Scenario: Top-level zone with unsupported type fails

- **WHEN** a viewless `named.conf` declares a top-level zone with `type slave;`
- **THEN** the loader returns the same unsupported-type fatal error as a zone inside a view declaring `type slave;`

## ADDED Requirements

### Requirement: Synthesize implicit _default view for viewless configurations

When the entire configuration (the root `named.conf` plus every included file) contains zero `view` blocks and at least one top-level zone declaration, the config-loader SHALL synthesize a single view named `_default` whose zone list contains every top-level zone in declaration order across files (with `include` directives expanded in place at their point of occurrence). The synthesized view's match-clients rule set SHALL consist of exactly the same rule value that parsing `match-clients { any; };` produces, and the synthesized view SHALL be a regular view value of the same data shape as a parsed view, so all consumers of the view list process it through unchanged code paths. When the configuration contains zero `view` blocks and zero zone declarations, the config-loader SHALL NOT synthesize a `_default` view and SHALL return an empty view list, preserving the pre-existing behavior for empty configurations. When two or more top-level zone declarations share the same zone name, synthesis SHALL succeed (no fatal error) and the synthesized view's zone list SHALL retain every declared entry in declaration order, but the config-loader SHALL log exactly one warning per duplicated zone name, naming the zone, the source file path and line number of every declaration of that name, and stating that the last declaration takes effect at serving time.

#### Scenario: Viewless configuration is served via the implicit _default view

- **WHEN** `named.conf` contains only an `options { ... };` block and two top-level zones `"example.com"` and `"example.net"` in that order
- **THEN** the loader returns exactly one view named `_default` whose match-clients matches any client IP AND whose zones are `example.com`, `example.net` in declaration order

#### Scenario: Configuration with no views and no zones stays empty

- **WHEN** `named.conf` contains only an `options { ... };` block
- **THEN** the loader succeeds AND returns zero views

#### Scenario: Explicitly declared view named _default is treated as a regular view

- **WHEN** `named.conf` declares `view "_default" { match-clients { any; }; zone "example.com" { type master; file "example.com.fwd"; }; };` and no top-level zones
- **THEN** the loader returns that view unchanged AND no additional view is synthesized

#### Scenario: Duplicate top-level zone names warn once per name without failing

- **WHEN** a viewless `named.conf` declares a top-level zone `"example.com"` on line 5 and again on line 9
- **THEN** the loader succeeds AND the synthesized `_default` view contains both entries in declaration order AND exactly one warning is logged naming `example.com` with both declaration positions (the named.conf path with lines 5 and 9) and stating the last declaration takes effect at serving time

### Requirement: Reject mixing of top-level zones and view blocks

When the configuration contains at least one `view` block and at least one top-level zone declaration — regardless of declaration order and regardless of which file (root or included) each appears in — the config-loader SHALL return a fatal error that names the first top-level zone together with its source file path and line number, where "first" means first in parse order with `include` directives expanded in place (depth-first) at their point of occurrence. The error SHALL state that all zones must be declared inside views when any view is present, and SHALL fail both startup and `--dry-run`; on SIGHUP reload the existing keep-old-config behavior for load failures applies.

#### Scenario: Top-level zone declared before a view fails

- **WHEN** `named.conf` declares a top-level zone `"example.com"` on line 5 and a `view "view-other"` block later in the same file
- **THEN** the loader returns a fatal error naming zone `example.com`, the named.conf path, and line 5

#### Scenario: Mixing across included files fails regardless of order

- **WHEN** `named.conf` includes `master.zones` containing only view blocks AND `named.conf` itself declares a top-level zone after the include directive
- **THEN** the loader returns a fatal error naming that top-level zone with its source file and line, even though the zone was parsed after all views
