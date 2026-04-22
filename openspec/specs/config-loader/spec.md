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


<!-- @trace
source: shadowdns-foundation
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - testdata/integration/named.conf
  - testdata/integration/master/example.com_view-th.fwd
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/master.zones
  - internal/alias/override.go
  - internal/server/listener.go
  - internal/transfer/notify.go
  - internal/view/matcher.go
  - internal/view/netmatch.go
  - internal/transfer/axfr.go
  - Makefile
  - README.md
  - internal/view/loader.go
  - internal/config/match.go
  - testdata/integration/README.md
  - internal/dnsutil/dnsutil.go
  - internal/zone/parser.go
  - internal/transfer/acl.go
  - testdata/integration/master/example.com_view-other.fwd
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/master/backup.example_view-th.fwd
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/master/backup.example_view-other.fwd
tests:
  - internal/view/testhelper_test.go
  - internal/view/geoip_country_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/zone/zone_test.go
  - internal/transfer/axfr_test.go
  - test/integration/backup_test.go
  - internal/view/netmatch_test.go
  - internal/view/geoip_asn_test.go
  - test/integration/query_test.go
  - internal/config/options_test.go
  - internal/view/loader_test.go
  - internal/zone/parser_test.go
  - internal/zone/classify_test.go
  - internal/config/aliases_test.go
  - internal/alias/rewrite_test.go
  - test/integration/negative_test.go
  - internal/alias/detect_test.go
  - internal/alias/override_test.go
  - internal/server/server_test.go
  - internal/view/matcher_test.go
  - test/integration/axfr_test.go
  - internal/config/zones_test.go
  - test/integration/helpers_test.go
  - internal/config/match_test.go
  - internal/transfer/acl_test.go
  - cmd/shadowdns/main_test.go
  - internal/alias/soa_test.go
  - internal/transfer/notify_test.go
-->


<!-- @trace
source: notify-toggle
updated: 2026-04-15
code:
  - internal/config/options.go
  - CHANGELOG.md
  - README.md
  - cmd/shadowdns/main.go
  - packaging/named.conf.example
  - .release-please-manifest.json
tests:
  - internal/config/options_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/notify_test.go
-->

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


<!-- @trace
source: shadowdns-foundation
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - testdata/integration/named.conf
  - testdata/integration/master/example.com_view-th.fwd
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/master.zones
  - internal/alias/override.go
  - internal/server/listener.go
  - internal/transfer/notify.go
  - internal/view/matcher.go
  - internal/view/netmatch.go
  - internal/transfer/axfr.go
  - Makefile
  - README.md
  - internal/view/loader.go
  - internal/config/match.go
  - testdata/integration/README.md
  - internal/dnsutil/dnsutil.go
  - internal/zone/parser.go
  - internal/transfer/acl.go
  - testdata/integration/master/example.com_view-other.fwd
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/master/backup.example_view-th.fwd
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/master/backup.example_view-other.fwd
tests:
  - internal/view/testhelper_test.go
  - internal/view/geoip_country_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/zone/zone_test.go
  - internal/transfer/axfr_test.go
  - test/integration/backup_test.go
  - internal/view/netmatch_test.go
  - internal/view/geoip_asn_test.go
  - test/integration/query_test.go
  - internal/config/options_test.go
  - internal/view/loader_test.go
  - internal/zone/parser_test.go
  - internal/zone/classify_test.go
  - internal/config/aliases_test.go
  - internal/alias/rewrite_test.go
  - test/integration/negative_test.go
  - internal/alias/detect_test.go
  - internal/alias/override_test.go
  - internal/server/server_test.go
  - internal/view/matcher_test.go
  - test/integration/axfr_test.go
  - internal/config/zones_test.go
  - test/integration/helpers_test.go
  - internal/config/match_test.go
  - internal/transfer/acl_test.go
  - cmd/shadowdns/main_test.go
  - internal/alias/soa_test.go
  - internal/transfer/notify_test.go
-->

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


<!-- @trace
source: shadowdns-foundation
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - testdata/integration/named.conf
  - testdata/integration/master/example.com_view-th.fwd
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/master.zones
  - internal/alias/override.go
  - internal/server/listener.go
  - internal/transfer/notify.go
  - internal/view/matcher.go
  - internal/view/netmatch.go
  - internal/transfer/axfr.go
  - Makefile
  - README.md
  - internal/view/loader.go
  - internal/config/match.go
  - testdata/integration/README.md
  - internal/dnsutil/dnsutil.go
  - internal/zone/parser.go
  - internal/transfer/acl.go
  - testdata/integration/master/example.com_view-other.fwd
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/master/backup.example_view-th.fwd
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/master/backup.example_view-other.fwd
tests:
  - internal/view/testhelper_test.go
  - internal/view/geoip_country_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/zone/zone_test.go
  - internal/transfer/axfr_test.go
  - test/integration/backup_test.go
  - internal/view/netmatch_test.go
  - internal/view/geoip_asn_test.go
  - test/integration/query_test.go
  - internal/config/options_test.go
  - internal/view/loader_test.go
  - internal/zone/parser_test.go
  - internal/zone/classify_test.go
  - internal/config/aliases_test.go
  - internal/alias/rewrite_test.go
  - test/integration/negative_test.go
  - internal/alias/detect_test.go
  - internal/alias/override_test.go
  - internal/server/server_test.go
  - internal/view/matcher_test.go
  - test/integration/axfr_test.go
  - internal/config/zones_test.go
  - test/integration/helpers_test.go
  - internal/config/match_test.go
  - internal/transfer/acl_test.go
  - cmd/shadowdns/main_test.go
  - internal/alias/soa_test.go
  - internal/transfer/notify_test.go
-->

### Requirement: Warn when non-last view uses `any`

The config-loader SHALL emit a warning when a view declares `match-clients { any; }` but is not the last view in declaration order, because subsequent views would be shadowed.

#### Scenario: `any` view in middle triggers warning

- **WHEN** view order is `view-th`, `view-other` (with `any`), `view-eu`
- **THEN** the loader logs a warning identifying `view-other` as shadowing `view-eu` AND continues loading


<!-- @trace
source: shadowdns-foundation
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - testdata/integration/named.conf
  - testdata/integration/master/example.com_view-th.fwd
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/master.zones
  - internal/alias/override.go
  - internal/server/listener.go
  - internal/transfer/notify.go
  - internal/view/matcher.go
  - internal/view/netmatch.go
  - internal/transfer/axfr.go
  - Makefile
  - README.md
  - internal/view/loader.go
  - internal/config/match.go
  - testdata/integration/README.md
  - internal/dnsutil/dnsutil.go
  - internal/zone/parser.go
  - internal/transfer/acl.go
  - testdata/integration/master/example.com_view-other.fwd
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/master/backup.example_view-th.fwd
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/master/backup.example_view-other.fwd
tests:
  - internal/view/testhelper_test.go
  - internal/view/geoip_country_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/zone/zone_test.go
  - internal/transfer/axfr_test.go
  - test/integration/backup_test.go
  - internal/view/netmatch_test.go
  - internal/view/geoip_asn_test.go
  - test/integration/query_test.go
  - internal/config/options_test.go
  - internal/view/loader_test.go
  - internal/zone/parser_test.go
  - internal/zone/classify_test.go
  - internal/config/aliases_test.go
  - internal/alias/rewrite_test.go
  - test/integration/negative_test.go
  - internal/alias/detect_test.go
  - internal/alias/override_test.go
  - internal/server/server_test.go
  - internal/view/matcher_test.go
  - test/integration/axfr_test.go
  - internal/config/zones_test.go
  - test/integration/helpers_test.go
  - internal/config/match_test.go
  - internal/transfer/acl_test.go
  - cmd/shadowdns/main_test.go
  - internal/alias/soa_test.go
  - internal/transfer/notify_test.go
-->

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

- **WHEN** the `-aliases` flag is not provided or the file does not exist
- **THEN** the loader returns an empty alias map AND logs an info message; the server still starts normally


<!-- @trace
source: shadowdns-foundation
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - testdata/integration/named.conf
  - testdata/integration/master/example.com_view-th.fwd
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/master.zones
  - internal/alias/override.go
  - internal/server/listener.go
  - internal/transfer/notify.go
  - internal/view/matcher.go
  - internal/view/netmatch.go
  - internal/transfer/axfr.go
  - Makefile
  - README.md
  - internal/view/loader.go
  - internal/config/match.go
  - testdata/integration/README.md
  - internal/dnsutil/dnsutil.go
  - internal/zone/parser.go
  - internal/transfer/acl.go
  - testdata/integration/master/example.com_view-other.fwd
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/master/backup.example_view-th.fwd
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/master/backup.example_view-other.fwd
tests:
  - internal/view/testhelper_test.go
  - internal/view/geoip_country_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/zone/zone_test.go
  - internal/transfer/axfr_test.go
  - test/integration/backup_test.go
  - internal/view/netmatch_test.go
  - internal/view/geoip_asn_test.go
  - test/integration/query_test.go
  - internal/config/options_test.go
  - internal/view/loader_test.go
  - internal/zone/parser_test.go
  - internal/zone/classify_test.go
  - internal/config/aliases_test.go
  - internal/alias/rewrite_test.go
  - test/integration/negative_test.go
  - internal/alias/detect_test.go
  - internal/alias/override_test.go
  - internal/server/server_test.go
  - internal/view/matcher_test.go
  - test/integration/axfr_test.go
  - internal/config/zones_test.go
  - test/integration/helpers_test.go
  - internal/config/match_test.go
  - internal/transfer/acl_test.go
  - cmd/shadowdns/main_test.go
  - internal/alias/soa_test.go
  - internal/transfer/notify_test.go
-->


<!-- @trace
source: ephemeral-txt-api
updated: 2026-04-22
code:
  - docs/ephemeral-api.md
  - go.sum
  - .release-please-manifest.json
  - cmd/shadowdns/main.go
  - internal/transfer/notify.go
  - internal/config/zones.go
  - Makefile
  - scripts/smoke.sh
  - internal/ephemeral/store.go
  - go.mod
  - docs/benchmark.md
  - scripts/gen-container-testdata.go
  - testdata/integration/master/example.com_view-other.fwd
  - internal/server/server.go
  - internal/server/listener.go
  - cmd/shadowdns/pprof.go
  - internal/view/loader.go
  - internal/shadowdnscfg/config.go
  - internal/zone/parser.go
  - internal/server/handler.go
  - internal/alias/override.go
  - .github/workflows/release-please.yml
  - CLAUDE.md
  - internal/server/listenaddr.go
  - internal/zone/classify.go
  - CHANGELOG.md
  - testdata/integration/master/example.com_view-th.fwd
  - cmd/shadowdns/reload.go
  - internal/transfer/axfr.go
  - internal/zone/zone.go
  - internal/config/options.go
  - packaging/shadowdns.service
  - internal/api/server.go
  - packaging/shadowdns.yaml.example
  - packaging/aliases.yaml.example
  - packaging/named.conf.example
  - internal/server/build.go
  - internal/config/aliases.go
  - scripts/test-deb.sh
  - nfpm.yaml
  - internal/server/fingerprint.go
  - internal/logging/logger.go
  - docs/migration.md
  - README.md
tests:
  - cmd/shadowdns/main_ephemeral_test.go
  - test/integration/notify_test.go
  - internal/server/server_test.go
  - test/integration/negative_test.go
  - internal/transfer/axfr_test.go
  - internal/ephemeral/store_test.go
  - internal/zone/classify_test.go
  - internal/zone/parser_test.go
  - internal/config/aliases_test.go
  - cmd/shadowdns/listenon_test.go
  - cmd/shadowdns/pprof_test.go
  - cmd/shadowdns/main_test.go
  - internal/api/server_test.go
  - internal/config/zones_test.go
  - internal/server/fingerprint_test.go
  - test/integration/axfr_test.go
  - internal/logging/logger_test.go
  - test/integration/helpers_test.go
  - internal/view/loader_test.go
  - test/integration/reload_diff_test.go
  - test/integration/cname_following_test.go
  - internal/shadowdnscfg/config_test.go
  - internal/alias/override_test.go
  - internal/server/handler_ephemeral_test.go
  - internal/zone/zone_test.go
  - internal/transfer/notify_test.go
  - internal/server/listenaddr_test.go
  - internal/server/build_test.go
  - internal/config/options_test.go
  - test/integration/listenon_test.go
  - test/integration/wildcard_test.go
  - test/integration/cname_synthesis_test.go
-->

### Requirement: Reject unsupported named.conf directives at startup

The config-loader SHALL reject directives that would change DNS behavior in ways ShadowDNS does not implement (e.g., `dnssec-enable yes;`, `allow-update { ... };`, `zone { type forward; };`, `zone { type slave; };`) by returning a fatal error that names the unsupported directive and its location.

#### Scenario: Unsupported zone type fails startup

- **WHEN** any zone declares `type slave;` or `type forward;`
- **THEN** the loader returns a fatal error naming the zone and the unsupported type

## Requirements

<!-- @trace
source: shadowdns-foundation
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - testdata/integration/named.conf
  - testdata/integration/master/example.com_view-th.fwd
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/master.zones
  - internal/alias/override.go
  - internal/server/listener.go
  - internal/transfer/notify.go
  - internal/view/matcher.go
  - internal/view/netmatch.go
  - internal/transfer/axfr.go
  - Makefile
  - README.md
  - internal/view/loader.go
  - internal/config/match.go
  - testdata/integration/README.md
  - internal/dnsutil/dnsutil.go
  - internal/zone/parser.go
  - internal/transfer/acl.go
  - testdata/integration/master/example.com_view-other.fwd
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/master/backup.example_view-th.fwd
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/master/backup.example_view-other.fwd
tests:
  - internal/view/testhelper_test.go
  - internal/view/geoip_country_test.go
  - internal/dnsutil/dnsutil_test.go
  - internal/zone/zone_test.go
  - internal/transfer/axfr_test.go
  - test/integration/backup_test.go
  - internal/view/netmatch_test.go
  - internal/view/geoip_asn_test.go
  - test/integration/query_test.go
  - internal/config/options_test.go
  - internal/view/loader_test.go
  - internal/zone/parser_test.go
  - internal/zone/classify_test.go
  - internal/config/aliases_test.go
  - internal/alias/rewrite_test.go
  - test/integration/negative_test.go
  - internal/alias/detect_test.go
  - internal/alias/override_test.go
  - internal/server/server_test.go
  - internal/view/matcher_test.go
  - test/integration/axfr_test.go
  - internal/config/zones_test.go
  - test/integration/helpers_test.go
  - internal/config/match_test.go
  - internal/transfer/acl_test.go
  - cmd/shadowdns/main_test.go
  - internal/alias/soa_test.go
  - internal/transfer/notify_test.go
-->

### Requirement: Parse named.conf options block

The config-loader SHALL parse the `options { ... }` block of `named.conf` and extract at minimum the following fields: `directory`, `geoip-directory`, `listen-on`, `listen-on-v6`, `allow-transfer`, `recursion`, `minimal-responses`, `version`, `hostname`, `transfer-format`, `notify`. Unknown options SHALL be ignored with a warning log entry rather than causing a parse failure.

The `notify` directive SHALL accept exactly two values: `yes` or `no` (case-insensitive). Any other value SHALL produce a parse error that includes the file path and line number. When the `notify` directive is absent from the options block, the parsed options record SHALL indicate "not set" in a form distinguishable from both `yes` and `no` (so that downstream precedence logic can apply a default).

#### Scenario: Standard options block loads successfully

- **WHEN** `named.conf` contains `options { directory "/etc/namedb"; geoip-directory "/usr/local/share/GeoIP/"; listen-on { any; }; recursion no; };`
- **THEN** the loader produces an options record with `directory="/etc/namedb"`, `geoipDirectory="/usr/local/share/GeoIP/"`, `listenOn=[any]`, `recursion=false`

#### Scenario: Unknown option emits warning but does not fail

- **WHEN** `named.conf` contains an option key that is not in the supported list
- **THEN** the loader logs a warning including the option name and line number AND continues parsing

#### Scenario: Malformed options block fails with actionable error

- **WHEN** `named.conf` has an unmatched `{` or missing `;` inside the options block
- **THEN** the loader returns an error that includes the file path and the line number of the first unparseable token

#### Scenario: notify yes parses to enabled state

- **WHEN** `named.conf` contains `options { notify yes; };`
- **THEN** the loader produces an options record whose `notify` field indicates "set to true"

#### Scenario: notify no parses to disabled state

- **WHEN** `named.conf` contains `options { notify no; };`
- **THEN** the loader produces an options record whose `notify` field indicates "set to false"

#### Scenario: notify absent parses to not-set state

- **WHEN** `named.conf` options block omits the `notify` directive
- **THEN** the loader produces an options record whose `notify` field indicates "not set" (distinguishable from both true and false)

#### Scenario: invalid notify value fails with actionable error

- **WHEN** `named.conf` contains `options { notify bogus; };`
- **THEN** the loader returns an error that includes the file path, the line number, and the invalid value

---
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

---
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

---
### Requirement: Warn when non-last view uses `any`

The config-loader SHALL emit a warning when a view declares `match-clients { any; }` but is not the last view in declaration order, because subsequent views would be shadowed.

#### Scenario: `any` view in middle triggers warning

- **WHEN** view order is `view-th`, `view-other` (with `any`), `view-eu`
- **THEN** the loader logs a warning identifying `view-other` as shadowing `view-eu` AND continues loading

---
### Requirement: Parse aliases.yaml

The config-loader SHALL obtain the root-to-backup alias map from the `aliases` section of the unified ShadowDNS YAML configuration file loaded via the `shadowdns-config` capability, not from a standalone `aliases.yaml` file. The `--aliases` CLI flag SHALL NOT be accepted: because the flag is not registered in the cobra command, passing `--aliases` SHALL cause the server binary to fail to start with cobra's standard `unknown flag: --aliases` error. The alias-map data shape and the duplicate/self-alias rejection rules SHALL remain unchanged; only the source file and loader entry point move.

#### Scenario: Well-formed aliases section produces alias map

- **WHEN** the unified config file contains `aliases: {backup.com: root.com, mirror.com: root.com}`
- **THEN** the loader produces a map `{backup.com → root.com, mirror.com → root.com}`

#### Scenario: Backup appearing under two roots is rejected

- **WHEN** the `aliases` section declares the same backup domain under two different roots
- **THEN** the loader returns an error citing both root entries

#### Scenario: Backup domain equal to root domain is rejected

- **WHEN** the `aliases` section lists `root.com: root.com`
- **THEN** the loader returns an error

#### Scenario: Missing aliases section yields empty map

- **WHEN** the unified config file omits the `aliases` key
- **THEN** the loader returns an empty alias map AND logs an info message; the server still starts normally

#### Scenario: `--aliases` flag is rejected

- **WHEN** the server is started with `--aliases /etc/shadowdns/aliases.yaml`
- **THEN** the server SHALL fail to start with cobra's `unknown flag: --aliases` error; operators are expected to provide aliases via `--config` instead

---
### Requirement: Reject unsupported named.conf directives at startup

The config-loader SHALL reject directives that would change DNS behavior in ways ShadowDNS does not implement (e.g., `dnssec-enable yes;`, `allow-update { ... };`, `zone { type forward; };`, `zone { type slave; };`) by returning a fatal error that names the unsupported directive and its location.

#### Scenario: Unsupported zone type fails startup

- **WHEN** any zone declares `type slave;` or `type forward;`
- **THEN** the loader returns a fatal error naming the zone and the unsupported type