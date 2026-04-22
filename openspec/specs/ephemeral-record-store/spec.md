# ephemeral-record-store Specification

## Purpose

TBD - created by archiving change 'ephemeral-txt-api'. Update Purpose after archive.

## Requirements

### Requirement: Store ephemeral TXT records in memory with expiration

The ephemeral record store SHALL hold TXT records in memory, keyed by lowercased FQDN (with trailing dot). Each FQDN MAY have multiple independent entries; each entry SHALL store the TXT value and an absolute expiration timestamp. Entries for the same FQDN are distinguished by their value. The store SHALL be safe for concurrent access from multiple goroutines.

#### Scenario: Store and retrieve a TXT record

- **WHEN** a TXT record for `_acme-challenge.example.com.` with value `abc123` and TTL 120 is inserted
- **THEN** a subsequent lookup for `_acme-challenge.example.com.` SHALL return a non-empty list whose single element has value `abc123` and a TTL equal to the remaining seconds until expiration (minimum 1)

#### Scenario: Lookup returns empty for unknown FQDN

- **WHEN** a lookup is performed for an FQDN that has no ephemeral record
- **THEN** the store SHALL return nil or an empty list

#### Scenario: Put appends a new value under an existing FQDN

- **WHEN** a TXT record for `_acme-challenge.example.com.` with value `abc123` already exists and a new Put is issued for the same FQDN with a different value `def456`
- **THEN** the store SHALL hold both entries and a subsequent lookup for `_acme-challenge.example.com.` SHALL return both values, each with its own remaining TTL

#### Scenario: Put refreshes TTL when value already exists

- **WHEN** a TXT record for `_acme-challenge.example.com.` with value `abc123` already exists and a new Put is issued for the same FQDN with the identical value `abc123` and a new TTL
- **THEN** the store SHALL NOT create a second entry and SHALL update the existing entry's expiration using the new TTL


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

---
### Requirement: Expired records are not returned on lookup

The store SHALL NOT return records whose expiration timestamp is in the past. Expired records SHALL be treated as non-existent during lookup (lazy eviction).

#### Scenario: Lookup after TTL expiration returns empty

- **WHEN** a TXT record was inserted with TTL 60 and 61 seconds have elapsed
- **THEN** a lookup for that FQDN SHALL return nil or an empty list

#### Scenario: TTL in response is dynamically computed

- **WHEN** a TXT record was inserted with TTL 120 and 30 seconds have elapsed
- **THEN** a lookup SHALL return an entry with TTL 90

#### Scenario: Per-entry expiration under the same FQDN

- **WHEN** two entries exist for the same FQDN — one with TTL 30 and one with TTL 300 — and 31 seconds have elapsed
- **THEN** a lookup SHALL return only the second entry (with TTL ~269); the first entry SHALL NOT be returned


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

---
### Requirement: Periodic garbage collection removes expired records

The store SHALL run a background goroutine that periodically scans all entries and removes those whose expiration timestamp is in the past. The default scan interval SHALL be 30 seconds. The GC goroutine SHALL stop when the provided context is cancelled.

#### Scenario: Expired record is removed by GC

- **WHEN** a TXT record has expired and the GC cycle runs
- **THEN** the expired entry SHALL be removed from the store's internal map

#### Scenario: GC stops on context cancellation

- **WHEN** the context passed to the store is cancelled
- **THEN** the GC goroutine SHALL exit without leaking


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

---
### Requirement: Clear removes all ephemeral records

The store SHALL provide a Clear method that removes all entries unconditionally. This is used during SIGHUP reload.

#### Scenario: Clear removes all records

- **WHEN** the store contains 5 ephemeral records and Clear is called
- **THEN** all subsequent lookups SHALL return nil or empty results


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

---
### Requirement: Delete removes all ephemeral entries for an FQDN

The store SHALL provide a Delete method that removes every ephemeral entry associated with the given FQDN in a single operation. Delete SHALL NOT accept a specific value argument — the operation is always whole-FQDN. Deleting a non-existent FQDN SHALL be a no-op (no error). Delete SHALL only touch the ephemeral store; records served from zone files SHALL NOT be affected.

#### Scenario: Delete removes all entries for the FQDN

- **WHEN** three ephemeral entries exist for `_acme-challenge.example.com.` with different values, and Delete is called for that FQDN
- **THEN** a subsequent lookup for that FQDN SHALL return nil or an empty list

#### Scenario: Delete of non-existent FQDN is a no-op

- **WHEN** Delete is called for an FQDN that has no ephemeral record
- **THEN** no error SHALL be returned

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