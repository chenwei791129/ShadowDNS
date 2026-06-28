## MODIFIED Requirements

### Requirement: TLS certificate is obtained for an IP address via ACME HTTP-01

When the `doh` section is present, the DoH server SHALL obtain its TLS certificate for the `doh.acme.ip` address from the `doh.acme.directory_url` ACME directory using the HTTP-01 challenge and the certificate profile required for IP-address certificates. The ACME HTTP-01 challenge SHALL be served on a dedicated HTTP listener bound to `doh.acme.http01_listen`. Because ACME HTTP-01 validation always connects to port 80, `doh.acme.http01_listen` MUST be reachable from the public Internet as port 80. The DoH HTTPS service on the configured `doh.listen` port SHALL NOT be required to be reachable by the ACME server.

Because the HTTP-01 listener is fully public and its only legitimate traffic is a GET for a currently-presented challenge token, the listener SHALL reduce its attack surface by responding only to that one legitimate request shape and aborting every other connection without an HTTP response (the behavior of nginx `return 444`). Specifically, the listener SHALL respond with HTTP 200 and the matching key authorization only when ALL of the following hold: the request method is GET, the request path is under `/.well-known/acme-challenge/`, and the trailing token matches a key authorization that is currently presented. For every other request — any path outside `/.well-known/acme-challenge/`, a path under `/.well-known/acme-challenge/` whose token is unknown or empty, or a request to any matching path using a method other than GET — the listener SHALL abort the connection without sending any HTTP response (no status line, no headers, no body), and SHALL NOT emit a stack trace for the aborted request. The listener SHALL NOT rely on a redirect for any request (in particular it SHALL NOT redirect `/.well-known/acme-challenge` without a trailing slash), so that no aborted request shape produces an observable HTTP response.

Each aborted request SHALL increment the `shadowdns_doh_acme_dropped_total` counter (Prometheus namespace `shadowdns`, subsystem `doh`) by one, labeled by `reason` with exactly one of the bounded values `unknown_path` (path outside `/.well-known/acme-challenge/`), `unknown_token` (path under `/.well-known/acme-challenge/` with an unknown or empty token), or `bad_method` (a non-GET method on an otherwise-matching path). Aborting the HTTP-01 listener's connection SHALL NOT increment `shadowdns_panics_total`.

The ACME account SHALL be registered without a contact email. The `doh.acme` configuration SHALL NOT accept an email field, and configuration loading SHALL NOT require one. Consistent with the strict `KnownFields(true)` decoding, a `doh.acme.email` key present in the configuration SHALL be rejected as an unknown field.

#### Scenario: ACME challenge is served on the HTTP-01 listener

- **WHEN** the ACME server sends a GET request to `http://203.0.113.10/.well-known/acme-challenge/<token>` during validation, the token matches a currently-presented key authorization, and `doh.acme.http01_listen` is bound such that the request reaches it on port 80
- **THEN** the HTTP-01 listener SHALL respond with HTTP 200 and the matching challenge key authorization, and SHALL NOT increment `shadowdns_doh_acme_dropped_total`

#### Scenario: Non-challenge path aborts the connection

- **WHEN** a GET request is sent to any path outside `/.well-known/acme-challenge/` on the HTTP-01 listener (for example `http://203.0.113.10/`)
- **THEN** the HTTP-01 listener SHALL abort the connection without sending any HTTP response, and SHALL increment `shadowdns_doh_acme_dropped_total{reason="unknown_path"}` by one

#### Scenario: Unknown challenge token aborts the connection

- **WHEN** a GET request is sent to a path under `/.well-known/acme-challenge/` whose trailing token is empty or does not match any currently-presented key authorization
- **THEN** the HTTP-01 listener SHALL abort the connection without sending any HTTP response, and SHALL increment `shadowdns_doh_acme_dropped_total{reason="unknown_token"}` by one

#### Scenario: Non-GET method on a challenge path aborts the connection

- **WHEN** a request using a method other than GET (for example POST) is sent to a path under `/.well-known/acme-challenge/` on the HTTP-01 listener
- **THEN** the HTTP-01 listener SHALL abort the connection without sending any HTTP response, and SHALL increment `shadowdns_doh_acme_dropped_total{reason="bad_method"}` by one

#### Scenario: Trailing-slash-less challenge base path does not redirect

- **WHEN** a GET request is sent to `http://203.0.113.10/.well-known/acme-challenge` (no trailing slash) on the HTTP-01 listener
- **THEN** the HTTP-01 listener SHALL abort the connection without sending any HTTP response (in particular SHALL NOT return an HTTP 301 redirect), and SHALL increment `shadowdns_doh_acme_dropped_total{reason="unknown_token"}` by one

#### Scenario: ACME account is registered without an email

- **WHEN** ShadowDNS is started with a `doh.acme` section that contains no `email` field and obtains a certificate
- **THEN** configuration loading SHALL succeed without an email field and the ACME account SHALL be registered with no contact

#### Scenario: An email field in the configuration is rejected

- **WHEN** ShadowDNS is started with a `--config` file whose `doh.acme` section includes an `email` key
- **THEN** configuration loading SHALL fail naming the unknown `email` field, and no DoH server SHALL be started
