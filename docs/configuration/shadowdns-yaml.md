# shadowdns.yaml

`shadowdns.yaml` is ShadowDNS's own unified configuration file (specified with `--config`): a single YAML document containing three optional top-level sections, `aliases` (backup domain → root mapping), `ephemeral_api` (an HTTP API for short-lived TXT records), and `doh` (DNS-over-HTTPS, RFC 8484). Any other top-level key is rejected at startup (strict decoding).

```yaml
# shadowdns.yaml

aliases:
  example.com:
    members:
      - backup.example.com
      - mirror.example.com
  example.org:
    members:
      - backup.example.org
    rewrite_rdata_labels: true
    collapse_cname_chain: true

ephemeral_api:
  listen: "127.0.0.1:8053"
  allow:
    - "127.0.0.1"
    - "10.0.0.0/8"
  # token: "optional-bearer-token"

doh:
  listen: "203.0.113.10:443"
  acme:
    directory_url: "https://acme-v02.api.letsencrypt.org/directory"
    ip: "203.0.113.10"
    http01_listen: "203.0.113.10:80"
```

## aliases fields

Each key under `aliases` is a root domain; the value is an object:

| Field | Required | Description |
|------|------|------|
| `members` | Yes (must be non-empty) | List of backup domains served by rewriting queries to this root |
| `rewrite_rdata_labels` | No (default `false`) | When `true`, RDATA name fields (CNAME/SRV targets, NS, MX, PTR, SOA names) get a label-anywhere rewrite — every root-label sequence inside the value is replaced with the backup origin, not just the in-bailiwick suffix. For zones using templated CDN-style targets that embed the root origin as a middle label |
| `collapse_cname_chain` | No (default `false`) | When `true`, in-zone CNAME chains are collapsed in responses for this root and all of its members — see [CNAME Chain Collapsing](../guides/cname-chain-collapsing.md) |

## aliases rules

- A given backup domain may appear at most once across all roots (after normalization).
- A backup domain must not equal its root (self-aliases are rejected).
- Domains not listed here are treated as independent root zones, fully loaded into memory.
- A backup zone may optionally provide its own zone file containing TXT, MX, and SRV override records. A, AAAA, CNAME, NS, and SOA records in a backup zone file are discarded with a WARN — these types are always inherited from the root.

For the query-handling details of zone aliasing, see [Zone Aliasing Internals](../guides/zone-aliasing.md).

## ephemeral_api fields

| Field | Required | Description |
|------|------|------|
| `listen` | Yes | The `host:port` the API server binds to |
| `allow` | Yes (must be non-empty) | List of source IPs or CIDRs allowed to access the API; an empty list is rejected |
| `token` | No | Pre-shared bearer token. When set, every request must carry `Authorization: Bearer <token>`; when omitted, token verification is skipped (the IP ACL still applies) |

When the `ephemeral_api` section is absent, no HTTP API server is started. For endpoint details, request/response schemas, and `curl` examples, see [Ephemeral TXT API](../ephemeral-api.md).

## doh fields

All fields are required; loading fails naming the first missing field.

| Field | Required | Description |
|------|------|------|
| `listen` | Yes | The `host:port` the DoH HTTPS service binds to, e.g. `203.0.113.10:443` |
| `acme.directory_url` | Yes | ACME directory URL of the issuing CA (must be an absolute `https://` URL) |
| `acme.ip` | Yes | The IP address the certificate is issued for (RFC 8738 IP-identifier certificate) |
| `acme.http01_listen` | Yes | The `host:port` the ACME HTTP-01 challenge responder binds to; MUST be reachable from the public Internet as port 80 |

The ACME account is registered without a contact email, so `doh.acme` accepts no `email` field; including one fails the load as an unknown field. (Contact email is optional under RFC 8555, and the short-lived auto-renewed certificate makes expiry notifications moot.)

When the `doh` section is absent, no DoH server, ACME client, or HTTP-01 listener is started.

DoH reuses the authoritative query path and is **non-recursive**: it only answers zones ShadowDNS hosts, and out-of-zone queries get `REFUSED`. It is **not** a general recursive DoH resolver.

The TLS certificate is obtained for the IP via ACME HTTP-01 using the Let's Encrypt short-lived profile (~6 days) and is auto-renewed and hot-swapped without a restart.

For deployment walkthrough and operational details, see [DNS-over-HTTPS](../guides/doh.md).

## SIGHUP hot reload

SIGHUP re-reads `shadowdns.yaml` and **atomically** replaces the in-memory alias map:

- If validation of either section fails, the running server keeps its previous state and ephemeral records are unaffected.
- On a successful reload, the ephemeral record store is cleared.
- The `doh` section is re-validated on reload (validation errors keep the running server). However, changes to `doh.listen` or any `doh.acme.*` field are **not** applied live — they require a process restart and are logged as an advisory. Certificate rotation is independent and automatic.
- Every reload attempt is observable via Prometheus:
    - `shadowdns_reload_total{result="success"|"failure"}` counts reload outcomes
    - `shadowdns_config_last_reload_success_timestamp_seconds` records the Unix time of the last successful configuration load (initialized at startup); use `time() - <gauge>` for configuration-staleness alerting

!!! warning "Breaking change as of v0.x"
    The legacy `--aliases` CLI flag and the `aliases.yaml` file have been removed. Migration is mechanical: move the entries from the old `aliases.yaml` (root → [backups] format) under the `aliases:` section of the new `shadowdns.yaml`.
