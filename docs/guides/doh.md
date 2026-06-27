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

## TLS and certificates

The DoH listener serves TLS with a certificate issued **for the IP address** (`acme.ip`), obtained automatically via ACME HTTP-01 validation using the Let's Encrypt short-lived certificate profile (~6-day validity). ShadowDNS auto-renews the certificate well before expiry and hot-swaps it into the running listener **without restarting** — in-flight and subsequent connections pick up the new certificate transparently.

Because the certificate is bound to the IP rather than a hostname, clients connect to the IP directly (as in the curl examples above).

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

- **Port 80** (`acme.http01_listen`) **must be reachable from the public Internet** so the ACME server can complete HTTP-01 validation. This responder serves **only** `/.well-known/acme-challenge/` — every other path returns `404`. It carries no DNS data.
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

See [Monitoring](../operations/monitoring.md) for how these are scraped and dashboarded.

---

## Reload behavior (SIGHUP)

The `doh:` section is re-validated on SIGHUP, but changes to `doh.listen` or any `doh.acme.*` field **require a process restart** to take effect — the listener and ACME account are established at startup. When such a change is detected on reload, ShadowDNS logs an advisory entry noting that a restart is required; the running listener continues with its previous settings until then.

---

## See also

- [`shadowdns.yaml`](../configuration/shadowdns-yaml.md) — the `doh:` section field reference and example.
- [CLI Reference](../reference/cli.md) for related flags.
- [Monitoring](../operations/monitoring.md) for the DoH metrics above.
