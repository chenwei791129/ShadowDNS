## ADDED Requirements

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
