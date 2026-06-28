# DNS-over-HTTPS (DoH)

ShadowDNS exposes an RFC 8484 DNS-over-HTTPS endpoint at `/dns-query` that reuses the same authoritative query path as the UDP/TCP listeners. The intent is operational: it lets operators verify zone records over standard HTTPS (TCP/443) — for example through a firewall or middlebox that only permits TCP/443 — without opening port 53. A DoH query is decoded, handed to the identical handler the UDP/TCP path uses, and the wire-format answer is returned over HTTPS.

!!! warning
    **ShadowDNS DoH is AUTHORITATIVE and NON-RECURSIVE.** It answers only the zones ShadowDNS hosts; any out-of-zone query returns REFUSED. It is **not** a general-purpose recursive DoH resolver — do **not** point browsers or client devices at it expecting public name resolution. It exists to verify ShadowDNS's own authoritative records over HTTPS, nothing more.

---

## Enabling

DoH is configured entirely through the `doh:` section in [`shadowdns.yaml`](../configuration/shadowdns-yaml.md). When the section is absent, **no DoH server starts** and the binary behaves exactly as a build without the feature.

The required fields are:

| Field | Purpose |
|-------|---------|
| `listen` | Address the DoH HTTPS service binds (TCP/443) |
| `acme.directory_url` | ACME directory endpoint (e.g. `https://acme-v02.api.letsencrypt.org/directory`) |
| `acme.ip` | The public IP the certificate is issued for |
| `acme.http01_listen` | Address the ACME HTTP-01 challenge responder binds (TCP/80) |
| `acme.account_key_file` | Absolute path to the persisted ACME account private key (see [ACME account key persistence](#acme-account-key-persistence)) |

See [`shadowdns.yaml`](../configuration/shadowdns-yaml.md) for the full field tables and an example block.

---

## RFC 8484 protocol

The endpoint accepts both GET and POST on the `/dns-query` path:

- **GET** `/dns-query?dns=<base64url-no-padding>` — the DNS query message is base64url-encoded (no padding) in the `dns` query parameter.
- **POST** `/dns-query` — the raw DNS query message is the request body, with `Content-Type: application/dns-message`.

Responses are always returned with `Content-Type: application/dns-message`.

Error handling:

| Condition | Status |
|-----------|--------|
| Path other than `/dns-query` | `404 Not Found` |
| Method other than GET or POST | `405 Method Not Allowed` |
| Request that cannot be decoded into a DNS message | `400 Bad Request` |
| POST body larger than 65535 bytes | `413 Payload Too Large` |

---

## curl examples

```bash
# GET: base64url-encoded (no padding) DNS query in the `dns` parameter
curl -sS 'https://203.0.113.10/dns-query?dns=AAABAAABAAAAAAAAA3d3dwdleGFtcGxlA2NvbQAAAQAB' \
  | xxd

# POST: raw DNS message as the request body
curl -sS -H 'content-type: application/dns-message' \
  --data-binary @query.bin \
  https://203.0.113.10/dns-query | xxd
```

To build `query.bin`, capture a wire-format query — for example with `dig +noedns +qr www.example.com A` and extracting the request bytes, or any tool that emits a raw DNS message.

---

## application/dns-json format

Alongside the RFC 8484 wire format, the `/dns-query` endpoint serves the Google Public DNS / CloudFlare de-facto `application/dns-json` format on **GET** requests. This lets you verify records with `curl` + `jq` and zero client-side encoding — no need to hand-assemble a base64url wire query.

!!! note
    `application/dns-json` is **not** an RFC; it follows the Google Public DNS response schema. Field ordering and whitespace are not significant — only field names, types, and values are.

### Format negotiation

The format is chosen on the GET path as follows:

- A request that carries a `?dns=` parameter is **always** handled as RFC 8484 wire-format, regardless of its `Accept` header. The `?dns=` parameter takes precedence, so a wire query is never misrouted to the JSON parser.
- A request with **no** `?dns=` parameter and an `Accept` header that lists `application/dns-json` is served as JSON, with `Content-Type: application/dns-json`.
- **POST** is always wire-format; the JSON format is GET-only (matching Google / CloudFlare).

### Query parameters

| Parameter | Required | Description |
|-----------|----------|-------------|
| `name` | yes | The query name. Must be non-empty. Normalized to a trailing-dot FQDN; on-wire letter case is preserved (so `ExAmple.COM` is echoed verbatim). |
| `type` | no (default `A`) | DNS record type. Accepts a mnemonic **case-insensitively** (`TXT`, `txt`, `Txt`) or a numeric code in the range 0–65535. |
| `edns_client_subnet` | no | A client subnet as `<ip>[/<prefix>]` injected as an EDNS Client Subnet option (see below). When the prefix is omitted it defaults to `/24` for IPv4 and `/56` for IPv6. |
| `cd` | no | Accepted but **ignored** — ShadowDNS is non-recursive and does no DNSSEC validation. It never sets the response `CD` bit. |

The `do` and `ct` parameters are not honored; their presence is ignored and does not cause an error.

### Response schema

A successful response is a JSON object following the Google Public DNS schema:

```json
{
  "Status": 0,
  "TC": false,
  "RD": true,
  "RA": false,
  "AD": false,
  "CD": false,
  "Question": [{ "name": "www.example.com.", "type": 1 }],
  "Answer": [{ "name": "www.example.com.", "type": 1, "TTL": 300, "data": "203.0.113.20" }]
}
```

- `Status` is the integer DNS RCODE (e.g. `0` NOERROR, `3` NXDOMAIN, `5` REFUSED).
- `RD` is always `true` (the dispatched query sets recursion-desired); `CD` is always `false`.
- `Answer[].data` is the RDATA in DNS presentation format with the record header stripped, so multi-field RDATA (SOA, MX) and quoted TXT data are preserved intact.
- The response carries the same `Cache-Control: max-age=N` header as the wire path, bounded by the smallest Answer TTL.

DNS-level outcomes are conveyed in `Status`, not via HTTP error codes:

| Condition | HTTP status |
|-----------|-------------|
| Well-formed query (any RCODE, including REFUSED / NXDOMAIN / empty answer) | `200 OK` |
| Missing, empty, or malformed `name` (a label over 63 octets or a name over 255 octets), unparseable `type`, or unparseable `edns_client_subnet` | `400 Bad Request` |
| Dispatched query produced no captured response (internal failure) | `500 Internal Server Error` |

### Zone transfers are refused

`type=AXFR` and `type=IXFR` are refused with `Status` 5 (REFUSED) and an empty `Answer`, identical to the wire path — a zone transfer is a multi-message stream with no representation in a single JSON response.

### curl + jq examples

```bash
# Look up an A record as JSON
curl -sS -H 'accept: application/dns-json' \
  'https://203.0.113.10/dns-query?name=www.example.com&type=A' | jq

# Extract just the answer data
curl -sS -H 'accept: application/dns-json' \
  'https://203.0.113.10/dns-query?name=www.example.com&type=TXT' \
  | jq -r '.Answer[].data'
```

### Simulating a client subnet (ECS)

When ECS is enabled on the server (`--ecs-enable`), the `edns_client_subnet` parameter lets a single host simulate queries from any network, so you can verify split-horizon / GeoIP view selection without sourcing traffic from that network:

```bash
curl -sS -H 'accept: application/dns-json' \
  'https://203.0.113.10/dns-query?name=www.example.com&type=A&edns_client_subnet=198.51.100.0/24' \
  | jq '{Answer, edns_client_subnet}'
```

Host bits beyond the prefix are masked automatically (e.g. `198.51.100.5/24` becomes `198.51.100.0/24`), so a sloppy value does not produce a FORMERR. When ECS is in effect, the response includes an `edns_client_subnet` field formatted as `<network>/<source-prefix>/<scope-prefix>`. ShadowDNS is authoritative and does not narrow the scope to a geo boundary, so the **scope-prefix echoes the source-prefix** — it confirms the subnet was accepted and used for view selection, nothing more.

!!! warning
    When `--ecs-enable` is **off** (the default), an injected `edns_client_subnet` is silently ignored — exactly as for a wire query carrying ECS while ECS is disabled — and the response carries no `edns_client_subnet` field.

---

## TLS and certificates

The DoH listener serves TLS with a certificate issued **for the IP address** (`acme.ip`), obtained automatically via ACME HTTP-01 validation using the Let's Encrypt short-lived certificate profile (~6-day validity). ShadowDNS auto-renews the certificate well before expiry and hot-swaps it into the running listener **without restarting** — in-flight and subsequent connections pick up the new certificate transparently.

Because the certificate is bound to the IP rather than a hostname, clients connect to the IP directly (as in the curl examples above).

---

## ACME HTTP-01 listener hardening

The HTTP-01 responder on port 80 (`acme.http01_listen`) is, by design, the **only fully public HTTP surface ShadowDNS exposes** — it must accept connections from the entire Internet so the ACME server can reach it. To keep that attack surface and fingerprint as small as possible, the listener answers exactly **one** kind of request and drops everything else.

A request is served (`200 OK` with the key authorization body) only when **all** of the following hold:

- the method is **GET**, and
- the path is **under** `/.well-known/acme-challenge/` (the trailing slash matters), and
- the token names a challenge that is **currently being presented** for an in-flight authorization.

Every other request — an unknown path, an unknown or empty token, the bare `/.well-known/acme-challenge` with no trailing slash, or any non-GET method — is **aborted at the connection level**. ShadowDNS sends **no HTTP response whatsoever**: no status line, no headers, no body. The client sees a connection reset / EOF, and the server logs no stack trace. This is the same posture as nginx's `return 444`. In particular there is **no** `404` for unknown paths and **no** `301` redirect for the slash-less subtree path — both of those would otherwise leak that a server is listening and what it is.

This hardening has **no effect on legitimate certificate issuance or renewal**: the ACME validator only ever fetches the exact token ShadowDNS just began presenting, which is the one request shape that is served `200`. Certificates continue to be issued and renewed normally.

---

## ACME account key persistence

ShadowDNS persists its ACME **account** private key to the absolute path set in `acme.account_key_file` and reuses it across restarts and registration retries. The recommended location is under the systemd state directory:

```yaml
acme:
  account_key_file: "/var/lib/shadowdns/acme/account.key"
```

The packaged systemd unit declares `StateDirectory=shadowdns`, so `/var/lib/shadowdns` is created on every start owned by the service user with mode `0700`.

Behavior:

- **First start** — when the file does not exist, ShadowDNS generates a new P256 account key and writes it to the path as PKCS#8 PEM with permissions `0600`, then registers the ACME account.
- **Restart / retry** — the same key is loaded, so the ACME directory returns the *existing* account (RFC 8555 §7.3) instead of registering a new one. This is what keeps re-registration idempotent and avoids exhausting the per-source-IP **new-account** rate limit during crash loops or repeated registration failures.
- **Corrupt or unreadable key file** — ShadowDNS **fails loudly**: it logs an error naming the file and does **not** silently mint a replacement key or register a new account (a silent rebuild is exactly what would trip the rate limit). Because the obtainer is not cached on failure, the error recurs on every renewal retry until you repair or remove the file; DoH serves no certificate until then.

Operational notes:

- The account key is a **secret**. Keep it `0600` and owned by the service user; do not commit it or copy it into shared locations.
- Persistence relies on a **static** service user (`User=shadowdns`). Do not switch the unit to `DynamicUser=yes` — a per-boot UID would change `StateDirectory` ownership and make the persisted key unreadable, silently reintroducing new-account churn.
- Changing `account_key_file` **requires a process restart** to take effect. On SIGHUP reload it is detected as DoH config drift and logged with a "restart to apply" advisory, like the other `doh.acme.*` fields.

---

## Firewall and port deployment

DoH uses two TCP ports with very different exposure requirements:

- **Port 80** (`acme.http01_listen`) **must be reachable from the public Internet** so the ACME server can complete HTTP-01 validation. This responder is ShadowDNS's only fully public HTTP surface, so it is hardened to answer exactly one kind of request: a GET for a live challenge token returns `200` with the key authorization; **every other request is dropped at the connection level** — no HTTP response is sent at all (no `404`, no `301` redirect), the client just sees a reset/EOF. See [ACME HTTP-01 listener hardening](#acme-http-01-listener-hardening). It carries no DNS data.
- **Port 443** (`listen`, the DoH service) **should be restricted by firewall to trusted source IPs**. It does **not** need to be reachable by the ACME server, only by the operators who use it to verify records.

A typical deployment opens port 80 to the world (challenge-only) and limits port 443 to a small allowlist of operator addresses.

---

## Source IP and views

DoH view selection uses the **TCP connection's source IP** — the address ShadowDNS observes at the transport layer. `X-Forwarded-For` and `Forwarded` HTTP headers are **ignored**. This is a deliberate security boundary: a client cannot forge a view by setting a header.

---

## Cache headers

Each DoH response carries a `Cache-Control: max-age=N` header, where `N` is bounded by the smallest Answer TTL in the response. For responses with no positive-lifetime answer (empty answer sections), `N` is `0`.

---

## Observability

DoH queries are visible in the standard metrics alongside UDP and TCP:

- `shadowdns_dns_requests_total` carries a `proto="doh"` label, distinct from `proto="udp"` and `proto="tcp"`, so DoH traffic can be counted and rate-tracked separately.
- `shadowdns_doh_cert_renewals_total{result="success"|"failure"}` counts certificate renewal attempts by outcome.
- `shadowdns_doh_cert_not_after_timestamp_seconds` records the current certificate's expiry as a Unix timestamp, for alerting on imminent expiry.
- `shadowdns_doh_acme_dropped_total{reason="unknown_path"|"unknown_token"|"bad_method"}` counts the probe connections the port 80 HTTP-01 listener aborted without responding (see [ACME HTTP-01 listener hardening](#acme-http-01-listener-hardening)). Use it to observe how much port 80 is being probed.

See [Monitoring](../operations/monitoring.md) for how these are scraped and dashboarded.

---

## Reload behavior (SIGHUP)

The `doh:` section is re-validated on SIGHUP, but changes to `doh.listen` or any `doh.acme.*` field **require a process restart** to take effect — the listener and ACME account are established at startup. When such a change is detected on reload, ShadowDNS logs an advisory entry noting that a restart is required; the running listener continues with its previous settings until then.

---

## FAQ

### Is the issued TLS certificate stored, or re-issued on every restart?

**It is re-issued on every restart.** The leaf certificate (and its private key) lives only in memory — it is never written to disk. On every process start, ShadowDNS obtains a fresh certificate from the ACME directory before the listener serves its first handshake. The only ACME material persisted to disk is the **account** key (see [ACME account key persistence](#acme-account-key-persistence)), which is a different thing: it lets restarts reuse the same ACME account instead of registering a new one.

This is a deliberate trade-off. The certificate is short-lived (~6 days) and restarts are expected to be far rarer than that, so re-issuing on start keeps the design simple and avoids ever writing the certificate's private key to disk.

Operational consequence: **each restart is a real certificate issuance.** Persisting the account key prevents the **new-account** rate limit, but not the per-IP certificate / new-order limits. Avoid putting ShadowDNS in a crash loop or a rapid restart cycle against the **production** ACME directory; use a staging directory when you need to restart repeatedly during testing.

### When the certificate auto-renews, does it run a full config reload?

**No.** Renewal is independent of SIGHUP config reload — it never re-reads configuration, re-opens zone data, or restarts the listener. A background loop obtains the renewed certificate and atomically swaps it into the holder that `tls.Config.GetCertificate` reads on every handshake, so the **next** TLS handshake picks up the new certificate while in-flight connections continue uninterrupted. Nothing rebinds the port and no other subsystem is touched.

The two paths are orthogonal: certificate rotation is automatic and listener-local, while a SIGHUP reload re-reads the rest of the configuration but does **not** touch the certificate (and `doh.*` changes still require a restart — see [Reload behavior (SIGHUP)](#reload-behavior-sighup)).

---

## See also

- [`shadowdns.yaml`](../configuration/shadowdns-yaml.md) — the `doh:` section field reference and example.
- [CLI Reference](../reference/cli.md) for related flags.
- [Monitoring](../operations/monitoring.md) for the DoH metrics above.
