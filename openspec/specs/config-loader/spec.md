## Requirements

<!-- @trace
source: shadowdns-foundation
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - testdata/integration/named.conf
  - testdata/integration/db.example.com-th
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/named.conf.local
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
  - testdata/integration/db.example.com-other
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/db.backup.example-th
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/db.backup.example-other
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

<!-- @trace
source: shadowdns-foundation
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - testdata/integration/named.conf
  - testdata/integration/db.example.com-th
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/named.conf.local
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
  - testdata/integration/db.example.com-other
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/db.backup.example-th
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/db.backup.example-other
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

---
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


<!-- @trace
source: bind-config-tolerant-parsing
updated: 2026-06-13
code:
  - internal/config/match.go
  - testdata/integration/bindcompat/db.0
  - docs/configuration/named-conf.zh.md
  - scripts/smoke.sh
  - testdata/integration/master/cnames/example.com_cname
  - internal/config/options.go
  - testdata/integration/bindcompat/db.127
  - internal/config/zones.go
  - testdata/integration/db.backup.example.overrides
  - testdata/integration/master/example.com_view-other.fwd
  - testdata/integration/cnames/db.example.com.cname
  - testdata/integration/master/backup.example_view-other.fwd
  - docs/getting-started.md
  - testdata/integration/bindcompat/shadowdns.yaml
  - testdata/integration/master/backup.example_overrides
  - testdata/integration/named.conf.local
  - testdata/integration/master.zones
  - docs/migration.md
  - testdata/integration/db.backup.example-th
  - testdata/integration/bindcompat/README.md
  - packaging/named.conf.options.example
  - docs/getting-started.zh.md
  - testdata/integration/bindcompat/named.conf
  - testdata/integration/db.include-test.example
  - testdata/integration/bindcompat/named.conf.local
  - nfpm.yaml
  - testdata/integration/db.example.com-other
  - docs/migration.zh.md
  - docs/configuration/named-conf.md
  - scripts/gen-container-testdata.go
  - scripts/test-deb.sh
  - testdata/integration/db.backup.example-other
  - testdata/integration/bindcompat/named.conf.default-zones
  - testdata/integration/bindcompat/db.255
  - testdata/integration/master/example.com_include.fwd
  - testdata/integration/named.conf
  - testdata/integration/bindcompat/db.local
  - testdata/integration/master/backup.example_view-th.fwd
  - packaging/named.conf.local.example
  - testdata/integration/named.conf.options
  - testdata/integration/README.md
  - testdata/integration/db.example.com-th
  - testdata/integration/bindcompat/named.conf.options
  - packaging/named.conf.example
  - testdata/integration/master/example.com_view-th.fwd
  - README.md
tests:
  - test/integration/bind_compat_test.go
  - internal/prunebackup/lexer_test.go
  - internal/view/matcher_test.go
  - test/integration/listenon_test.go
  - test/integration/query_test.go
  - test/integration/prune_backup_test.go
  - internal/config/match_test.go
  - internal/config/zones_test.go
  - test/integration/helpers_test.go
-->

---
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


<!-- @trace
source: bind-config-tolerant-parsing
updated: 2026-06-13
code:
  - internal/config/match.go
  - testdata/integration/bindcompat/db.0
  - docs/configuration/named-conf.zh.md
  - scripts/smoke.sh
  - testdata/integration/master/cnames/example.com_cname
  - internal/config/options.go
  - testdata/integration/bindcompat/db.127
  - internal/config/zones.go
  - testdata/integration/db.backup.example.overrides
  - testdata/integration/master/example.com_view-other.fwd
  - testdata/integration/cnames/db.example.com.cname
  - testdata/integration/master/backup.example_view-other.fwd
  - docs/getting-started.md
  - testdata/integration/bindcompat/shadowdns.yaml
  - testdata/integration/master/backup.example_overrides
  - testdata/integration/named.conf.local
  - testdata/integration/master.zones
  - docs/migration.md
  - testdata/integration/db.backup.example-th
  - testdata/integration/bindcompat/README.md
  - packaging/named.conf.options.example
  - docs/getting-started.zh.md
  - testdata/integration/bindcompat/named.conf
  - testdata/integration/db.include-test.example
  - testdata/integration/bindcompat/named.conf.local
  - nfpm.yaml
  - testdata/integration/db.example.com-other
  - docs/migration.zh.md
  - docs/configuration/named-conf.md
  - scripts/gen-container-testdata.go
  - scripts/test-deb.sh
  - testdata/integration/db.backup.example-other
  - testdata/integration/bindcompat/named.conf.default-zones
  - testdata/integration/bindcompat/db.255
  - testdata/integration/master/example.com_include.fwd
  - testdata/integration/named.conf
  - testdata/integration/bindcompat/db.local
  - testdata/integration/master/backup.example_view-th.fwd
  - packaging/named.conf.local.example
  - testdata/integration/named.conf.options
  - testdata/integration/README.md
  - testdata/integration/db.example.com-th
  - testdata/integration/bindcompat/named.conf.options
  - packaging/named.conf.example
  - testdata/integration/master/example.com_view-th.fwd
  - README.md
tests:
  - test/integration/bind_compat_test.go
  - internal/prunebackup/lexer_test.go
  - internal/view/matcher_test.go
  - test/integration/listenon_test.go
  - test/integration/query_test.go
  - test/integration/prune_backup_test.go
  - internal/config/match_test.go
  - internal/config/zones_test.go
  - test/integration/helpers_test.go
-->

---
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
  - testdata/integration/db.example.com-th
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/named.conf.local
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
  - testdata/integration/db.example.com-other
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/db.backup.example-th
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/db.backup.example-other
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
source: shadowdns-foundation
updated: 2026-04-14
code:
  - cmd/shadowdns/main.go
  - testdata/integration/named.conf
  - testdata/integration/db.example.com-th
  - internal/view/geoip_asn.go
  - go.mod
  - internal/config/zones.go
  - internal/zone/zone.go
  - internal/config/options.go
  - internal/view/geoip_country.go
  - .spectra.yaml
  - internal/alias/detect.go
  - internal/zone/classify.go
  - testdata/integration/named.conf.local
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
  - testdata/integration/db.example.com-other
  - docs/benchmark.md
  - go.sum
  - testdata/integration/aliases.yaml
  - internal/server/server.go
  - testdata/integration/db.backup.example-th
  - docs/migration.md
  - internal/config/aliases.go
  - scripts/smoke.sh
  - testdata/integration/geoip/.gitkeep
  - internal/server/build.go
  - internal/alias/rewrite.go
  - internal/alias/soa.go
  - internal/server/handler.go
  - testdata/integration/db.backup.example-other
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
  - testdata/integration/db.example.com-other
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
  - testdata/integration/db.example.com-th
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

---
### Requirement: Parse aliases.yaml

The config-loader SHALL obtain the alias map from the `aliases` section of the unified ShadowDNS YAML configuration file loaded via the `shadowdns-config` capability, not from a standalone `aliases.yaml` file. The `aliases` section SHALL use a root-to-backups structure: each top-level key is a root domain, and each value is a list of backup domains for that root. The `--aliases` CLI flag SHALL NOT be accepted: because the flag is not registered in the cobra command, passing `--aliases` SHALL cause the server binary to fail to start with cobra's standard `unknown flag: --aliases` error. The resulting in-memory alias map data shape (backup-to-root) and the duplicate/self-alias rejection rules SHALL remain unchanged; only the YAML surface syntax and the loader entry point differ from the legacy `aliases.yaml` behavior.

#### Scenario: Well-formed aliases section produces alias map

- **WHEN** the unified config file contains `aliases: {root.com: [backup.com, mirror.com]}`
- **THEN** the loader SHALL produce a map `{backup.com → root.com, mirror.com → root.com}`

#### Scenario: Backup appearing under two roots is rejected

- **WHEN** the `aliases` section lists the same backup domain under two different root keys
- **THEN** the loader SHALL return an error citing the duplicate backup and both root entries

#### Scenario: Backup domain equal to root domain is rejected

- **WHEN** the `aliases` section lists `root.com: [root.com]`
- **THEN** the loader SHALL return an error identifying the self-alias

#### Scenario: Missing aliases section yields empty map

- **WHEN** the unified config file omits the `aliases` key
- **THEN** the loader SHALL return an empty alias map AND SHALL log an info message; the server SHALL still start normally

#### Scenario: Legacy one-to-one aliases format is rejected

- **WHEN** the `aliases` section contains a bare-string value such as `backup.com: root.com`
- **THEN** the loader SHALL return a YAML decoding error identifying the type mismatch and SHALL NOT build an alias map

#### Scenario: `--aliases` flag is rejected

- **WHEN** the server is started with `--aliases /etc/shadowdns/aliases.yaml`
- **THEN** the server SHALL fail to start with cobra's `unknown flag: --aliases` error; operators are expected to provide aliases via `--config` instead

<!-- @trace
source: aliases-root-to-backups-schema
updated: 2026-04-22
code:
  - scripts/smoke.sh
  - testdata/integration/README.md
  - internal/server/build.go
  - internal/config/aliases.go
  - .release-please-manifest.json
  - scripts/gen-container-testdata.go
  - docs/benchmark.md
  - testdata/integration/aliases.yaml
  - CHANGELOG.md
  - CLAUDE.md
  - internal/shadowdnscfg/config.go
  - README.md
  - testdata/integration/shadowdns.yaml
  - .spectra.yaml
  - packaging/shadowdns.yaml.example
  - scripts/test-deb.sh
tests:
  - test/integration/reload_diff_test.go
  - cmd/shadowdns/main_ephemeral_test.go
  - internal/config/aliases_test.go
  - internal/shadowdnscfg/config_test.go
  - test/integration/axfr_test.go
  - test/integration/listenon_test.go
  - test/integration/helpers_test.go
-->

---
### Requirement: Reject unsupported named.conf directives at startup

The config-loader SHALL reject directives that would change DNS behavior in ways ShadowDNS does not implement (e.g., `dnssec-enable yes;`, `allow-update { ... };`, `zone { type forward; };`, `zone { type slave; };`) by returning a fatal error that names the unsupported directive and its location.

#### Scenario: Unsupported zone type fails startup

- **WHEN** any zone declares `type slave;` or `type forward;`
- **THEN** the loader returns a fatal error naming the zone and the unsupported type

---
### Requirement: Parse named.conf logging block for query logging

The config-loader SHALL parse the top-level `logging { ... };` block (replacing the previous silent skip) and extract: each `channel <name> { ... }` declaration's `file` path with optional `versions` and `size` parameters, `severity`, `print-time`, `print-category`, and `print-severity`; and the `category queries { ... };` channel list. The result SHALL be exposed on the loaded configuration as an optional query-log record carrying the resolved file path (relative paths joined to the `options { directory }` value, consistent with zone file path resolution), the three print option values, and a flag indicating whether `versions` or `size` were present. Channel parameters and categories other than those listed SHALL be ignored with a warning log entry rather than causing a parse failure. A syntactically malformed `logging{}` block (e.g., unbalanced braces) SHALL produce a fatal error naming the file and line, consistent with `options{}` parsing.

#### Scenario: Production-shaped logging block parses successfully

- **WHEN** named.conf contains `logging { channel queries_log { file "/var/log/shadowdns/queries.log" versions 3 size 5000m; severity debug; print-severity yes; print-time yes; print-category yes; }; category queries { queries_log; }; };`
- **THEN** the loader produces a query-log record with file path `/var/log/shadowdns/queries.log`, all three print options enabled, and the rotation-parameters-present flag set

#### Scenario: Relative file path resolves against options directory

- **WHEN** the channel declares `file "queries.log";` and `options { directory "/etc/namedb"; };` is present
- **THEN** the query-log record's file path is `/etc/namedb/queries.log`

#### Scenario: Multiple channels in category queries

- **WHEN** `category queries { chan_a; chan_b; };` lists two file channels
- **THEN** the loader uses the first file channel and emits a warning that the remaining channels are ignored

<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
-->

<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
code:
  - packaging/named.conf.example
  - internal/config/zones.go
  - NOTES.md
  - cmd/shadowdns/main.go
  - internal/config/logging.go
  - README.md
  - internal/querylog/querylog.go
  - internal/server/server.go
  - internal/server/handler.go
tests:
  - internal/server/handler_querylog_test.go
  - internal/config/zones_test.go
  - cmd/shadowdns/querylog_test.go
  - internal/querylog/querylog_test.go
  - internal/config/logging_test.go
-->

---
### Requirement: Disable query logging for unsupported logging configurations

The config-loader SHALL yield no query-log record (query logging disabled) without failing startup when any of the following holds: the `logging{}` block is absent; no `category queries` is declared; the channel referenced by `category queries` is `null` or another built-in channel (`default_syslog`, `default_stderr`, `default_debug`); the referenced channel is a user-defined non-file channel (e.g., `syslog` or `stderr` destination); or the channel's `severity` is stricter than `info` (`notice`, `warning`, `error`, or `critical`). For the user-defined-non-file-channel and severity-stricter-than-info cases the loader SHALL emit a warning naming the reason; the remaining cases — including built-in channels, which deliberately target non-file destinations and are not configuration errors — SHALL disable silently. Severity values `info`, `debug` (with optional level), and `dynamic` SHALL enable query logging.

#### Scenario: Disable conditions yield no query-log record

- **WHEN** named.conf matches any disable condition
- **THEN** the loaded configuration carries no query-log record and startup proceeds

##### Example: disable condition matrix

| logging block content | Result | Warning emitted |
| --------------------- | ------ | --------------- |
| (no `logging{}` block at all) | disabled | no |
| `logging { category queries { null; }; };` | disabled | no |
| channel `queries_log` declared but no `category queries` | disabled | no |
| `category queries { default_syslog; };` | disabled | no |
| file channel with `severity warning;` | disabled | yes |
| `channel q { syslog daemon; }; category queries { q; };` | disabled | yes |
| file channel with `severity debug 3;` | enabled | no |
| file channel with `severity dynamic;` | enabled | no |

<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
-->

<!-- @trace
source: add-bind9-compatible-queries-log
updated: 2026-06-07
code:
  - packaging/named.conf.example
  - internal/config/zones.go
  - NOTES.md
  - cmd/shadowdns/main.go
  - internal/config/logging.go
  - README.md
  - internal/querylog/querylog.go
  - internal/server/server.go
  - internal/server/handler.go
tests:
  - internal/server/handler_querylog_test.go
  - internal/config/zones_test.go
  - cmd/shadowdns/querylog_test.go
  - internal/querylog/querylog_test.go
  - internal/config/logging_test.go
-->

---
### Requirement: Parse the rate-limit block in the options block

The config-loader SHALL parse a `rate-limit { ... }` block inside the `options` block of `named.conf` into a rate-limit configuration value. It SHALL recognize the sub-options `responses-per-second`, `referrals-per-second`, `nodata-per-second`, `nxdomains-per-second`, `errors-per-second`, `all-per-second`, `window`, `slip`, `ipv4-prefix-length`, `ipv6-prefix-length`, `exempt-clients`, `log-only`, `max-table-size`, and `min-table-size`. Absent sub-options SHALL take BIND-compatible defaults: per-second limits default to `0` (and the per-category limits default to the value of `responses-per-second` when not individually set), `window` defaults to `15`, `slip` defaults to `2`, `ipv4-prefix-length` defaults to `24`, `ipv6-prefix-length` defaults to `56`, `log-only` defaults to `no`, `max-table-size` defaults to `20000`, and `min-table-size` defaults to `500`. A value outside its BIND-defined valid range SHALL cause a fatal parse error consistent with other numeric option validation. When no `rate-limit` block is present, the parsed configuration SHALL indicate that rate limiting is unconfigured (distinct from a block with all-zero limits).

#### Scenario: Full rate-limit block parses with explicit values

- **WHEN** the `options` block contains `rate-limit { responses-per-second 10; window 20; slip 3; exempt-clients { 192.0.2.0/24; }; };`
- **THEN** the parsed configuration SHALL report `responses-per-second = 10`, `window = 20`, `slip = 3`, and an exempt list containing `192.0.2.0/24`

#### Scenario: Omitted sub-options take BIND defaults

- **WHEN** the `options` block contains `rate-limit { responses-per-second 5; };`
- **THEN** the parsed configuration SHALL report `window = 15`, `slip = 2`, `ipv4-prefix-length = 24`, `ipv6-prefix-length = 56`, `max-table-size = 20000`, and `min-table-size = 500`

#### Scenario: Per-category limit defaults to responses-per-second

- **WHEN** the block sets `responses-per-second 8;` and does not set `nxdomains-per-second`
- **THEN** the parsed `nxdomains-per-second` SHALL be `8`

#### Scenario: Out-of-range value is fatal

- **WHEN** the block contains `slip 99;` (outside the valid range 0–10)
- **THEN** the loader SHALL return a fatal parse error and SHALL NOT start the server

#### Scenario: Absent block is distinguishable from zeroed block

- **WHEN** the `options` block contains no `rate-limit` block
- **THEN** the parsed configuration SHALL indicate rate limiting is unconfigured rather than configured with zero limits

<!-- @trace
source: add-response-rate-limiting
updated: 2026-06-09
code:
  - internal/ratelimit/exempt.go
  - cmd/shadowdns/main.go
  - internal/config/options.go
  - internal/server/handler.go
  - internal/ratelimit/writer.go
  - testdata/integration/named.conf
  - internal/config/ratelimit.go
  - internal/config/zones.go
  - internal/ratelimit/slip.go
  - internal/ratelimit/table.go
  - internal/ratelimit/classify.go
  - internal/ratelimit/limiter.go
  - internal/metrics/metrics.go
  - internal/server/server.go
  - README.md
  - internal/ratelimit/key.go
tests:
  - internal/config/ratelimit_test.go
  - internal/ratelimit/classify_test.go
  - internal/ratelimit/table_test.go
  - internal/ratelimit/limiter_decide_test.go
  - internal/server/handler_ratelimit_test.go
  - internal/metrics/metrics_ratelimit_test.go
  - internal/ratelimit/writer_test.go
  - internal/ratelimit/slip_test.go
  - internal/ratelimit/limiter_credit_test.go
  - internal/ratelimit/key_test.go
  - internal/config/ratelimit_warn_test.go
-->

---
### Requirement: Warn and ignore unsupported rate-limit constructs

The config-loader SHALL emit a warning and ignore the `qps-scale` sub-option when it appears inside a `rate-limit` block, because load-adaptive scaling is not implemented. The config-loader SHALL emit a warning and ignore a `rate-limit` block that appears inside a `view` clause, because rate limiting is supported only at the global `options` scope; such a view-level block SHALL NOT cause a fatal error.

#### Scenario: qps-scale is warned and ignored

- **WHEN** a `rate-limit` block contains `qps-scale 250;`
- **THEN** the loader SHALL emit a warning, SHALL ignore the sub-option, and SHALL continue parsing the rest of the block

#### Scenario: View-level rate-limit is warned and ignored

- **WHEN** a `view` clause contains a `rate-limit { ... }` block
- **THEN** the loader SHALL emit a warning, SHALL ignore the block, and SHALL NOT return a fatal error

<!-- @trace
source: add-response-rate-limiting
updated: 2026-06-09
code:
  - internal/ratelimit/exempt.go
  - cmd/shadowdns/main.go
  - internal/config/options.go
  - internal/server/handler.go
  - internal/ratelimit/writer.go
  - testdata/integration/named.conf
  - internal/config/ratelimit.go
  - internal/config/zones.go
  - internal/ratelimit/slip.go
  - internal/ratelimit/table.go
  - internal/ratelimit/classify.go
  - internal/ratelimit/limiter.go
  - internal/metrics/metrics.go
  - internal/server/server.go
  - README.md
  - internal/ratelimit/key.go
tests:
  - internal/config/ratelimit_test.go
  - internal/ratelimit/classify_test.go
  - internal/ratelimit/table_test.go
  - internal/ratelimit/limiter_decide_test.go
  - internal/server/handler_ratelimit_test.go
  - internal/metrics/metrics_ratelimit_test.go
  - internal/ratelimit/writer_test.go
  - internal/ratelimit/slip_test.go
  - internal/ratelimit/limiter_credit_test.go
  - internal/ratelimit/key_test.go
  - internal/config/ratelimit_warn_test.go
-->

---
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


<!-- @trace
source: implicit-default-view
updated: 2026-06-13
code:
  - docs/reference/cli.md
  - cmd/shadowdns/main.go
  - internal/config/match.go
  - docs/getting-started.md
  - internal/config/zones.go
  - docs/configuration/geoip.md
  - README.md
  - docs/reference/cli.zh.md
  - docs/configuration/named-conf.md
  - docs/configuration/named-conf.zh.md
  - docs/guides/ecs.md
  - docs/configuration/geoip.zh.md
  - internal/view/matcher.go
  - docs/getting-started.zh.md
  - docs/guides/ecs.zh.md
  - internal/metrics/metrics.go
tests:
  - cmd/shadowdns/main_test.go
  - internal/metrics/metrics_reload_test.go
  - test/integration/viewless_test.go
  - test/integration/geoip_optional_test.go
  - internal/config/match_test.go
  - internal/config/zones_test.go
  - test/integration/helpers_test.go
  - internal/metrics/metrics_test.go
  - cmd/shadowdns/main_reload_test.go
-->

---
### Requirement: Reject mixing of top-level zones and view blocks

When the configuration contains at least one `view` block and at least one top-level zone declaration — regardless of declaration order and regardless of which file (root or included) each appears in — the config-loader SHALL return a fatal error that names the first top-level zone together with its source file path and line number, where "first" means first in parse order with `include` directives expanded in place (depth-first) at their point of occurrence. The error SHALL state that all zones must be declared inside views when any view is present, and SHALL fail both startup and `--dry-run`; on SIGHUP reload the existing keep-old-config behavior for load failures applies.

#### Scenario: Top-level zone declared before a view fails

- **WHEN** `named.conf` declares a top-level zone `"example.com"` on line 5 and a `view "view-other"` block later in the same file
- **THEN** the loader returns a fatal error naming zone `example.com`, the named.conf path, and line 5

#### Scenario: Mixing across included files fails regardless of order

- **WHEN** `named.conf` includes `master.zones` containing only view blocks AND `named.conf` itself declares a top-level zone after the include directive
- **THEN** the loader returns a fatal error naming that top-level zone with its source file and line, even though the zone was parsed after all views

<!-- @trace
source: implicit-default-view
updated: 2026-06-13
code:
  - docs/reference/cli.md
  - cmd/shadowdns/main.go
  - internal/config/match.go
  - docs/getting-started.md
  - internal/config/zones.go
  - docs/configuration/geoip.md
  - README.md
  - docs/reference/cli.zh.md
  - docs/configuration/named-conf.md
  - docs/configuration/named-conf.zh.md
  - docs/guides/ecs.md
  - docs/configuration/geoip.zh.md
  - internal/view/matcher.go
  - docs/getting-started.zh.md
  - docs/guides/ecs.zh.md
  - internal/metrics/metrics.go
tests:
  - cmd/shadowdns/main_test.go
  - internal/metrics/metrics_reload_test.go
  - test/integration/viewless_test.go
  - test/integration/geoip_optional_test.go
  - internal/config/match_test.go
  - internal/config/zones_test.go
  - test/integration/helpers_test.go
  - internal/metrics/metrics_test.go
  - cmd/shadowdns/main_reload_test.go
-->

---
### Requirement: Honor options block from included files

The config-loader SHALL apply an `options { ... }` block regardless of which file in the include tree declares it: a block appearing in a file reached via an `include "...";` directive (for example the Debian-idiomatic `named.conf.options`) SHALL populate the loaded configuration's options exactly as if the block were written inline in the root `named.conf`. This applies to every options field (`directory`, `geoip-directory`, `listen-on`, `listen-on-v6`, `allow-transfer`, `rate-limit`, `recursion`, `minimal-responses`, `version`, `hostname`, `transfer-format`, `notify`).

Because options fields consumed at parse time (notably `directory` for relative zone-`file` resolution) read the options state accumulated so far, an `options` block intended to govern later views/zones SHALL be included before those views/zones in declaration order (as in the Debian layout, where `named.conf` includes `named.conf.options` before `named.conf.local`).

When more than one `options { ... }` block is encountered across the include tree, the last-parsed block SHALL take effect (BIND permits a single `options` statement; ShadowDNS tolerates duplicates rather than failing) and the loader SHALL emit a warning naming the file and line of the additional block.

#### Scenario: Options block in an included file is honored

- **WHEN** `named.conf` contains only `include "named.conf.options";` and `include "named.conf.local";`, and `named.conf.options` contains `options { directory "/etc/bind"; geoip-directory "/etc/bind/geoip"; listen-on { 192.0.2.1; }; };`
- **THEN** the loaded configuration's options record has `directory="/etc/bind"`, `geoipDirectory="/etc/bind/geoip"`, and `listenOn=[192.0.2.1]` (not empty/dropped)

#### Scenario: Geo views in an included view file start when geoip-directory is in the included options file

- **WHEN** `named.conf` includes `named.conf.options` (declaring `geoip-directory`) before `named.conf.local` (declaring a view with a `geoip country` match-clients rule)
- **THEN** startup GeoIP loading reads the populated `geoip-directory` and does not fail with "geoip-directory is not set"

#### Scenario: Multiple options blocks across the include tree warn and last wins

- **WHEN** the root `named.conf` declares `options { directory "/first"; };` and then includes a file declaring `options { directory "/second"; };`
- **THEN** the loaded configuration's options record has `directory="/second"` AND the loader emits exactly one warning naming the file and line of the second block

<!-- @trace
source: debian-named-conf-layout
updated: 2026-06-13
code:
  - nfpm.yaml
  - packaging/named.conf.local.example
  - testdata/integration/cnames/db.example.com.cname
  - scripts/test-deb.sh
  - internal/config/zones.go
  - testdata/integration/db.example.com-other
  - README.md
  - docs/getting-started.zh.md
  - docs/configuration/named-conf.zh.md
  - testdata/integration/db.include-test.example
  - docs/migration.md
  - testdata/integration/db.example.com-th
  - docs/configuration/named-conf.md
  - testdata/integration/named.conf
  - testdata/integration/named.conf.local
  - packaging/named.conf.options.example
  - docs/getting-started.md
  - docs/migration.zh.md
  - testdata/integration/named.conf.options
  - scripts/gen-container-testdata.go
  - scripts/smoke.sh
  - testdata/integration/db.backup.example-other
  - testdata/integration/db.backup.example-th
  - testdata/integration/db.backup.example.overrides
  - packaging/named.conf.example
  - testdata/integration/README.md
tests:
  - test/integration/helpers_test.go
  - internal/config/zones_test.go
  - test/integration/listenon_test.go
  - test/integration/query_test.go
  - test/integration/prune_backup_test.go
  - internal/prunebackup/lexer_test.go
-->

---
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

<!-- @trace
source: bind-config-tolerant-parsing
updated: 2026-06-13
code:
  - internal/config/match.go
  - testdata/integration/bindcompat/db.0
  - docs/configuration/named-conf.zh.md
  - scripts/smoke.sh
  - testdata/integration/master/cnames/example.com_cname
  - internal/config/options.go
  - testdata/integration/bindcompat/db.127
  - internal/config/zones.go
  - testdata/integration/db.backup.example.overrides
  - testdata/integration/master/example.com_view-other.fwd
  - testdata/integration/cnames/db.example.com.cname
  - testdata/integration/master/backup.example_view-other.fwd
  - docs/getting-started.md
  - testdata/integration/bindcompat/shadowdns.yaml
  - testdata/integration/master/backup.example_overrides
  - testdata/integration/named.conf.local
  - testdata/integration/master.zones
  - docs/migration.md
  - testdata/integration/db.backup.example-th
  - testdata/integration/bindcompat/README.md
  - packaging/named.conf.options.example
  - docs/getting-started.zh.md
  - testdata/integration/bindcompat/named.conf
  - testdata/integration/db.include-test.example
  - testdata/integration/bindcompat/named.conf.local
  - nfpm.yaml
  - testdata/integration/db.example.com-other
  - docs/migration.zh.md
  - docs/configuration/named-conf.md
  - scripts/gen-container-testdata.go
  - scripts/test-deb.sh
  - testdata/integration/db.backup.example-other
  - testdata/integration/bindcompat/named.conf.default-zones
  - testdata/integration/bindcompat/db.255
  - testdata/integration/master/example.com_include.fwd
  - testdata/integration/named.conf
  - testdata/integration/bindcompat/db.local
  - testdata/integration/master/backup.example_view-th.fwd
  - packaging/named.conf.local.example
  - testdata/integration/named.conf.options
  - testdata/integration/README.md
  - testdata/integration/db.example.com-th
  - testdata/integration/bindcompat/named.conf.options
  - packaging/named.conf.example
  - testdata/integration/master/example.com_view-th.fwd
  - README.md
tests:
  - test/integration/bind_compat_test.go
  - internal/prunebackup/lexer_test.go
  - internal/view/matcher_test.go
  - test/integration/listenon_test.go
  - test/integration/query_test.go
  - test/integration/prune_backup_test.go
  - internal/config/match_test.go
  - internal/config/zones_test.go
  - test/integration/helpers_test.go
-->