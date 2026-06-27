# doh-endpoint Specification

## Purpose

TBD - created by archiving change 'add-doh-endpoint'. Update Purpose after archive.

## Requirements

### Requirement: DoH server listens on a configured address

The DoH server SHALL listen for HTTPS connections on the address specified in the `doh.listen` field of the unified ShadowDNS YAML configuration loaded via `--config`. The server SHALL start only when the `doh` section is present in the configuration. When the section is absent, no DoH server, no ACME client, and no ACME HTTP-01 listener SHALL be started.

The `doh` section SHALL use the following YAML fields. Loading SHALL fail with an error naming the missing field when any required field is absent (consistent with the existing strict `KnownFields(true)` decoding):

| Field | Required | Meaning |
|-------|----------|---------|
| `doh.listen` | yes | host:port the DoH HTTPS service binds (for example `203.0.113.10:443`) |
| `doh.acme.directory_url` | yes | ACME directory URL of the issuing CA |
| `doh.acme.ip` | yes | the IP address the certificate is issued for |
| `doh.acme.http01_listen` | yes | host:port the ACME HTTP-01 challenge responder binds; MUST be reachable from the public Internet as port 80 |

#### Scenario: DoH server starts on configured address

- **WHEN** ShadowDNS is started with a `--config` file containing a `doh` section whose `listen` is `203.0.113.10:443` and all required `doh.acme.*` fields are present
- **THEN** the DoH server SHALL accept HTTPS connections on `203.0.113.10:443`

#### Scenario: DoH server is not started when the doh section is absent

- **WHEN** ShadowDNS is started with a `--config` file that omits the `doh` section
- **THEN** no DoH HTTPS server SHALL be started, no port SHALL be bound for DoH, and no ACME HTTP-01 listener SHALL be started

#### Scenario: Missing required doh field fails the load

- **WHEN** ShadowDNS is started with a `--config` file whose `doh` section omits the required `acme.ip` field
- **THEN** configuration loading SHALL fail with an error naming the missing `acme.ip` field, and no DoH server SHALL be started


<!-- @trace
source: add-doh-endpoint
updated: 2026-06-27
code:
  - mkdocs.yml
  - internal/doh/acme.go
  - internal/metrics/metrics.go
  - docs/index.zh.md
  - README.md
  - docs/guides/doh.zh.md
  - internal/doh/responsewriter.go
  - go.sum
  - docs/configuration/shadowdns-yaml.md
  - internal/shadowdnscfg/config.go
  - go.mod
  - docs/index.md
  - packaging/shadowdns.yaml.example
  - docs/guides/doh.md
  - cmd/shadowdns/main.go
  - internal/server/handler.go
  - internal/doh/server.go
  - docs/configuration/shadowdns-yaml.zh.md
tests:
  - internal/doh/server_run_test.go
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/acme_integration_test.go
  - internal/metrics/metrics_doh_test.go
  - internal/shadowdnscfg/doh_test.go
  - internal/doh/server_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/doh/acme_test.go
  - internal/doh/responsewriter_test.go
  - internal/doh/helpers_test.go
  - internal/doh/metrics_test.go
-->

---
### Requirement: DoH endpoint implements RFC 8484 GET and POST

The DoH server SHALL serve requests at the path `/dns-query` using both the GET and POST methods. For GET, the DNS query SHALL be read from the `dns` query-string parameter, base64url-encoded with padding removed. For POST, the DNS query SHALL be read from the request body. In both cases the request and response media type SHALL be `application/dns-message` (raw DNS wire format). Requests to any path other than `/dns-query` SHALL receive HTTP 404. Requests to `/dns-query` using a method other than GET or POST SHALL receive HTTP 405.

#### Scenario: POST query returns a wire-format response

- **WHEN** a POST request is sent to `/dns-query` with `Content-Type: application/dns-message` and a body containing a wire-format query for an A record that exists in a loaded zone
- **THEN** the server SHALL respond with HTTP 200, `Content-Type: application/dns-message`, and a wire-format DNS response containing the answer

#### Scenario: GET query with base64url dns parameter returns a response

- **WHEN** a GET request is sent to `/dns-query?dns=<base64url-no-padding>` where the decoded bytes are a wire-format query for a record in a loaded zone
- **THEN** the server SHALL respond with HTTP 200 and a wire-format DNS response containing the answer

##### Example: RFC 8484 base64url GET encoding

- **GIVEN** the canonical RFC 8484 §4.1.1 encoding of an `A` query for `www.example.com`
- **WHEN** a GET request is sent to `/dns-query?dns=AAABAAABAAAAAAAAA3d3dwdleGFtcGxlA2NvbQAAAQAB` (base64url, no padding, no `=`/`+`/`/` characters)
- **THEN** the server SHALL decode it to a query for `www.example.com. A` and respond with HTTP 200 and `Content-Type: application/dns-message`

#### Scenario: Unknown path returns 404

- **WHEN** a GET request is sent to `/resolve?dns=<base64url>`
- **THEN** the server SHALL respond with HTTP 404

#### Scenario: Unsupported method returns 405

- **WHEN** a PUT request is sent to `/dns-query`
- **THEN** the server SHALL respond with HTTP 405


<!-- @trace
source: add-doh-endpoint
updated: 2026-06-27
code:
  - mkdocs.yml
  - internal/doh/acme.go
  - internal/metrics/metrics.go
  - docs/index.zh.md
  - README.md
  - docs/guides/doh.zh.md
  - internal/doh/responsewriter.go
  - go.sum
  - docs/configuration/shadowdns-yaml.md
  - internal/shadowdnscfg/config.go
  - go.mod
  - docs/index.md
  - packaging/shadowdns.yaml.example
  - docs/guides/doh.md
  - cmd/shadowdns/main.go
  - internal/server/handler.go
  - internal/doh/server.go
  - docs/configuration/shadowdns-yaml.zh.md
tests:
  - internal/doh/server_run_test.go
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/acme_integration_test.go
  - internal/metrics/metrics_doh_test.go
  - internal/shadowdnscfg/doh_test.go
  - internal/doh/server_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/doh/acme_test.go
  - internal/doh/responsewriter_test.go
  - internal/doh/helpers_test.go
  - internal/doh/metrics_test.go
-->

---
### Requirement: DoH reuses the authoritative query path and is non-recursive

DoH queries SHALL be answered by the same authoritative query-handling path as UDP and TCP queries. For any given query, the DNS response delivered over DoH SHALL be byte-for-byte equal to the response the same query would receive over TCP after DoH-specific transport framing is removed (same RCODE, same answer records, same authoritative semantics). The DoH server SHALL NOT perform recursive resolution: a query for a name outside every loaded zone SHALL receive RCODE REFUSED (the same code the existing UDP/TCP path returns for an out-of-bailiwick qname), never a recursively resolved answer.

#### Scenario: In-zone query over DoH matches the TCP answer

- **WHEN** the loaded zone `example.com.` contains `www.example.com. 300 IN A 203.0.113.20` and the same `A` query is issued over DoH and over TCP
- **THEN** both responses SHALL have RCODE NOERROR (0) and an Answer section containing exactly `www.example.com. 300 IN A 203.0.113.20`, and the wire-format answer bytes SHALL be equal

#### Scenario: Out-of-zone query is refused, not recursively resolved

- **WHEN** a DoH query is issued for `www.example.net.` and no loaded zone is authoritative for `example.net.`
- **THEN** the server SHALL respond with RCODE REFUSED (5) and an empty Answer section, and SHALL NOT return a recursively resolved answer


<!-- @trace
source: add-doh-endpoint
updated: 2026-06-27
code:
  - mkdocs.yml
  - internal/doh/acme.go
  - internal/metrics/metrics.go
  - docs/index.zh.md
  - README.md
  - docs/guides/doh.zh.md
  - internal/doh/responsewriter.go
  - go.sum
  - docs/configuration/shadowdns-yaml.md
  - internal/shadowdnscfg/config.go
  - go.mod
  - docs/index.md
  - packaging/shadowdns.yaml.example
  - docs/guides/doh.md
  - cmd/shadowdns/main.go
  - internal/server/handler.go
  - internal/doh/server.go
  - docs/configuration/shadowdns-yaml.zh.md
tests:
  - internal/doh/server_run_test.go
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/acme_integration_test.go
  - internal/metrics/metrics_doh_test.go
  - internal/shadowdnscfg/doh_test.go
  - internal/doh/server_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/doh/acme_test.go
  - internal/doh/responsewriter_test.go
  - internal/doh/helpers_test.go
  - internal/doh/metrics_test.go
-->

---
### Requirement: DoH response cache headers are bounded by the minimum answer TTL

The DoH server SHALL set the HTTP response freshness (for example via `Cache-Control: max-age`) to a value less than or equal to the smallest TTL among the records in the DNS response Answer section. When the Answer section is empty, the server SHALL NOT advertise a positive cache lifetime.

#### Scenario: max-age does not exceed the smallest answer TTL

- **WHEN** a DoH query returns an Answer section whose records have TTLs 300 and 60
- **THEN** the response `Cache-Control: max-age` value SHALL be less than or equal to 60

##### Example: TTL-bounded caching

| Answer TTLs | max-age upper bound |
|-------------|---------------------|
| 300, 60     | 60                  |
| 0           | 0                   |
| (empty)     | no positive max-age |


<!-- @trace
source: add-doh-endpoint
updated: 2026-06-27
code:
  - mkdocs.yml
  - internal/doh/acme.go
  - internal/metrics/metrics.go
  - docs/index.zh.md
  - README.md
  - docs/guides/doh.zh.md
  - internal/doh/responsewriter.go
  - go.sum
  - docs/configuration/shadowdns-yaml.md
  - internal/shadowdnscfg/config.go
  - go.mod
  - docs/index.md
  - packaging/shadowdns.yaml.example
  - docs/guides/doh.md
  - cmd/shadowdns/main.go
  - internal/server/handler.go
  - internal/doh/server.go
  - docs/configuration/shadowdns-yaml.zh.md
tests:
  - internal/doh/server_run_test.go
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/acme_integration_test.go
  - internal/metrics/metrics_doh_test.go
  - internal/shadowdnscfg/doh_test.go
  - internal/doh/server_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/doh/acme_test.go
  - internal/doh/responsewriter_test.go
  - internal/doh/helpers_test.go
  - internal/doh/metrics_test.go
-->

---
### Requirement: Malformed DoH requests are rejected with HTTP 400

The DoH server SHALL reject requests it cannot decode into a DNS message with HTTP 400, without invoking the query path. This includes a GET request whose `dns` parameter is missing or is not valid base64url, and a POST request with an empty or undecodable body.

#### Scenario: GET with invalid base64url returns 400

- **WHEN** a GET request is sent to `/dns-query?dns=!!!notbase64!!!`
- **THEN** the server SHALL respond with HTTP 400

#### Scenario: POST with empty body returns 400

- **WHEN** a POST request is sent to `/dns-query` with an empty body
- **THEN** the server SHALL respond with HTTP 400


<!-- @trace
source: add-doh-endpoint
updated: 2026-06-27
code:
  - mkdocs.yml
  - internal/doh/acme.go
  - internal/metrics/metrics.go
  - docs/index.zh.md
  - README.md
  - docs/guides/doh.zh.md
  - internal/doh/responsewriter.go
  - go.sum
  - docs/configuration/shadowdns-yaml.md
  - internal/shadowdnscfg/config.go
  - go.mod
  - docs/index.md
  - packaging/shadowdns.yaml.example
  - docs/guides/doh.md
  - cmd/shadowdns/main.go
  - internal/server/handler.go
  - internal/doh/server.go
  - docs/configuration/shadowdns-yaml.zh.md
tests:
  - internal/doh/server_run_test.go
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/acme_integration_test.go
  - internal/metrics/metrics_doh_test.go
  - internal/shadowdnscfg/doh_test.go
  - internal/doh/server_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/doh/acme_test.go
  - internal/doh/responsewriter_test.go
  - internal/doh/helpers_test.go
  - internal/doh/metrics_test.go
-->

---
### Requirement: DoH server enforces request size and timeout limits

The DoH HTTPS server and the ACME HTTP-01 listener SHALL each set read and idle timeouts on their connections. The DoH server SHALL cap the accepted request body size and SHALL reject a POST whose body exceeds the cap with HTTP 413 (Payload Too Large) without invoking the query path. A DNS message cannot exceed 65535 bytes, so the cap SHALL NOT be smaller than 65535 bytes.

#### Scenario: Oversize POST body is rejected

- **WHEN** a POST request is sent to `/dns-query` with a body larger than the configured request-body cap
- **THEN** the server SHALL respond with HTTP 413 and SHALL NOT invoke the query path


<!-- @trace
source: add-doh-endpoint
updated: 2026-06-27
code:
  - mkdocs.yml
  - internal/doh/acme.go
  - internal/metrics/metrics.go
  - docs/index.zh.md
  - README.md
  - docs/guides/doh.zh.md
  - internal/doh/responsewriter.go
  - go.sum
  - docs/configuration/shadowdns-yaml.md
  - internal/shadowdnscfg/config.go
  - go.mod
  - docs/index.md
  - packaging/shadowdns.yaml.example
  - docs/guides/doh.md
  - cmd/shadowdns/main.go
  - internal/server/handler.go
  - internal/doh/server.go
  - docs/configuration/shadowdns-yaml.zh.md
tests:
  - internal/doh/server_run_test.go
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/acme_integration_test.go
  - internal/metrics/metrics_doh_test.go
  - internal/shadowdnscfg/doh_test.go
  - internal/doh/server_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/doh/acme_test.go
  - internal/doh/responsewriter_test.go
  - internal/doh/helpers_test.go
  - internal/doh/metrics_test.go
-->

---
### Requirement: DoH derives the client source IP from the HTTP connection

The DoH server SHALL present the HTTP client's source IP address to the authoritative query path so that view selection and DNS cookies behave as they do for TCP queries from the same source. The source IP SHALL be the remote address of the HTTP (TCP) connection. The DoH server SHALL NOT derive the source IP from the `X-Forwarded-For` or `Forwarded` request headers; such headers SHALL be ignored for view selection so that a client cannot select a view by forging them.

#### Scenario: View selection uses the DoH client IP

- **WHEN** two views are configured to return different records for `www.example.com.` based on source IP, and a DoH client whose source IP matches the second view queries `www.example.com.`
- **THEN** the DoH response SHALL contain the records assigned to the second view

#### Scenario: X-Forwarded-For header does not change view selection

- **WHEN** a DoH client whose connection source IP matches the first view sends a request including `X-Forwarded-For: <an IP that would match the second view>`
- **THEN** the DoH response SHALL contain the records assigned to the first view (the header SHALL be ignored)


<!-- @trace
source: add-doh-endpoint
updated: 2026-06-27
code:
  - mkdocs.yml
  - internal/doh/acme.go
  - internal/metrics/metrics.go
  - docs/index.zh.md
  - README.md
  - docs/guides/doh.zh.md
  - internal/doh/responsewriter.go
  - go.sum
  - docs/configuration/shadowdns-yaml.md
  - internal/shadowdnscfg/config.go
  - go.mod
  - docs/index.md
  - packaging/shadowdns.yaml.example
  - docs/guides/doh.md
  - cmd/shadowdns/main.go
  - internal/server/handler.go
  - internal/doh/server.go
  - docs/configuration/shadowdns-yaml.zh.md
tests:
  - internal/doh/server_run_test.go
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/acme_integration_test.go
  - internal/metrics/metrics_doh_test.go
  - internal/shadowdnscfg/doh_test.go
  - internal/doh/server_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/doh/acme_test.go
  - internal/doh/responsewriter_test.go
  - internal/doh/helpers_test.go
  - internal/doh/metrics_test.go
-->

---
### Requirement: TLS certificate is obtained for an IP address via ACME HTTP-01

When the `doh` section is present, the DoH server SHALL obtain its TLS certificate for the `doh.acme.ip` address from the `doh.acme.directory_url` ACME directory using the HTTP-01 challenge and the certificate profile required for IP-address certificates. The ACME HTTP-01 challenge SHALL be served on a dedicated HTTP listener bound to `doh.acme.http01_listen`, which responds only to paths under `/.well-known/acme-challenge/`; all other paths on that listener SHALL return HTTP 404. Because ACME HTTP-01 validation always connects to port 80, `doh.acme.http01_listen` MUST be reachable from the public Internet as port 80. The DoH HTTPS service on the configured `doh.listen` port SHALL NOT be required to be reachable by the ACME server.

The ACME account SHALL be registered without a contact email. The `doh.acme` configuration SHALL NOT accept an email field, and configuration loading SHALL NOT require one. Consistent with the strict `KnownFields(true)` decoding, a `doh.acme.email` key present in the configuration SHALL be rejected as an unknown field.

#### Scenario: ACME challenge is served on the HTTP-01 listener

- **WHEN** the ACME server requests `http://203.0.113.10/.well-known/acme-challenge/<token>` during validation and `doh.acme.http01_listen` is bound such that the request reaches it on port 80
- **THEN** the HTTP-01 listener SHALL respond with the matching challenge key authorization

#### Scenario: Non-challenge path on the HTTP-01 listener returns 404

- **WHEN** a GET request is sent to any path outside `/.well-known/acme-challenge/` on the HTTP-01 listener (for example `http://203.0.113.10/`)
- **THEN** the HTTP-01 listener SHALL respond with HTTP 404

#### Scenario: ACME account is registered without an email

- **WHEN** ShadowDNS is started with a `doh.acme` section that contains no `email` field and obtains a certificate
- **THEN** configuration loading SHALL succeed without an email field and the ACME account SHALL be registered with no contact

#### Scenario: An email field in the configuration is rejected

- **WHEN** ShadowDNS is started with a `--config` file whose `doh.acme` section includes an `email` key
- **THEN** configuration loading SHALL fail naming the unknown `email` field, and no DoH server SHALL be started


<!-- @trace
source: add-doh-endpoint
updated: 2026-06-27
code:
  - mkdocs.yml
  - internal/doh/acme.go
  - internal/metrics/metrics.go
  - docs/index.zh.md
  - README.md
  - docs/guides/doh.zh.md
  - internal/doh/responsewriter.go
  - go.sum
  - docs/configuration/shadowdns-yaml.md
  - internal/shadowdnscfg/config.go
  - go.mod
  - docs/index.md
  - packaging/shadowdns.yaml.example
  - docs/guides/doh.md
  - cmd/shadowdns/main.go
  - internal/server/handler.go
  - internal/doh/server.go
  - docs/configuration/shadowdns-yaml.zh.md
tests:
  - internal/doh/server_run_test.go
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/acme_integration_test.go
  - internal/metrics/metrics_doh_test.go
  - internal/shadowdnscfg/doh_test.go
  - internal/doh/server_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/doh/acme_test.go
  - internal/doh/responsewriter_test.go
  - internal/doh/helpers_test.go
  - internal/doh/metrics_test.go
-->

---
### Requirement: TLS certificate is renewed and hot-swapped without restart

The DoH server SHALL select its TLS certificate per-handshake from an in-memory holder that can be updated at runtime. The ACME client SHALL renew the certificate before expiry and SHALL atomically replace the in-memory certificate on success, without restarting the HTTPS listener and without dropping existing connections. A renewal failure SHALL be recorded (log and metric) and SHALL NOT discard the still-valid current certificate.

#### Scenario: Renewed certificate is served on the next handshake

- **WHEN** the ACME client successfully renews the certificate while the DoH server is running
- **THEN** TLS handshakes established after the renewal SHALL present the renewed certificate, and the HTTPS listener SHALL NOT be restarted

#### Scenario: Renewal failure retains the current certificate

- **WHEN** a renewal attempt fails while the current certificate is still valid
- **THEN** the server SHALL continue presenting the current certificate and SHALL record the failure


<!-- @trace
source: add-doh-endpoint
updated: 2026-06-27
code:
  - mkdocs.yml
  - internal/doh/acme.go
  - internal/metrics/metrics.go
  - docs/index.zh.md
  - README.md
  - docs/guides/doh.zh.md
  - internal/doh/responsewriter.go
  - go.sum
  - docs/configuration/shadowdns-yaml.md
  - internal/shadowdnscfg/config.go
  - go.mod
  - docs/index.md
  - packaging/shadowdns.yaml.example
  - docs/guides/doh.md
  - cmd/shadowdns/main.go
  - internal/server/handler.go
  - internal/doh/server.go
  - docs/configuration/shadowdns-yaml.zh.md
tests:
  - internal/doh/server_run_test.go
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/acme_integration_test.go
  - internal/metrics/metrics_doh_test.go
  - internal/shadowdnscfg/doh_test.go
  - internal/doh/server_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/doh/acme_test.go
  - internal/doh/responsewriter_test.go
  - internal/doh/helpers_test.go
  - internal/doh/metrics_test.go
-->

---
### Requirement: DoH configuration is re-validated on SIGHUP; listener changes require a restart

DoH configuration loaded from the `doh` section SHALL be re-parsed and validated on SIGHUP as part of the existing reload flow, and configuration errors in the `doh` section SHALL be surfaced the same way other sections' errors are. Consistent with the existing DNS-listener drift behavior (the reload path deliberately does not rebind listeners), a change to `doh.listen` or the `doh.acme.*` parameters SHALL NOT be applied live: the reload SHALL log that the changed DoH listener/ACME settings require a process restart to take effect, and SHALL keep the currently bound DoH and HTTP-01 listeners running with their startup configuration. Certificate rotation is independent of SIGHUP and is governed by the "TLS certificate is renewed and hot-swapped without restart" requirement.

#### Scenario: Invalid doh configuration is rejected on SIGHUP

- **WHEN** the `doh` section is edited to an invalid configuration (for example `acme.ip` removed) and SIGHUP is delivered
- **THEN** the reload SHALL surface the `doh` configuration error and SHALL keep the currently running DoH server unchanged

#### Scenario: Changed doh listen address requires a restart

- **WHEN** `doh.listen` is edited to a different address and SIGHUP is delivered
- **THEN** the reload SHALL log that the changed DoH listener requires a process restart, and the DoH server SHALL keep listening on its startup address until the process is restarted


<!-- @trace
source: add-doh-endpoint
updated: 2026-06-27
code:
  - mkdocs.yml
  - internal/doh/acme.go
  - internal/metrics/metrics.go
  - docs/index.zh.md
  - README.md
  - docs/guides/doh.zh.md
  - internal/doh/responsewriter.go
  - go.sum
  - docs/configuration/shadowdns-yaml.md
  - internal/shadowdnscfg/config.go
  - go.mod
  - docs/index.md
  - packaging/shadowdns.yaml.example
  - docs/guides/doh.md
  - cmd/shadowdns/main.go
  - internal/server/handler.go
  - internal/doh/server.go
  - docs/configuration/shadowdns-yaml.zh.md
tests:
  - internal/doh/server_run_test.go
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/acme_integration_test.go
  - internal/metrics/metrics_doh_test.go
  - internal/shadowdnscfg/doh_test.go
  - internal/doh/server_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/doh/acme_test.go
  - internal/doh/responsewriter_test.go
  - internal/doh/helpers_test.go
  - internal/doh/metrics_test.go
-->

---
### Requirement: DoH server shuts down gracefully

The DoH HTTPS server and the ACME HTTP-01 listener SHALL shut down gracefully when the main context is cancelled (SIGINT/SIGTERM), giving in-flight requests up to 5 seconds to complete before forcefully closing.

#### Scenario: Graceful shutdown on SIGTERM

- **WHEN** SIGTERM is sent to the ShadowDNS process while the DoH server is running
- **THEN** the DoH server and the port-80 listener SHALL stop accepting new connections and wait up to 5 seconds for in-flight requests to finish


<!-- @trace
source: add-doh-endpoint
updated: 2026-06-27
code:
  - mkdocs.yml
  - internal/doh/acme.go
  - internal/metrics/metrics.go
  - docs/index.zh.md
  - README.md
  - docs/guides/doh.zh.md
  - internal/doh/responsewriter.go
  - go.sum
  - docs/configuration/shadowdns-yaml.md
  - internal/shadowdnscfg/config.go
  - go.mod
  - docs/index.md
  - packaging/shadowdns.yaml.example
  - docs/guides/doh.md
  - cmd/shadowdns/main.go
  - internal/server/handler.go
  - internal/doh/server.go
  - docs/configuration/shadowdns-yaml.zh.md
tests:
  - internal/doh/server_run_test.go
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/acme_integration_test.go
  - internal/metrics/metrics_doh_test.go
  - internal/shadowdnscfg/doh_test.go
  - internal/doh/server_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/doh/acme_test.go
  - internal/doh/responsewriter_test.go
  - internal/doh/helpers_test.go
  - internal/doh/metrics_test.go
-->

---
### Requirement: DoH queries are labeled distinctly in metrics

Per-query Prometheus metrics SHALL label DoH-transported queries with a `proto` label value distinct from `udp` and `tcp` (for example `doh`), so DoH usage is independently observable. Because the synthetic DoH writer is not a UDP writer, the existing UDP-versus-TCP detection alone cannot distinguish DoH from TCP; the synthetic writer SHALL therefore expose its transport so the query path can assign the distinct `proto` value.

#### Scenario: DoH query increments the DoH protocol label

- **WHEN** a query is answered over DoH
- **THEN** the per-query metric SHALL be incremented under the `proto` label with a value distinct from `udp` and `tcp`

<!-- @trace
source: add-doh-endpoint
updated: 2026-06-27
code:
  - mkdocs.yml
  - internal/doh/acme.go
  - internal/metrics/metrics.go
  - docs/index.zh.md
  - README.md
  - docs/guides/doh.zh.md
  - internal/doh/responsewriter.go
  - go.sum
  - docs/configuration/shadowdns-yaml.md
  - internal/shadowdnscfg/config.go
  - go.mod
  - docs/index.md
  - packaging/shadowdns.yaml.example
  - docs/guides/doh.md
  - cmd/shadowdns/main.go
  - internal/server/handler.go
  - internal/doh/server.go
  - docs/configuration/shadowdns-yaml.zh.md
tests:
  - internal/doh/server_run_test.go
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/acme_integration_test.go
  - internal/metrics/metrics_doh_test.go
  - internal/shadowdnscfg/doh_test.go
  - internal/doh/server_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/doh/acme_test.go
  - internal/doh/responsewriter_test.go
  - internal/doh/helpers_test.go
  - internal/doh/metrics_test.go
-->

---
### Requirement: ACME account key is persisted and reused across restarts

When the `doh` section is present, the DoH server SHALL load its ACME account private key from the file path configured in `doh.acme.account_key_file` and SHALL reuse the same key across process restarts and registration retries, so that account registration is idempotent and does not mint a new ACME account each time. Because the ACME directory returns the existing account for a previously registered key (RFC 8555 §7.3), reusing a persisted key SHALL NOT consume the per-source-IP new-account rate limit on restart or retry.

The `doh.acme.account_key_file` configuration field SHALL be required whenever the `doh` section is present and SHALL be an absolute path. Configuration loading SHALL fail, naming the `account_key_file` field, when it is missing or not an absolute path, and no DoH server SHALL be started in that case.

When the configured key file does not exist, the server SHALL generate a new account key, create any missing parent directory, and persist the key to the configured path with file permissions `0600` before use. When the configured key file exists but cannot be parsed as a valid account key, the server SHALL fail loudly with an error and SHALL NOT silently generate a replacement key or register a new account.

#### Scenario: First run generates and persists the account key

- **WHEN** ShadowDNS starts with a `doh.acme.account_key_file` path that does not yet exist
- **THEN** the server SHALL generate a new ACME account key, persist it to that path with `0600` permissions, and use it to register the ACME account

#### Scenario: Restart reuses the persisted account key

- **WHEN** ShadowDNS restarts with a `doh.acme.account_key_file` path that holds a previously persisted account key
- **THEN** the server SHALL load the same key and the ACME directory SHALL return the existing account rather than registering a new one

#### Scenario: Persistent registration failure does not mint new accounts

- **WHEN** ACME account registration fails repeatedly and the renewal loop retries
- **THEN** each retry SHALL reuse the same persisted account key and SHALL NOT create a new ACME account

#### Scenario: Corrupt key file fails loudly

- **WHEN** ShadowDNS starts with a `doh.acme.account_key_file` path that exists but does not contain a parseable account key
- **THEN** the server SHALL return an error identifying the key file and SHALL NOT overwrite the file, generate a new key, or register a new account

#### Scenario: Missing or relative account_key_file is rejected at load

- **WHEN** ShadowDNS is started with a `doh.acme` section whose `account_key_file` is absent or is a relative path
- **THEN** configuration loading SHALL fail naming the `account_key_file` field, and no DoH server SHALL be started

<!-- @trace
source: persist-acme-account-key
updated: 2026-06-27
code:
  - docs/configuration/shadowdns-yaml.md
  - internal/shadowdnscfg/config.go
  - internal/doh/acme_key.go
  - docs/configuration/shadowdns-yaml.zh.md
  - docs/guides/doh.zh.md
  - docs/guides/doh.md
  - packaging/shadowdns.yaml.example
  - packaging/shadowdns.service
  - internal/doh/acme.go
tests:
  - cmd/shadowdns/doh_startup_test.go
  - internal/doh/acme_integration_test.go
  - internal/doh/acme_key_test.go
  - internal/doh/helpers_test.go
  - cmd/shadowdns/doh_reload_test.go
  - internal/shadowdnscfg/doh_test.go
-->