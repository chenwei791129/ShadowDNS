## MODIFIED Requirements

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

### Requirement: PUT endpoint adds or refreshes an ephemeral TXT value

The API SHALL accept `PUT /v1/txt/{fqdn}` with a JSON body containing `value` (string) and `ttl` (integer, seconds). The FQDN path parameter SHALL be canonicalized to lowercase with a trailing dot. The TTL SHALL be clamped to the range [1, 3600]. The `value` field SHALL be validated to be at most 255 UTF-8 bytes in length (the RFC 1035 TXT character-string limit); PUT requests with a longer value SHALL be rejected with HTTP 400 before touching the store. On success, the API SHALL respond with HTTP 200 and a JSON body confirming the operation.

PUT SHALL support multiple distinct values per FQDN. When the posted value does not match any existing entry under the FQDN, the API SHALL append a new entry. When the posted value matches an existing entry exactly, the API SHALL refresh that entry's expiration using the new TTL instead of creating a duplicate. The operation SHALL remain idempotent: two consecutive identical PUT calls SHALL produce the same final state as a single call.

The response body SHALL include the canonical FQDN, the clamped TTL applied to the affected entry, and the total number of ephemeral entries currently held for that FQDN.

#### Scenario: Create a new ephemeral TXT record

- **WHEN** a PUT request is sent to `/v1/txt/_acme-challenge.example.com` with body `{"value": "token123", "ttl": 120}` and no prior entries exist for that FQDN
- **THEN** the API SHALL respond with HTTP 200 and body `{"status": "ok", "fqdn": "_acme-challenge.example.com.", "ttl": 120, "count": 1}`
- **THEN** a DNS TXT query for `_acme-challenge.example.com.` SHALL return `token123`

#### Scenario: PUT a second distinct value appends an entry

- **WHEN** an ephemeral entry with value `token-A` already exists for `_acme-challenge.example.com.` and a PUT request is sent with body `{"value": "token-B", "ttl": 120}`
- **THEN** the API SHALL respond with HTTP 200 and body whose `count` is `2`
- **THEN** a DNS TXT query for that FQDN SHALL return both `token-A` and `token-B`

#### Scenario: PUT with the same value refreshes the existing entry

- **WHEN** an ephemeral entry with value `token-A` and remaining TTL 30 exists for `_acme-challenge.example.com.` and a PUT request is sent with body `{"value": "token-A", "ttl": 300}`
- **THEN** the API SHALL respond with HTTP 200 and body whose `count` is `1`
- **THEN** a subsequent DNS TXT query SHALL return `token-A` with TTL approximately 300 (not 30)

#### Scenario: TTL below minimum is clamped to 1

- **WHEN** a PUT request specifies `"ttl": 0`
- **THEN** the API SHALL store the entry with TTL 1 and respond with `"ttl": 1`

#### Scenario: TTL above maximum is clamped to 3600

- **WHEN** a PUT request specifies `"ttl": 7200`
- **THEN** the API SHALL store the entry with TTL 3600 and respond with `"ttl": 3600`

#### Scenario: Missing or invalid JSON body returns 400

- **WHEN** a PUT request has an empty body, invalid JSON, or missing `value` field
- **THEN** the API SHALL respond with HTTP 400 and a JSON error message

#### Scenario: PUT with oversize value returns 400

- **WHEN** a PUT request body contains a `value` field whose UTF-8 byte length exceeds 255
- **THEN** the API SHALL respond with HTTP 400
- **THEN** no ephemeral entry SHALL be created or modified
