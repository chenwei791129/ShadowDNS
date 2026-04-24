## MODIFIED Requirements

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

### Requirement: Ephemeral TXT entries override exact CNAME at the same qname for TXT queries

An ephemeral TXT entry written via the API for a qname SHALL become visible to DNS TXT queries for that qname even when the zone contains an exact (non-wildcard) CNAME record at the same qname. While any live ephemeral entry exists for the qname, DNS TXT queries SHALL receive the ephemeral TXT RRSet and SHALL NOT receive the zone's CNAME record in the answer section. When all ephemeral entries for that qname expire or are deleted, DNS TXT queries SHALL revert to the standard RFC 1034 §3.6.2 CNAME synthesis behavior without further operator action.

This override SHALL apply only to `TXT` DNS query type. Queries of other types (e.g., `CNAME`, `A`, `AAAA`) at the same qname SHALL observe the zone's CNAME as usual and SHALL NOT be affected by the ephemeral store.

The override SHALL apply equally when the API writes an entry into a root-zone qname and when it writes an entry into a backup (alias) zone qname. The lookup key SHALL be the same qname the API caller used in the PUT request.

#### Scenario: ACME delegation qname with ephemeral TXT returns the ephemeral value to TXT queries

- **WHEN** the zone `example.com.` contains `_acme-challenge.foo.example.com. CNAME acme-dns.external.net.` AND the API PUTs a TXT entry `token-xyz` for `_acme-challenge.foo.example.com.` with `ttl` 120 AND a DNS TXT query arrives for `_acme-challenge.foo.example.com.`
- **THEN** the server SHALL respond with `_acme-challenge.foo.example.com. TXT "token-xyz"` (RR TTL 30, AA set), and SHALL NOT include the CNAME record

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
