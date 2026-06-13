# ephemeral-api Specification

## Purpose

TBD - created by archiving change 'ephemeral-txt-api'. Update Purpose after archive.

## Requirements

### Requirement: HTTP API server listens on a configured address

The API server SHALL listen on the address specified in the `ephemeral_api.listen` field of the unified ShadowDNS YAML configuration file loaded via `--config`. The server SHALL start only when the `ephemeral_api` section is present in the config file. When the section is absent, no API server SHALL be started.

#### Scenario: API server starts on configured address

- **WHEN** ShadowDNS is started with `--config /etc/shadowdns/shadowdns.yaml` and the file contains `ephemeral_api: {listen: "127.0.0.1:8053", allow: ["10.0.0.5"]}`
- **THEN** the API server SHALL accept HTTP connections on `127.0.0.1:8053`

#### Scenario: API server is not started when ephemeral_api section is absent

- **WHEN** ShadowDNS is started with a `--config` file that omits the `ephemeral_api` section
- **THEN** no HTTP API server SHALL be started and no API port SHALL be bound


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
### Requirement: PUT endpoint adds or refreshes an ephemeral TXT value

The API SHALL accept `PUT /v1/txt/{fqdn}` with a JSON body containing `value` (string) and `ttl` (integer, seconds). The FQDN path parameter SHALL be canonicalized to lowercase with a trailing dot. The TTL SHALL be clamped to the range [1, 3600]. The `value` field SHALL be validated to be at most 255 UTF-8 bytes in length (the RFC 1035 TXT character-string limit); PUT requests with a longer value SHALL be rejected with HTTP 400 before touching the store. On success, the API SHALL respond with HTTP 200 and a JSON body confirming the operation.

The `ttl` field in the PUT body SHALL control only the Store-side lifespan of the entry (its expiration timestamp). It SHALL NOT influence the TTL value written into DNS response packets; DNS response TTL is fixed by the `dns-server` spec.

PUT SHALL support multiple distinct values per FQDN. When the posted value does not match any existing entry under the FQDN, the API SHALL append a new entry. When the posted value matches an existing entry exactly, the API SHALL refresh that entry's expiration using the new TTL instead of creating a duplicate. The operation SHALL remain idempotent: two consecutive identical PUT calls SHALL produce the same final state as a single call.

The response body SHALL include the canonical FQDN, the clamped TTL applied to the affected entry (the Store-side lifespan value), and the total number of ephemeral entries currently held for that FQDN.

#### Scenario: Create a new ephemeral TXT record

- **WHEN** a PUT request is sent to `/v1/txt/_acme-challenge.example.com` with body `{"value": "token123", "ttl": 120}` and no prior entries exist for that FQDN
- **THEN** the API SHALL respond with HTTP 200 and body `{"status": "ok", "fqdn": "_acme-challenge.example.com.", "ttl": 120, "count": 1}`
- **THEN** a DNS TXT query for `_acme-challenge.example.com.` SHALL return `token123`

#### Scenario: PUT a second distinct value appends an entry

- **WHEN** an ephemeral entry with value `token-A` already exists for `_acme-challenge.example.com.` and a PUT request is sent with body `{"value": "token-B", "ttl": 120}`
- **THEN** the API SHALL respond with HTTP 200 and body whose `count` is `2`
- **THEN** a DNS TXT query for that FQDN SHALL return both `token-A` and `token-B`

#### Scenario: PUT with the same value refreshes the existing entry

- **WHEN** an ephemeral entry with value `token-A` and 30 seconds of remaining lifetime exists for `_acme-challenge.example.com.` and a PUT request is sent with body `{"value": "token-A", "ttl": 300}`
- **THEN** the API SHALL respond with HTTP 200 and body whose `count` is `1` and `ttl` is `300`
- **THEN** the entry's Store-side expiration SHALL be extended so that a DNS TXT query at T+31 seconds still returns `token-A` (proving the refresh, whereas without refresh the original entry would have expired at T+30)

#### Scenario: TTL below minimum is clamped to 1

- **WHEN** a PUT request specifies `"ttl": 0`
- **THEN** the API SHALL store the entry with TTL 1 and respond with `"ttl": 1`

#### Scenario: TTL above maximum is clamped to 3600

- **WHEN** a PUT request specifies `"ttl": 7200`
- **THEN** the API SHALL store the entry with TTL 3600 and respond with `"ttl": 3600`

#### Scenario: Missing or invalid JSON body returns 400

- **WHEN** a PUT request has an empty body, invalid JSON, or missing `value` field
- **THEN** the API SHALL respond with HTTP 400 and a JSON error message


<!-- @trace
source: ephemeral-fixed-response-ttl
updated: 2026-04-24
code:
  - internal/ephemeral/store.go
  - internal/server/handler.go
tests:
  - test/integration/ephemeral_overrides_cname_test.go
  - internal/server/handler_ephemeral_test.go
  - internal/ephemeral/store_test.go
-->

---
### Requirement: DELETE endpoint removes all ephemeral TXT entries for an FQDN

The API SHALL accept `DELETE /v1/txt/{fqdn}`. The FQDN SHALL be canonicalized to lowercase with a trailing dot. DELETE SHALL only touch the ephemeral record store; TXT records served from zone files SHALL be unaffected. On success (including when no matching ephemeral entries exist), the API SHALL respond with HTTP 200.

DELETE SHALL support an optional `value` query-string parameter. When `value` is absent, DELETE SHALL remove every ephemeral entry for the FQDN in a single operation regardless of how many distinct values are currently stored. When `value` is present and non-empty, DELETE SHALL remove only the single entry whose stored value exactly matches the supplied value (byte-exact, case-sensitive, no normalization) and SHALL leave any other entries under the same FQDN intact. When `value` is present but empty (`?value=`), the API SHALL reject the request with HTTP 400.

When `value` is present and exceeds 255 bytes (the RFC 1035 TXT character-string limit), the API SHALL reject the request with HTTP 400 before touching the store.

#### Scenario: Delete without value selector removes every ephemeral entry for the FQDN

- **WHEN** two ephemeral entries exist for `_acme-challenge.example.com.` (values `token-A` and `token-B`) and a DELETE request is sent to `/v1/txt/_acme-challenge.example.com` with no `value` parameter
- **THEN** the API SHALL respond with HTTP 200
- **THEN** a subsequent DNS TXT query SHALL not return either ephemeral value

#### Scenario: Delete with value selector removes only the matching entry

- **WHEN** two ephemeral entries exist for `_acme-challenge.example.com.` (values `token-A` and `token-B`) and a DELETE request is sent to `/v1/txt/_acme-challenge.example.com?value=token-A`
- **THEN** the API SHALL respond with HTTP 200
- **THEN** a subsequent DNS TXT query SHALL return only `token-B`

#### Scenario: Delete with value selector that matches no entry returns 200

- **WHEN** an ephemeral entry with value `token-A` exists for `_acme-challenge.example.com.` and a DELETE request is sent to `/v1/txt/_acme-challenge.example.com?value=token-X`
- **THEN** the API SHALL respond with HTTP 200 (idempotent delete)
- **THEN** a subsequent DNS TXT query SHALL still return `token-A`

#### Scenario: Delete a non-existent record returns 200

- **WHEN** a DELETE request is sent for an FQDN that has no ephemeral record (with or without a `value` parameter)
- **THEN** the API SHALL respond with HTTP 200

#### Scenario: Delete does not affect zone file records

- **WHEN** a zone file defines a TXT record for `_acme-challenge.example.com.` with value `static-value`, an ephemeral entry with value `token-A` has been added via the API, and a DELETE request is sent for that FQDN with no `value` parameter
- **THEN** the API SHALL respond with HTTP 200
- **THEN** a subsequent DNS TXT query SHALL still return `static-value` from the zone file

#### Scenario: Delete with empty value selector returns 400

- **WHEN** a DELETE request is sent to `/v1/txt/_acme-challenge.example.com?value=`
- **THEN** the API SHALL respond with HTTP 400
- **THEN** no ephemeral entry SHALL be modified

#### Scenario: Delete with oversize value selector returns 400

- **WHEN** a DELETE request is sent with a `value` query parameter whose UTF-8 byte length exceeds 255
- **THEN** the API SHALL respond with HTTP 400
- **THEN** no ephemeral entry SHALL be modified


<!-- @trace
source: delete-ephemeral-by-value
updated: 2026-04-22
code:
  - scripts/smoke.sh
  - internal/ephemeral/store.go
  - internal/api/server.go
  - docs/ephemeral-api.md
tests:
  - cmd/shadowdns/main_ephemeral_test.go
  - internal/ephemeral/store_test.go
  - internal/api/server_test.go
-->

---
### Requirement: IP ACL enforces source IP restriction

The API server SHALL check the source IP of every request against the configured allow list. Requests from IPs not in the allow list SHALL be rejected with HTTP 403 Forbidden. The allow list SHALL support individual IP addresses and CIDR notation.

#### Scenario: Request from allowed IP is accepted

- **WHEN** a request arrives from IP `10.0.0.5` and the allow list contains `10.0.0.5`
- **THEN** the request SHALL proceed to the next authentication step

#### Scenario: Request from disallowed IP is rejected

- **WHEN** a request arrives from IP `192.168.99.1` and the allow list does not include that IP or a matching CIDR
- **THEN** the API SHALL respond with HTTP 403 Forbidden

#### Scenario: CIDR range matching

- **WHEN** a request arrives from IP `192.168.1.50` and the allow list contains `192.168.1.0/24`
- **THEN** the request SHALL be accepted


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
### Requirement: Optional token authentication

When a token is configured in the API config, the API server SHALL require an `Authorization: Bearer <token>` header on every request. Requests with a missing or incorrect token SHALL be rejected with HTTP 401 Unauthorized. When no token is configured, the API server SHALL skip token validation entirely.

#### Scenario: Valid token is accepted

- **WHEN** the config specifies `token: "secret123"` and a request includes `Authorization: Bearer secret123`
- **THEN** the request SHALL proceed

#### Scenario: Invalid token is rejected

- **WHEN** the config specifies a token and a request includes a different token value
- **THEN** the API SHALL respond with HTTP 401 Unauthorized

#### Scenario: Missing Authorization header when token is configured

- **WHEN** the config specifies a token and a request has no Authorization header
- **THEN** the API SHALL respond with HTTP 401 Unauthorized

#### Scenario: No token configured skips validation

- **WHEN** the config does not specify a token
- **THEN** requests SHALL proceed without token validation regardless of whether an Authorization header is present


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
### Requirement: Graceful shutdown of API server

The API server SHALL shut down gracefully when the main context is cancelled (SIGINT/SIGTERM). In-flight requests SHALL be given up to 5 seconds to complete before the server forcefully closes.

#### Scenario: Graceful shutdown on SIGTERM

- **WHEN** SIGTERM is sent to the ShadowDNS process while the API server is running
- **THEN** the API server SHALL stop accepting new connections and wait up to 5 seconds for in-flight requests to finish

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
### Requirement: Ephemeral TXT entries override exact CNAME at the same qname for TXT queries

An ephemeral TXT entry written via the API for a qname SHALL become visible to DNS TXT queries for that qname even when the zone contains an exact (non-wildcard) CNAME record at the same qname. While any live ephemeral entry exists for the qname, DNS TXT queries SHALL receive the ephemeral TXT RRSet and SHALL NOT receive the zone's CNAME record in the answer section. When all ephemeral entries for that qname expire or are deleted, DNS TXT queries SHALL revert to the standard RFC 1034 §3.6.2 CNAME synthesis behavior without further operator action.

This override SHALL apply only to `TXT` DNS query type. Queries of other types (e.g., `CNAME`, `A`, `AAAA`) at the same qname SHALL observe the zone's CNAME as usual and SHALL NOT be affected by the ephemeral store.

The override SHALL apply equally when the API writes an entry into a root-zone qname and when it writes an entry into a backup (alias) zone qname. The lookup key SHALL be the same qname the API caller used in the PUT request.

#### Scenario: ACME delegation qname with ephemeral TXT returns the ephemeral value to TXT queries

- **WHEN** the zone `example.com.` contains `_acme-challenge.foo.example.com. CNAME acme-dns.external.net.` AND the API PUTs a TXT entry `token-xyz` for `_acme-challenge.foo.example.com.` with `ttl` 120 AND a DNS TXT query arrives for `_acme-challenge.foo.example.com.`
- **THEN** the server SHALL respond with `_acme-challenge.foo.example.com. TXT "token-xyz"` (RR TTL 0, AA set), and SHALL NOT include the CNAME record

##### Example: ACME-01 validation path with local override

- **GIVEN** zone has `_acme-challenge.foo.example.com. CNAME acme-dns.external.net.` AND API PUTs `{"name": "_acme-challenge.foo.example.com.", "value": "token-xyz", "ttl": 120}`
- **WHEN** ACME validator runs `dig +short TXT _acme-challenge.foo.example.com. @<shadowdns>`
- **THEN** response contains `"token-xyz"` and nothing else

#### Scenario: CNAME query at the same qname still receives the zone CNAME

- **WHEN** the zone contains `_acme-challenge.foo.example.com. CNAME acme-dns.external.net.` AND a live ephemeral TXT entry exists for that qname AND a DNS CNAME query arrives for `_acme-challenge.foo.example.com.`
- **THEN** the server SHALL respond with the zone CNAME `acme-dns.external.net.` only; the ephemeral TXT entry SHALL NOT be exposed via CNAME queries

#### Scenario: TXT query falls back to CNAME behavior once ephemeral entries are gone

- **WHEN** the zone contains `_acme-challenge.foo.example.com. CNAME target.example.com.` AND the API previously had a TXT entry for `_acme-challenge.foo.example.com.` that has since expired (or been deleted) AND a DNS TXT query arrives
- **THEN** the server SHALL perform standard CNAME synthesis as if the ephemeral entry had never existed

#### Scenario: Override applies to backup (alias) zone qnames using the API's qname

- **WHEN** `backup.com.` is a backup of `example.com.` AND the root zone contains `_acme-challenge.foo.example.com. CNAME acme-dns.external.net.` AND the API PUTs a TXT entry for `_acme-challenge.foo.backup.com.` AND a DNS TXT query arrives for `_acme-challenge.foo.backup.com.`
- **THEN** the server SHALL respond with the ephemeral TXT RR owned by `_acme-challenge.foo.backup.com.`; the backup-rewritten CNAME SHALL NOT be emitted

#### Scenario: Ephemeral TXT response carries TTL 0 to suppress downstream caching

- **WHEN** the API PUTs a TXT entry for any in-bailiwick qname with any `ttl` body value AND a DNS TXT query arrives for that qname
- **THEN** the server SHALL emit the ephemeral TXT RR with RR header TTL set to 0, regardless of the `ttl` body value used in the PUT request

##### Example: store-side ttl 120 still produces RR TTL 0

- **GIVEN** API PUTs `{"value": "token-A", "ttl": 120}` for `_acme-challenge.example.com.`
- **WHEN** a client runs `dig +noall +answer TXT _acme-challenge.example.com. @<shadowdns>`
- **THEN** the answer line shows TTL 0, e.g. `_acme-challenge.example.com. 0 IN TXT "token-A"`

---
### Requirement: PUT rejects FQDNs outside every configured zone

The API SHALL reject `PUT /v1/txt/{fqdn}` requests whose canonical FQDN is not in-bailiwick of at least one loaded zone origin. The check SHALL consider both root zones and backup zones, across all views, and SHALL use the current zone origin snapshot so that zones added or removed by a SIGHUP reload take effect on the next request without requiring the API server to restart.

An FQDN SHALL be considered in-bailiwick when it equals a loaded zone origin or is a subdomain of a loaded zone origin, after canonicalization (lowercase, trailing dot). Zone membership SHALL be evaluated regardless of which view, if any, the caller's source IP would match for DNS queries.

When the FQDN is not in-bailiwick of any loaded zone, the API SHALL respond with HTTP 422 Unprocessable Entity and a JSON body in the existing `{"status": "error", "error": "<message>"}` shape. The error message SHALL identify the rejected FQDN. The ephemeral record store SHALL NOT be modified for rejected requests.

The zone-membership check SHALL run after IP ACL, token authentication, FQDN canonicalization, JSON parsing, value validation, and TTL clamping have all passed, but before the ephemeral store is written. DELETE requests SHALL NOT be subject to this check and SHALL retain their existing idempotent semantics regardless of whether the FQDN is in-bailiwick of any loaded zone.

#### Scenario: PUT for FQDN outside every loaded zone returns 422

- **WHEN** ShadowDNS is running with only `example.com.` loaded as a root zone and a PUT request is sent to `/v1/txt/_acme-challenge.exmaple.com` with body `{"value":"test123","ttl":30}`
- **THEN** the API SHALL respond with HTTP 422 and a JSON body whose `status` field is `"error"`
- **THEN** no ephemeral entry SHALL be created under `_acme-challenge.exmaple.com.`

##### Example: typo rejection

- **GIVEN** loaded zone origins: `example.com.`
- **WHEN** PUT `/v1/txt/_acme-challenge.exmaple.com` with `{"value":"test123","ttl":30}`
- **THEN** response status 422 and body contains `"error"` field naming `_acme-challenge.exmaple.com.`

#### Scenario: PUT for FQDN inside a loaded root zone succeeds

- **WHEN** `example.com.` is loaded as a root zone and a PUT request is sent to `/v1/txt/_acme-challenge.example.com` with body `{"value":"token123","ttl":120}`
- **THEN** the API SHALL respond with HTTP 200 and the standard PUT response body
- **THEN** the ephemeral store SHALL contain an entry for `_acme-challenge.example.com.`

#### Scenario: PUT for FQDN inside a loaded backup zone succeeds

- **WHEN** `backup.com.` is loaded as a backup-override zone and a PUT request is sent to `/v1/txt/_acme-challenge.foo.backup.com` with body `{"value":"token-B","ttl":120}`
- **THEN** the API SHALL respond with HTTP 200 and the standard PUT response body
- **THEN** the ephemeral store SHALL contain an entry for `_acme-challenge.foo.backup.com.`

#### Scenario: PUT for FQDN equal to a zone origin succeeds

- **WHEN** `example.com.` is loaded as a root zone and a PUT request is sent to `/v1/txt/example.com` with body `{"value":"apex","ttl":120}`
- **THEN** the API SHALL respond with HTTP 200
- **THEN** the ephemeral store SHALL contain an entry for `example.com.`

#### Scenario: Zone added via SIGHUP reload becomes acceptable on the next PUT

- **WHEN** a PUT to `/v1/txt/foo.newzone.com` would have returned 422 because `newzone.com.` was not loaded, and then SIGHUP reload loads `newzone.com.` as a root zone, and then the same PUT is retried
- **THEN** the retried PUT SHALL respond with HTTP 200
- **THEN** the ephemeral store SHALL contain an entry for `foo.newzone.com.`

#### Scenario: Zone removed via SIGHUP reload becomes unacceptable on the next PUT

- **WHEN** `example.com.` was loaded and then SIGHUP reload removes it, and then a PUT request is sent to `/v1/txt/_acme-challenge.example.com`
- **THEN** the API SHALL respond with HTTP 422
- **THEN** no ephemeral entry SHALL be created

#### Scenario: DELETE is not subject to the zone-membership check

- **WHEN** a DELETE request is sent to `/v1/txt/_acme-challenge.exmaple.com` and `exmaple.com.` is not loaded as any zone
- **THEN** the API SHALL respond with HTTP 200 (idempotent delete)
- **THEN** the ephemeral store SHALL remain unchanged

#### Scenario: Zone-membership check runs after existing validations

- **WHEN** a PUT request has a well-formed body with an in-bailiwick FQDN but oversize value (>255 bytes)
- **THEN** the API SHALL respond with HTTP 400 for the oversize value, not HTTP 422

- **WHEN** a PUT request has a well-formed body with an out-of-bailiwick FQDN but is rejected by the IP ACL
- **THEN** the API SHALL respond with HTTP 403, not HTTP 422

<!-- @trace
source: ephemeral-api-reject-unknown-zone
updated: 2026-04-23
-->

<!-- @trace
source: ephemeral-api-reject-unknown-zone
updated: 2026-04-23
code:
  - CHANGELOG.md
  - docs/ephemeral-api.md
  - internal/api/server.go
  - .release-please-manifest.json
  - cmd/shadowdns/main.go
  - internal/server/server.go
tests:
  - cmd/shadowdns/main_ephemeral_test.go
  - internal/api/server_test.go
-->
