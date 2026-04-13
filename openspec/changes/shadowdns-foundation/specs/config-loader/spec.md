## ADDED Requirements

### Requirement: Parse named.conf options block

The config-loader SHALL parse the `options { ... }` block of `named.conf` and extract at minimum the following fields: `directory`, `geoip-directory`, `listen-on`, `listen-on-v6`, `allow-transfer`, `recursion`, `minimal-responses`, `version`, `hostname`, `transfer-format`. Unknown options SHALL be ignored with a warning log entry rather than causing a parse failure.

#### Scenario: Standard options block loads successfully

- **WHEN** `named.conf` contains `options { directory "/etc/namedb"; geoip-directory "/usr/local/share/GeoIP/"; listen-on { any; }; recursion no; };`
- **THEN** the loader produces an options record with `directory="/etc/namedb"`, `geoipDirectory="/usr/local/share/GeoIP/"`, `listenOn=[any]`, `recursion=false`

#### Scenario: Unknown option emits warning but does not fail

- **WHEN** `named.conf` contains an option key that is not in the supported list
- **THEN** the loader logs a warning including the option name and line number AND continues parsing

#### Scenario: Malformed options block fails with actionable error

- **WHEN** `named.conf` has an unmatched `{` or missing `;` inside the options block
- **THEN** the loader returns an error that includes the file path and the line number of the first unparseable token

### Requirement: Parse view and zone declarations from master.zones

The config-loader SHALL parse `view "<name>" { match-clients { ... }; zone "<domain>" { type master; file "<path>"; }; ... };` blocks from any file included by `named.conf` (e.g., `master.zones`). For each view, it SHALL preserve the declaration order of `match-clients` rules and of the zones within that view.

#### Scenario: Multiple views with ordered rules

- **WHEN** `master.zones` declares `view "view-th"` before `view "view-other"` where `view-other` has `match-clients { any; }`
- **THEN** the loader returns views in that exact order AND the rules within each view are preserved in declaration order

#### Scenario: Zone file path is resolved relative to options.directory

- **WHEN** a zone declares `file "master/group-a/example.com_view-th.fwd"` and options.directory is `/etc/namedb`
- **THEN** the loader resolves the absolute path as `/etc/namedb/master/group-a/example.com_view-th.fwd`

#### Scenario: Same zone name across different views produces independent entries

- **WHEN** both `view "view-th"` and `view "view-other"` declare a zone `"example.com"` with different file paths
- **THEN** the loader returns two separate zone entries, one per view, each with its own file path

### Requirement: Parse match-clients rule syntax

The config-loader SHALL recognize the following rule forms inside `match-clients { ... };`:

- `geoip country <ISO-2>;` — country code match
- `geoip asnum "AS<number> <description>";` — AS number extracted from the leading numeric portion, description ignored
- `<IPv4-address>;` — single IPv4 address
- `<IPv4-prefix>/<bits>;` — CIDR prefix
- `any;` — catch-all

The loader SHALL accept rules written either one per line or as multiple rules on the same line separated by `;`.

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

### Requirement: Warn when non-last view uses `any`

The config-loader SHALL emit a warning when a view declares `match-clients { any; }` but is not the last view in declaration order, because subsequent views would be shadowed.

#### Scenario: `any` view in middle triggers warning

- **WHEN** view order is `view-th`, `view-other` (with `any`), `view-eu`
- **THEN** the loader logs a warning identifying `view-other` as shadowing `view-eu` AND continues loading

### Requirement: Parse aliases.yaml

The config-loader SHALL parse an `aliases.yaml` file that declares root-to-backup domain mappings. The file path SHALL be configurable via command-line flag. The YAML structure SHALL be a top-level mapping from root domain name to a list of backup domain names.

#### Scenario: Well-formed aliases.yaml produces alias map

- **WHEN** `aliases.yaml` contains `root.com: [backup.com, mirror.com]`
- **THEN** the loader produces a map `{backup.com → root.com, mirror.com → root.com}`

#### Scenario: Backup appearing under two roots is rejected

- **WHEN** `aliases.yaml` declares the same backup domain under two different roots
- **THEN** the loader returns an error citing both root entries

#### Scenario: Backup domain equal to root domain is rejected

- **WHEN** `aliases.yaml` lists `root.com` as a backup of itself
- **THEN** the loader returns an error

#### Scenario: Missing aliases.yaml is tolerated

- **WHEN** the `--aliases` flag is not provided or the file does not exist
- **THEN** the loader returns an empty alias map AND logs an info message; the server still starts normally

### Requirement: Reject unsupported named.conf directives at startup

The config-loader SHALL reject directives that would change DNS behavior in ways ShadowDNS does not implement (e.g., `dnssec-enable yes;`, `allow-update { ... };`, `zone { type forward; };`, `zone { type slave; };`) by returning a fatal error that names the unsupported directive and its location.

#### Scenario: Unsupported zone type fails startup

- **WHEN** any zone declares `type slave;` or `type forward;`
- **THEN** the loader returns a fatal error naming the zone and the unsupported type
