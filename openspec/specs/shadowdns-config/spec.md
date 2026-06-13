# shadowdns-config Specification

## Purpose

TBD - created by archiving change 'ephemeral-txt-api'. Update Purpose after archive.

## Requirements

### Requirement: Load unified ShadowDNS configuration from a YAML file

The shadowdns-config loader SHALL parse a single YAML file specified by the `--config` CLI flag. The file is a single YAML document containing the following top-level sections:

- `aliases` (optional): Mapping from each root domain to an object with `members` (non-empty list of backup domain strings), `rewrite_rdata_labels` (bool, optional, default `false`), and `collapse_cname_chain` (bool, optional, default `false`). When the key is absent or the map is empty, no aliases are loaded.
- `ephemeral_api` (object, optional): Configuration for the ephemeral TXT API server. When the key is absent, the API server is not started.

The loader SHALL use strict decoding: unknown top-level keys or unknown fields inside recognized sections SHALL cause a load error that identifies the offending key. A value under `aliases` whose YAML node type is not a mapping with the documented fields (for example, a sequence of strings such as `root.com: [backup.com]`, or a bare string such as `backup.com: root.com`) SHALL be rejected by the YAML decoder as a type mismatch.

#### Scenario: Valid config with both sections

- **WHEN** the config file contains `aliases: {root.com: {members: [backup.com]}}` and `ephemeral_api: {listen: "127.0.0.1:8053", allow: ["10.0.0.5"]}`
- **THEN** the loader SHALL return a config where the alias map has one entry `{backup.com. -> root.com.}` with `rewrite_rdata_labels: false` and `ephemeral_api` is populated

#### Scenario: Aliases object form with rewrite_rdata_labels enabled

- **WHEN** the config file contains `aliases: {root.com: {members: [backup.com, mirror.com], rewrite_rdata_labels: true}}`
- **THEN** the loader SHALL return a config where both `backup.com.` and `mirror.com.` map to root `root.com.` with `rewrite_rdata_labels: true`

#### Scenario: Aliases object form omits rewrite_rdata_labels

- **WHEN** the config file contains `aliases: {root.com: {members: [backup.com]}}`
- **THEN** the loader SHALL return a config where `backup.com.` maps to root `root.com.` with `rewrite_rdata_labels: false`

#### Scenario: Aliases object form omits collapse_cname_chain

- **WHEN** the config file contains `aliases: {root.com: {members: [backup.com]}}`
- **THEN** the loader SHALL return a config whose collapse lookup reports `false` for root `root.com.`

#### Scenario: Multiple roots with mapping form

- **WHEN** the config file contains `aliases: {root-a.net: {members: [alias-a.net]}, root-b.net: {members: [alias-b.net], rewrite_rdata_labels: true}}`
- **THEN** the loader SHALL return a config where `alias-a.net.` maps to `root-a.net.` with the flag false, and `alias-b.net.` maps to `root-b.net.` with the flag true

#### Scenario: Ephemeral-API-only config

- **WHEN** the config file contains only `ephemeral_api: {listen: "127.0.0.1:8053", allow: ["10.0.0.5"]}`
- **THEN** the loader SHALL return a config with an empty alias map and `ephemeral_api` populated

#### Scenario: Empty aliases map is accepted

- **WHEN** the config file contains `aliases: {}`
- **THEN** the loader SHALL return a config with an empty alias map and no error

#### Scenario: Sequence form aliases value is rejected

- **WHEN** the config file contains `aliases: {root.com: [backup.com]}` (a sequence of backup strings under a root key)
- **THEN** the loader SHALL return a YAML decoding error identifying the type mismatch and naming `members` as the required field; the server SHALL NOT start with the partial configuration

#### Scenario: Legacy one-to-one aliases format is rejected

- **WHEN** the config file contains `aliases: {backup.com: root.com}` (a bare string value under a root key)
- **THEN** the loader SHALL return a YAML decoding error identifying the type mismatch; the server SHALL NOT start with the partial configuration

#### Scenario: Aliases object form with unknown field is rejected

- **WHEN** the config file contains `aliases: {root.com: {members: [backup.com], unknown_flag: true}}`
- **THEN** the loader SHALL return an error that names the unknown field `unknown_flag` and lists the accepted fields `members`, `rewrite_rdata_labels`, and `collapse_cname_chain`

#### Scenario: Aliases object form missing members is rejected

- **WHEN** the config file contains `aliases: {root.com: {rewrite_rdata_labels: true}}`
- **THEN** the loader SHALL return an error indicating that `members` is required when the alias value is an object

#### Scenario: Aliases object form with empty members is rejected

- **WHEN** the config file contains `aliases: {root.com: {members: []}}`
- **THEN** the loader SHALL return an error indicating that `members` MUST be non-empty

#### Scenario: Unknown top-level key fails

- **WHEN** the config file contains a top-level key that is not `aliases` or `ephemeral_api`
- **THEN** the loader SHALL return an error that names the unknown key

#### Scenario: Config file does not exist

- **WHEN** the path passed to `--config` does not exist
- **THEN** the loader SHALL return an error identifying the missing file path


<!-- @trace
source: add-cname-chain-collapsing
updated: 2026-06-12
code:
  - packaging/shadowdns.yaml.example
  - internal/shadowdnscfg/config.go
  - internal/server/server.go
  - internal/config/aliases.go
  - internal/zone/collapse.go
  - internal/server/build.go
  - cmd/shadowdns/main.go
  - internal/alias/rewrite.go
  - internal/alias/override.go
  - internal/server/handler.go
tests:
  - test/integration/axfr_test.go
  - test/integration/listenon_test.go
  - test/integration/reload_diff_test.go
  - internal/zone/collapse_test.go
  - internal/shadowdnscfg/config_test.go
  - internal/config/aliases_test.go
  - test/integration/cname_collapse_test.go
  - internal/alias/override_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/helpers_test.go
  - test/integration/case_preservation_test.go
  - internal/server/handler_test.go
  - internal/server/build_test.go
  - internal/server/server_test.go
-->

---
### Requirement: Validate aliases section

The loader SHALL validate the `aliases` section after YAML decoding. The loader SHALL flatten the `map[root]{members, rewrite_rdata_labels, collapse_cname_chain}` structure into a normalized backup-to-root map; during flattening the loader SHALL reject the following conditions:

- The same backup domain (after normalization) appears under two different root keys.
- A backup domain is listed under a root key whose value (after normalization) equals that backup domain (self-alias).
- Any backup entry or root key is empty or contains whitespace.

A root key whose `members` list is omitted entirely from a mapping value SHALL be rejected at YAML decoding time (covered by the schema requirement above). A root key with a present but empty `members` list SHALL also be rejected at decoding time.

In addition to the backup-to-root map, flattening SHALL emit a collapse lookup keyed by the normalized root origin (lookup-fold FQDN with trailing dot) whose value is the group's `collapse_cname_chain` setting. The query-time interpretation of a missing entry (treated as disabled) is normatively owned by the `cname-chain-collapsing` capability; this loader requirement covers only the lookup's construction.

#### Scenario: Duplicate backup under different roots fails

- **WHEN** the `aliases` section contains `root-a.com: {members: [shared.com]}` and `root-b.com: {members: [shared.com]}`
- **THEN** the loader SHALL return an error naming the duplicate backup domain and both root keys

#### Scenario: Self-alias entry fails

- **WHEN** the `aliases` section contains an entry where a backup equals its root (e.g., `example.com: {members: [example.com]}`)
- **THEN** the loader SHALL return an error naming the self-alias entry

#### Scenario: Multiple backups under one root are all mapped to that root

- **WHEN** the `aliases` section contains `root.com: {members: [backup.com, mirror.com, shadow.com]}`
- **THEN** the loader SHALL return an alias map with three entries all pointing to `root.com.`

#### Scenario: Collapse lookup is keyed by normalized root origin

- **WHEN** the `aliases` section contains `Root.COM: {members: [backup.com], collapse_cname_chain: true}` (mixed-case root key)
- **THEN** the collapse lookup SHALL contain the entry `root.com. -> true`


<!-- @trace
source: add-cname-chain-collapsing
updated: 2026-06-12
code:
  - packaging/shadowdns.yaml.example
  - internal/shadowdnscfg/config.go
  - internal/server/server.go
  - internal/config/aliases.go
  - internal/zone/collapse.go
  - internal/server/build.go
  - cmd/shadowdns/main.go
  - internal/alias/rewrite.go
  - internal/alias/override.go
  - internal/server/handler.go
tests:
  - test/integration/axfr_test.go
  - test/integration/listenon_test.go
  - test/integration/reload_diff_test.go
  - internal/zone/collapse_test.go
  - internal/shadowdnscfg/config_test.go
  - internal/config/aliases_test.go
  - test/integration/cname_collapse_test.go
  - internal/alias/override_test.go
  - cmd/shadowdns/main_test.go
  - test/integration/helpers_test.go
  - test/integration/case_preservation_test.go
  - internal/server/handler_test.go
  - internal/server/build_test.go
  - internal/server/server_test.go
-->

---
### Requirement: Validate ephemeral_api section

When the `ephemeral_api` section is present, the loader SHALL validate the following fields:

- `listen` (string, required): The `host:port` address for the API server; SHALL be parseable by `net.SplitHostPort`
- `allow` (list of strings, required, non-empty): IP addresses or CIDR ranges; each entry SHALL be a valid IPv4/IPv6 address or CIDR
- `token` (string, optional): Pre-shared bearer token for authentication

#### Scenario: ephemeral_api with all fields

- **WHEN** the section contains `listen: "127.0.0.1:8053"`, `allow: ["10.0.0.5"]`, and `token: "secret"`
- **THEN** the loader SHALL accept the section and return a populated config

#### Scenario: ephemeral_api without token

- **WHEN** the section contains `listen` and `allow` but no `token` field
- **THEN** the loader SHALL accept the section and mark token validation as disabled

#### Scenario: Missing listen field fails

- **WHEN** the `ephemeral_api` section is present but omits the `listen` field
- **THEN** the loader SHALL return an error indicating the missing field

#### Scenario: Empty allow list fails

- **WHEN** the `ephemeral_api` section has an empty `allow` list or omits the `allow` field
- **THEN** the loader SHALL return an error indicating that at least one ACL entry is required

#### Scenario: Invalid listen address fails

- **WHEN** the `ephemeral_api` section contains `listen: "not-a-host-port"`
- **THEN** the loader SHALL return an error identifying the invalid listen address

#### Scenario: Invalid CIDR in allow list

- **WHEN** the `ephemeral_api` section contains `allow: ["not-an-ip"]`
- **THEN** the loader SHALL return an error identifying `not-an-ip` as an invalid ACL entry

#### Scenario: Mixed valid IPv4 and CIDR entries

- **WHEN** the section contains `allow: ["10.0.0.5", "192.168.1.0/24"]`
- **THEN** the loader SHALL accept both entries without error


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
### Requirement: Atomic reload of unified config on SIGHUP

On SIGHUP reload, the server SHALL re-read the file passed via `--config`, validate every section, and swap to the new configuration only when all sections pass validation. If any section fails validation, the server SHALL retain the previous configuration and log an error identifying the failing section and reason. The ephemeral record store SHALL be cleared only after a successful swap.

#### Scenario: Reload succeeds when all sections valid

- **WHEN** SIGHUP is received and the file passes YAML decoding and validation of both `aliases` and `ephemeral_api` sections
- **THEN** the server SHALL atomically swap to the new ServerState, clear the ephemeral record store, and log reload success

#### Scenario: Reload fails when aliases section invalid

- **WHEN** SIGHUP is received and the new `aliases` section contains a duplicate backup key
- **THEN** the server SHALL NOT swap state, SHALL retain the previous alias map, SHALL NOT clear the ephemeral record store, and SHALL log an error naming the duplicate key

#### Scenario: Reload fails when ephemeral_api section invalid

- **WHEN** SIGHUP is received and the new `ephemeral_api.allow` list contains an invalid CIDR
- **THEN** the server SHALL NOT swap state, SHALL keep the existing API listener running with its previous config, SHALL NOT clear the ephemeral record store, and SHALL log an error naming the invalid entry

#### Scenario: Reload fails when YAML decoding fails

- **WHEN** SIGHUP is received and the file contains invalid YAML or an unknown top-level key
- **THEN** the server SHALL NOT swap state and SHALL log an error identifying the decode failure

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