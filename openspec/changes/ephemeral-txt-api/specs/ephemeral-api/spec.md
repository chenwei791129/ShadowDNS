## ADDED Requirements

### Requirement: HTTP API server listens on a configured address

The API server SHALL listen on the address specified in the API configuration file (e.g., `127.0.0.1:8053`). The server SHALL start only when the `-api-conf` CLI flag is provided. When the flag is absent, no API server SHALL be started.

#### Scenario: API server starts on configured address

- **WHEN** ShadowDNS is started with `-api-conf /etc/shadowdns/api.yaml` and the config specifies `listen: "127.0.0.1:8053"`
- **THEN** the API server SHALL accept HTTP connections on `127.0.0.1:8053`

#### Scenario: API server is not started when flag is absent

- **WHEN** ShadowDNS is started without the `-api-conf` flag
- **THEN** no HTTP API server SHALL be started and no API port SHALL be bound

### Requirement: PUT endpoint creates or updates an ephemeral TXT record

The API SHALL accept `PUT /v1/txt/{fqdn}` with a JSON body containing `value` (string) and `ttl` (integer, seconds). The FQDN path parameter SHALL be canonicalized to lowercase with a trailing dot. The TTL SHALL be clamped to the range [1, 3600]. On success, the API SHALL respond with HTTP 200 and a JSON body confirming the operation.

#### Scenario: Create a new ephemeral TXT record

- **WHEN** a PUT request is sent to `/v1/txt/_acme-challenge.example.com` with body `{"value": "token123", "ttl": 120}`
- **THEN** the API SHALL respond with HTTP 200 and body `{"status": "ok", "fqdn": "_acme-challenge.example.com.", "ttl": 120}`
- **THEN** a DNS TXT query for `_acme-challenge.example.com.` SHALL return `token123`

#### Scenario: Update an existing ephemeral TXT record

- **WHEN** an ephemeral TXT record for `_acme-challenge.example.com.` already exists and a PUT request is sent with a new value
- **THEN** the API SHALL overwrite the existing record and respond with HTTP 200

#### Scenario: TTL below minimum is clamped to 1

- **WHEN** a PUT request specifies `"ttl": 0`
- **THEN** the API SHALL store the record with TTL 1 and respond with `"ttl": 1`

#### Scenario: TTL above maximum is clamped to 3600

- **WHEN** a PUT request specifies `"ttl": 7200`
- **THEN** the API SHALL store the record with TTL 3600 and respond with `"ttl": 3600`

#### Scenario: Missing or invalid JSON body returns 400

- **WHEN** a PUT request has an empty body, invalid JSON, or missing `value` field
- **THEN** the API SHALL respond with HTTP 400 and a JSON error message

### Requirement: DELETE endpoint removes an ephemeral TXT record

The API SHALL accept `DELETE /v1/txt/{fqdn}`. The FQDN SHALL be canonicalized to lowercase with a trailing dot. On success (including when the record does not exist), the API SHALL respond with HTTP 200.

#### Scenario: Delete an existing ephemeral TXT record

- **WHEN** a DELETE request is sent to `/v1/txt/_acme-challenge.example.com` and the record exists
- **THEN** the API SHALL remove the record and respond with HTTP 200

#### Scenario: Delete a non-existent record returns 200

- **WHEN** a DELETE request is sent for an FQDN that has no ephemeral record
- **THEN** the API SHALL respond with HTTP 200 (idempotent delete)

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

### Requirement: Graceful shutdown of API server

The API server SHALL shut down gracefully when the main context is cancelled (SIGINT/SIGTERM). In-flight requests SHALL be given up to 5 seconds to complete before the server forcefully closes.

#### Scenario: Graceful shutdown on SIGTERM

- **WHEN** SIGTERM is sent to the ShadowDNS process while the API server is running
- **THEN** the API server SHALL stop accepting new connections and wait up to 5 seconds for in-flight requests to finish
