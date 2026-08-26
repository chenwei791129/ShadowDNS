## MODIFIED Requirements

### Requirement: PUT endpoint adds or refreshes an ephemeral TXT value

The API SHALL accept `PUT /v1/txt/{fqdn}` with a JSON body containing exactly named `value` (string) and `ttl` (integer, seconds) members. JSON object member matching SHALL be case-sensitive. The API MUST reject malformed JSON, unknown members, case-mismatched members, duplicate object member names, invalid UTF-8, and bodies containing more than one top-level JSON value with HTTP 400 before touching the ephemeral store. The FQDN path parameter SHALL be canonicalized to lowercase with a trailing dot. The TTL SHALL be clamped to the range [1, 3600]. The `value` field SHALL be validated to be at most 255 UTF-8 bytes in length (the RFC 1035 TXT character-string limit); PUT requests with a longer value SHALL be rejected with HTTP 400 before touching the store. On success, the API SHALL respond with HTTP 200 and a JSON body confirming the operation.

The `ttl` field in the PUT body SHALL control only the Store-side lifespan of the entry (its expiration timestamp). It SHALL NOT influence the TTL value written into DNS response packets; DNS response TTL is fixed by the `dns-server` spec.

PUT SHALL support multiple distinct values per FQDN. When the posted value does not match any existing entry under the FQDN, the API SHALL append a new entry. When the posted value matches an existing entry exactly, the API SHALL refresh that entry's expiration using the new TTL instead of creating a duplicate. The operation SHALL remain idempotent: two consecutive identical PUT calls SHALL produce the same final state as a single call.

The response body SHALL include the canonical FQDN, the clamped TTL applied to the affected entry (the Store-side lifespan value), and the total number of ephemeral entries currently held for that FQDN. JSON response member names, types, and decoded values SHALL remain stable; object member ordering and non-semantic escaping SHALL NOT be constrained. The response body SHALL end with exactly one newline.

#### Scenario: Create a new ephemeral TXT record

- **WHEN** a PUT request is sent to `/v1/txt/_acme-challenge.example.com` with body `{"value": "token123", "ttl": 120}` and no prior entries exist for that FQDN
- **THEN** the API SHALL respond with HTTP 200 and a JSON body equivalent to `{"status": "ok", "fqdn": "_acme-challenge.example.com.", "ttl": 120, "count": 1}` followed by one newline
- **THEN** a DNS TXT query for `_acme-challenge.example.com.` SHALL return `token123`

#### Scenario: PUT a second distinct value appends an entry

- **WHEN** an ephemeral entry with value `token-A` already exists for `_acme-challenge.example.com.` and a PUT request is sent with body `{"value": "token-B", "ttl": 120}`
- **THEN** the API SHALL respond with HTTP 200 and body whose `count` is `2`
- **THEN** a DNS TXT query for that FQDN SHALL return both `token-A` and `token-B`

#### Scenario: PUT with the same value refreshes the existing entry

- **WHEN** an ephemeral entry with value `token-A` and 30 seconds of remaining lifetime exists for `_acme-challenge.example.com.` and a PUT request is sent with body `{"value": "token-A", "ttl": 300}`
- **THEN** the API SHALL respond with HTTP 200 and body whose `count` is `1` and `ttl` is `300`
- **THEN** the entry's Store-side expiration SHALL be extended so that a DNS TXT query at T+31 seconds still returns `token-A`

#### Scenario: TTL below minimum is clamped to 1

- **WHEN** a PUT request specifies `"ttl": 0`
- **THEN** the API SHALL store the entry with TTL 1 and respond with `"ttl": 1`

#### Scenario: TTL above maximum is clamped to 3600

- **WHEN** a PUT request specifies `"ttl": 7200`
- **THEN** the API SHALL store the entry with TTL 3600 and respond with `"ttl": 3600`

#### Scenario: Missing or invalid JSON body returns 400

- **WHEN** a PUT request has an empty body, invalid JSON, or a missing `value` field
- **THEN** the API SHALL respond with HTTP 400 and a JSON error message
- **THEN** the ephemeral store SHALL remain unchanged

#### Scenario: Strict JSON input rejects ambiguous or non-canonical objects

- **WHEN** a PUT body contains a case-mismatched member, unknown member, duplicate member name, invalid UTF-8, or a second top-level JSON value
- **THEN** the API SHALL respond with HTTP 400 in the existing `status=error` JSON shape
- **THEN** the ephemeral store SHALL remain unchanged

##### Example: Strict PUT body acceptance

| Input | Result |
| ----- | ------ |
| `{"value":"token","ttl":60}` | HTTP 200 |
| `{"Value":"token","TTL":60}` | HTTP 400 |
| `{"value":"token","ttl":60,"extra":true}` | HTTP 400 |
| `{"value":"first","value":"second","ttl":60}` | HTTP 400 |
| `{"value":"first","ttl":60} {"value":"second","ttl":60}` | HTTP 400 |
| JSON string containing invalid UTF-8 | HTTP 400 |
