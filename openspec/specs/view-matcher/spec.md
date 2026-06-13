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

<!-- @trace
source: geoip-mmdb-fallback
updated: 2026-04-15
code:
  - internal/zone/parser.go
  - README.md
  - testdata/integration/cnames/db.example.com.cname
  - testdata/integration/db.include-test.example
  - scripts/smoke.sh
  - testdata/integration/named.conf.local
  - testdata/integration/db.backup.example.overrides
  - testdata/integration/db.backup.example-other
  - internal/view/geoip_asn.go
  - internal/view/geoip_country.go
  - internal/view/loader.go
tests:
  - internal/view/loader_test.go
  - test/integration/query_test.go
  - test/integration/helpers_test.go
  - internal/zone/parser_test.go
  - test/integration/backup_test.go
-->

### Requirement: Resolve client IP to a view using first-match semantics

The view-matcher SHALL accept two addresses — the real client source IP and a geo lookup address — and return the name of the first view whose `match-clients` rule set contains a matching rule. Country and ASN rules SHALL be evaluated against the geo lookup address; `any`, IP, and CIDR rules SHALL be evaluated against the source IP. Callers without an ECS-derived address SHALL pass the source IP as the geo lookup address, in which case behavior is identical to single-address resolution. Rules within a view SHALL be evaluated in declaration order and the first matching rule SHALL select that view without evaluating subsequent rules or views.

#### Scenario: First view whose rule matches wins

- **WHEN** views are declared in order `view-th` (rule: country TH), `view-eu` (rule: country DE), `view-other` (rule: any) AND the geo lookup address resolves to country DE
- **THEN** the matcher returns `view-eu`

#### Scenario: Fallback to `any` when no earlier view matches

- **WHEN** the geo lookup address resolves to a country not listed in any earlier view
- **THEN** the matcher returns the name of the view whose rule list contains `any`

#### Scenario: No matching view returns an empty result

- **WHEN** no view contains a rule that matches either address and no view declares `any`
- **THEN** the matcher returns an explicit no-view sentinel AND the caller is responsible for producing REFUSED

#### Scenario: Geo and ACL rules evaluate different addresses in one resolution

- **WHEN** views are declared in order `view-internal` (rule: CIDR `192.0.2.0/24`), `view-asia` (rule: country TW), the source IP is `198.51.100.1` (outside the CIDR), and the geo lookup address `203.0.113.0` resolves to country TW
- **THEN** the matcher returns `view-asia` because the CIDR rule evaluated the source IP and the country rule evaluated the geo lookup address

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
source: add-edns-client-subnet
updated: 2026-06-11
code:
  - docs/reference/cli.zh.md
  - internal/view/matcher.go
  - docs/index.zh.md
  - docs/index.md
  - internal/server/handler.go
  - docs/reference/cli.md
  - internal/server/server.go
  - cmd/shadowdns/main.go
  - internal/dnsutil/ecs.go
  - README.md
tests:
  - cmd/shadowdns/main_test.go
  - internal/dnsutil/ecs_test.go
  - internal/server/handler_ecs_test.go
  - internal/view/matcher_test.go
  - internal/view/testhelper_test.go
-->

---
### Requirement: Evaluate country match via MaxMind GeoLite2-Country

The view-matcher SHALL look up the geo lookup address in a MaxMind GeoLite2-Country `.mmdb` file loaded at startup and compare the resulting ISO 3166-1 alpha-2 country code (case-insensitive) against rules of type country.

#### Scenario: Country code matches

- **WHEN** the mmdb lookup for the geo lookup address returns country code `TH` and a rule declares `geoip country TH;`
- **THEN** the rule matches

#### Scenario: Case insensitivity

- **WHEN** a rule declares `geoip country th;` (lowercase) and the mmdb returns `TH`
- **THEN** the rule matches

#### Scenario: IP not in mmdb is treated as no-match for country rules

- **WHEN** the mmdb lookup returns no country for the geo lookup address
- **THEN** all country rules evaluate to no-match for that client AND matching proceeds to subsequent rules

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
source: add-edns-client-subnet
updated: 2026-06-11
code:
  - docs/reference/cli.zh.md
  - internal/view/matcher.go
  - docs/index.zh.md
  - docs/index.md
  - internal/server/handler.go
  - docs/reference/cli.md
  - internal/server/server.go
  - cmd/shadowdns/main.go
  - internal/dnsutil/ecs.go
  - README.md
tests:
  - cmd/shadowdns/main_test.go
  - internal/dnsutil/ecs_test.go
  - internal/server/handler_ecs_test.go
  - internal/view/matcher_test.go
  - internal/view/testhelper_test.go
-->

---
### Requirement: Evaluate ASN match via MaxMind GeoLite2-ASN

The view-matcher SHALL look up the geo lookup address in a MaxMind GeoLite2-ASN `.mmdb` file loaded at startup and compare the resulting AS number against the numeric AS number extracted from ASN rules.

#### Scenario: ASN number matches

- **WHEN** the mmdb lookup for the geo lookup address returns ASN 4134 and a rule declares `geoip asnum "AS4134 Chinanet";`
- **THEN** the rule matches

#### Scenario: ASN description text is ignored in comparison

- **WHEN** the mmdb description for ASN 4134 differs from the rule description but the number matches
- **THEN** the rule matches

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
source: add-edns-client-subnet
updated: 2026-06-11
code:
  - docs/reference/cli.zh.md
  - internal/view/matcher.go
  - docs/index.zh.md
  - docs/index.md
  - internal/server/handler.go
  - docs/reference/cli.md
  - internal/server/server.go
  - cmd/shadowdns/main.go
  - internal/dnsutil/ecs.go
  - README.md
tests:
  - cmd/shadowdns/main_test.go
  - internal/dnsutil/ecs_test.go
  - internal/server/handler_ecs_test.go
  - internal/view/matcher_test.go
  - internal/view/testhelper_test.go
-->

---
### Requirement: Evaluate IP and CIDR rules without external lookup

The view-matcher SHALL evaluate `IPRule` and `CIDRRule` entries by direct comparison against the real client source IP; no GeoIP lookup SHALL be performed for these rule types, and the geo lookup address MUST NOT influence their evaluation (an ECS-derived address is client-controlled data and MUST NOT satisfy ACL-style rules).

#### Scenario: Single IP rule matches exactly

- **WHEN** a rule declares `192.0.2.8;` and the client source IP is `192.0.2.8`
- **THEN** the rule matches

#### Scenario: Client IP inside CIDR prefix matches

- **WHEN** a rule declares `198.51.100.0/26;` and the client source IP is `198.51.100.30`
- **THEN** the rule matches

#### Scenario: Client IP outside CIDR prefix does not match

- **WHEN** the client source IP is `198.51.100.100` (outside the /26 prefix)
- **THEN** the rule does not match

#### Scenario: Geo lookup address never satisfies a CIDR rule

- **WHEN** a rule declares `192.0.2.0/24;`, the client source IP is `203.0.113.7`, and the geo lookup address is `192.0.2.5`
- **THEN** the rule does not match

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
source: add-edns-client-subnet
updated: 2026-06-11
code:
  - docs/reference/cli.zh.md
  - internal/view/matcher.go
  - docs/index.zh.md
  - docs/index.md
  - internal/server/handler.go
  - docs/reference/cli.md
  - internal/server/server.go
  - cmd/shadowdns/main.go
  - internal/dnsutil/ecs.go
  - README.md
tests:
  - cmd/shadowdns/main_test.go
  - internal/dnsutil/ecs_test.go
  - internal/server/handler_ecs_test.go
  - internal/view/matcher_test.go
  - internal/view/testhelper_test.go
-->

---
### Requirement: Fail startup when GeoIP databases are missing or unreadable

For the purpose of this requirement, `options.geoip-directory` counts as **unset** when the option is absent or its value is the empty string; the two cases SHALL behave identically.

When `options.geoip-directory` is set, the view-matcher SHALL load a country mmdb and an ASN mmdb from that directory at startup, regardless of whether any view declares a geo rule. For each database, the loader SHALL try candidate filenames in a fixed priority order and accept the first file that successfully opens and passes MaxMind mmdb validation:

- Country: `GeoIP2-Country.mmdb`, then `GeoLite2-Country.mmdb`.
- ASN: `GeoIP2-ASN.mmdb`, then `GeoLite2-ASN.mmdb`.

Country and ASN SHALL be resolved independently — the loader MUST NOT require both databases to use the same edition.

If every candidate filename for either database fails to open or validate, the server SHALL exit with a non-zero status and an error message listing every candidate path that was attempted and the reason each failed.

Upon successful load, the server SHALL log the full path of each opened mmdb so operators can identify the edition in use from the `path` field.

When `options.geoip-directory` is unset and at least one view's `match-clients` contains a country or ASN rule, the server SHALL exit with a non-zero status and an explicit configuration error (never a file-open error) naming the first such view (in declaration order) together with that view's source file path and line number. When `options.geoip-directory` is unset and no view declares a country or ASN rule, startup SHALL proceed without loading any GeoIP database.

#### Scenario: Paid-edition country mmdb loads when present

- **WHEN** `GeoIP2-Country.mmdb` exists in the geoip-directory and is a valid mmdb
- **THEN** the loader opens it AND does not attempt `GeoLite2-Country.mmdb`

#### Scenario: Falls back to GeoLite2 when GeoIP2 is absent

- **WHEN** `GeoIP2-Country.mmdb` does not exist AND `GeoLite2-Country.mmdb` exists and is a valid mmdb
- **THEN** the loader opens `GeoLite2-Country.mmdb`

#### Scenario: Mixed editions across Country and ASN

- **WHEN** only `GeoIP2-Country.mmdb` and `GeoLite2-ASN.mmdb` exist in the geoip-directory
- **THEN** the loader opens `GeoIP2-Country.mmdb` for country AND `GeoLite2-ASN.mmdb` for ASN

#### Scenario: Higher-priority file that fails validation falls through to next candidate

- **WHEN** `GeoIP2-Country.mmdb` exists but fails mmdb validation AND `GeoLite2-Country.mmdb` exists and is valid
- **THEN** the loader opens `GeoLite2-Country.mmdb`

#### Scenario: All country candidates missing is fatal

- **WHEN** neither `GeoIP2-Country.mmdb` nor `GeoLite2-Country.mmdb` exists at the configured path
- **THEN** the process exits with a non-zero status AND the error message names both attempted paths

#### Scenario: All ASN candidates invalid is fatal

- **WHEN** both `GeoIP2-ASN.mmdb` and `GeoLite2-ASN.mmdb` exist but both fail library-level mmdb validation
- **THEN** the process exits with a non-zero status AND the error message lists both paths together with the validation error for each

#### Scenario: Successful load is logged with full path

- **WHEN** the loader successfully opens a mmdb file for either database
- **THEN** the server emits an info-level log entry whose `path` field contains the full path of the opened file

#### Scenario: Geo rules without geoip-directory fail startup naming the offending view

- **WHEN** a view `"view-th"` declared in `master.zones` at line 12 contains `geoip country TH;` in its match-clients AND `options` has no `geoip-directory`
- **THEN** the process exits with a non-zero status AND the error message contains the view name `view-th`, the `master.zones` path, and line 12

#### Scenario: Empty-string geoip-directory behaves as unset

- **WHEN** `options` declares `geoip-directory "";` AND a view's match-clients contains `geoip country TH;`
- **THEN** the process exits with the same explicit configuration error as when the option is absent — never a relative-path file-open error

#### Scenario: Directory set but no geo rules still loads and validates

- **WHEN** `geoip-directory` is set to a non-empty path AND no view declares a country or ASN rule
- **THEN** both mmdb databases are loaded and validated exactly as before, and a load failure remains fatal


<!-- @trace
source: geoip-optional
updated: 2026-06-13
code:
  - docs/configuration/geoip.zh.md
  - docs/reference/cli.zh.md
  - internal/config/match.go
  - cmd/shadowdns/main.go
  - docs/guides/ecs.zh.md
  - docs/guides/ecs.md
  - internal/metrics/metrics.go
  - docs/configuration/named-conf.md
  - docs/configuration/geoip.md
  - docs/configuration/named-conf.zh.md
  - docs/reference/cli.md
  - internal/view/matcher.go
  - README.md
  - docs/getting-started.zh.md
  - docs/getting-started.md
tests:
  - internal/config/match_test.go
  - test/integration/helpers_test.go
  - test/integration/geoip_optional_test.go
  - internal/metrics/metrics_test.go
  - cmd/shadowdns/main_test.go
  - cmd/shadowdns/main_reload_test.go
  - internal/metrics/metrics_reload_test.go
-->

---
### Requirement: Operate without GeoIP databases when configuration declares no geo rules

When the entire configuration (the root `named.conf` plus every included file) declares no country and no ASN match-clients rule and `options.geoip-directory` is unset (absent or empty), the server SHALL start and serve queries with nil GeoIP database handles: `any`, IP, and CIDR rules SHALL evaluate exactly as when databases are loaded, and view resolution SHALL be unaffected. The startup readiness log SHALL carry a boolean field named `geoip_enabled` reporting whether GeoIP databases are loaded, and the `--dry-run` summary log SHALL carry the same `geoip_enabled` field. `--dry-run` SHALL succeed and fail under exactly the same GeoIP conditions as a real startup: it SHALL succeed without mmdb files when no geo rule exists, and SHALL fail with the same configuration error when a geo rule exists without `geoip-directory`. Whenever a configuration load (startup, or a SIGHUP reload) completes with `--ecs-enable` active and no GeoIP database loaded, the server SHALL log one warning stating that ECS cannot influence view selection (the ECS option echo behavior is retained); the warning SHALL NOT prevent startup or reload.

#### Scenario: Config with only IP and CIDR rules starts without mmdb files

- **WHEN** every view's match-clients uses only `any`, IP, or CIDR rules AND `geoip-directory` is not set AND no mmdb file exists on the host
- **THEN** the server starts successfully AND a query from a source IP matching a CIDR rule receives the authoritative answer from that view's zone

#### Scenario: Readiness log reports GeoIP state

- **WHEN** the server finishes startup without loading GeoIP databases
- **THEN** the readiness log line carries `geoip_enabled=false`

#### Scenario: Dry-run succeeds without GeoIP and reports the state

- **WHEN** `--dry-run` runs against a config with no geo rules and no `geoip-directory` on a host with no mmdb files
- **THEN** the dry-run exits successfully AND its summary log line carries `geoip_enabled=false`

#### Scenario: Dry-run fails when geo rules lack geoip-directory

- **WHEN** `--dry-run` runs against a config where a view declares `geoip country TH;` and `geoip-directory` is unset
- **THEN** the dry-run exits with a non-zero status AND the error names the offending view with its source file and line — identical to a real startup

#### Scenario: ECS enabled without GeoIP warns at startup

- **WHEN** the server starts with `--ecs-enable` AND no GeoIP database is loaded
- **THEN** one warning is logged stating ECS has no effect on view selection without GeoIP databases AND the server starts normally

#### Scenario: ECS warning repeats when a reload disables GeoIP

- **WHEN** a server running with `--ecs-enable` and loaded GeoIP databases completes a SIGHUP reload whose new configuration has no geo rules and no `geoip-directory`
- **THEN** the same ECS-without-GeoIP warning is logged once for that reload

<!-- @trace
source: geoip-optional
updated: 2026-06-13
code:
  - docs/configuration/geoip.zh.md
  - docs/reference/cli.zh.md
  - internal/config/match.go
  - cmd/shadowdns/main.go
  - docs/guides/ecs.zh.md
  - docs/guides/ecs.md
  - internal/metrics/metrics.go
  - docs/configuration/named-conf.md
  - docs/configuration/geoip.md
  - docs/configuration/named-conf.zh.md
  - docs/reference/cli.md
  - internal/view/matcher.go
  - README.md
  - docs/getting-started.zh.md
  - docs/getting-started.md
tests:
  - internal/config/match_test.go
  - test/integration/helpers_test.go
  - test/integration/geoip_optional_test.go
  - internal/metrics/metrics_test.go
  - cmd/shadowdns/main_test.go
  - cmd/shadowdns/main_reload_test.go
  - internal/metrics/metrics_reload_test.go
-->

---
### Requirement: Fail closed when a match-clients rule cannot be evaluated

When the config-loader drops a `match-clients` rule it cannot evaluate (a named-acl reference, a `!` negation, or a nested group — see config-loader), the view-matcher SHALL treat that dropped rule as never-matching. A dropped rule SHALL NOT be promoted to `any` or to any matching behavior. Consequently, a view whose entire `match-clients` set was dropped SHALL match no client and SHALL serve none of its zones; the matcher SHALL fall through to subsequent views exactly as it does for a view whose rules simply do not match the client. The matcher SHALL NOT fail open under any circumstance: an unevaluable access-control construct SHALL only ever reduce, never widen, the set of clients a view serves.

#### Scenario: View with only a dropped rule matches no client

- **WHEN** a view `"internal"` declared `match-clients { internal-net; }` where `internal-net` was dropped as an unrecognized rule, and a query arrives from any source IP
- **THEN** the view-matcher does not select `"internal"` for that query AND evaluation proceeds to subsequent views

#### Scenario: Dropped rule does not widen a view with other rules

- **WHEN** a view declares `match-clients { internal-net; 192.0.2.0/24; }` where `internal-net` was dropped, and a query arrives from source IP `198.51.100.7` (outside the CIDR)
- **THEN** the view-matcher does not select this view (the surviving CIDR rule does not match and the dropped rule never matches)

#### Scenario: Dropped rule is never treated as a catch-all

- **WHEN** a view's only `match-clients` entry was a dropped unrecognized rule AND no later view declares `any`
- **THEN** a client that matches no other view receives the explicit no-view result (REFUSED) rather than being served by the view with the dropped rule

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