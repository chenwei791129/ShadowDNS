# Ephemeral TXT API

ShadowDNS ships with a lightweight built-in HTTP API that lets ACME clients (certbot, acme.sh, lego, etc.) dynamically add or remove short-lived TXT records for DNS-01 challenge validation. All records live in memory only — they are never written to the zone file or to disk; they are cleared on TTL expiry, service restart, or SIGHUP reload.

---

## Enabling and configuration

The API is controlled by the `ephemeral_api` section of the unified config (the `shadowdns.yaml` pointed to by `--config`). When the section is absent, the API server does not start.

```yaml
# /etc/shadowdns/shadowdns.yaml
ephemeral_api:
  listen: "127.0.0.1:8053"
  allow:
    - "127.0.0.1"
    - "10.0.0.0/8"
  # token: "optional-bearer-token"
```

Field rules:

| Field | Type | Required | Description |
|------|------|------|------|
| `listen` | string | Required | `host:port`, the bind address of the API server |
| `allow` | list | Required, non-empty | Source IPs or CIDRs allowed to connect (IPv4/IPv6 both supported) |
| `token` | string | Optional | Pre-shared bearer token; omit to disable authentication |

The IP ACL is checked before token validation: a source IP not on the allow list gets a `403` immediately, even if the token is correct.

---

## Endpoints

| Method | Path | Purpose |
|--------|------|------|
| `PUT` | `/v1/txt/{fqdn}` | Add or update an ephemeral TXT record |
| `DELETE` | `/v1/txt/{fqdn}` | Delete an ephemeral TXT record (idempotent) |

`{fqdn}` is normalized to lowercase + trailing dot; letter case and the presence of a trailing dot do not affect the result.

---

## PUT — add or refresh a TXT value

Multiple values can coexist under the same FQDN. PUT has "add-or-refresh" semantics:

- The given `value` does not yet exist under that FQDN → a new entry is appended
- The given `value` already exists → the entry's TTL is refreshed in place, with **no** duplicate created

Two consecutive calls with the same body are therefore idempotent — the final state is identical to calling once. This maps to the ACME DNS-01 scenario of validating apex + wildcard concurrently: two clients can each PUT their own token without overwriting each other.

### Request body

| Field | Type | Required | Description |
|------|------|------|------|
| `value` | string | Required | The TXT record value (e.g. an ACME challenge token); UTF-8 bytes ≤ 255 (the RFC 1035 TXT character-string limit), `400` if exceeded |
| `ttl` | integer | Optional (default 0) | Seconds; clamped to `[1, 3600]` (`0` → `1`, `7200` → `3600`) |

### Example (without token)

```bash
curl -X PUT http://127.0.0.1:8053/v1/txt/_acme-challenge.example.com \
  -H 'Content-Type: application/json' \
  -d '{"value":"challenge-token-from-acme-client","ttl":120}'
```

### Example (with token)

```bash
curl -X PUT http://127.0.0.1:8053/v1/txt/_acme-challenge.example.com \
  -H 'Authorization: Bearer secret123' \
  -H 'Content-Type: application/json' \
  -d '{"value":"challenge-token","ttl":120}'
```

### Success response (200)

```json
{
  "status": "ok",
  "fqdn": "_acme-challenge.example.com.",
  "ttl": 120,
  "count": 1
}
```

- `fqdn`: canonical form (lowercase + trailing dot)
- `ttl`: the value actually applied (may have been clamped)
- `count`: the total number of ephemeral entries currently held for that FQDN (including the entry from this PUT). For example, if another ACME client has already placed a value under the same name, `count` will be `2` after your PUT.

### Multi-value example

```bash
# First PUT (apex validation)
curl -X PUT http://127.0.0.1:8053/v1/txt/_acme-challenge.example.com \
  -H 'Content-Type: application/json' \
  -d '{"value":"token-apex","ttl":120}'
# → {"status":"ok","fqdn":"...","ttl":120,"count":1}

# Second PUT (wildcard validation, different value)
curl -X PUT http://127.0.0.1:8053/v1/txt/_acme-challenge.example.com \
  -H 'Content-Type: application/json' \
  -d '{"value":"token-wildcard","ttl":120}'
# → {"status":"ok","fqdn":"...","ttl":120,"count":2}

# A DNS query returns two independent TXT RRs
dig @127.0.0.1 _acme-challenge.example.com TXT +short
# "token-apex"
# "token-wildcard"
```

---

## DELETE — clear ephemeral records

`DELETE` supports two modes:

- **Without `?value=` (wipe-all)**: removes **all** ephemeral entries under that FQDN, regardless of how many exist or what their values are.
- **With `?value=<value>` (per-value delete)**: removes only the entry under that FQDN whose value exactly matches the query string; other values are unaffected. This is the safe way to finish a single challenge under ACME DNS-01 parallel validation (apex + wildcard sharing the same name with different tokens) — a wipe-all would also wipe the other token that is still mid-validation.

**DELETE only affects the ephemeral store; same-name records in the zone file are completely untouched**, so deleting authoritative data through the API can never happen.

### Wipe-all

```bash
curl -X DELETE http://127.0.0.1:8053/v1/txt/_acme-challenge.example.com \
  -H 'Authorization: Bearer secret123'
```

### Per-value delete

```bash
# URL-encode any non-URL-safe characters in the value (tokens are usually base64url and need no encoding)
curl -X DELETE "http://127.0.0.1:8053/v1/txt/_acme-challenge.example.com?value=token-apex" \
  -H 'Authorization: Bearer secret123'
```

Matching rules:

- Byte-exact, case-sensitive, with no normalization whatsoever (consistent with the PUT matching logic).
- The `?value=` value must be ≤ 255 UTF-8 bytes (the RFC 1035 TXT character-string limit), `400` if exceeded.
- `?value=` (empty string) returns `400`, to avoid confusion with wipe-all (no query key at all).
- `?value=xxx` with no matching entry in the store returns `200` (idempotent).

### Success response (200)

```json
{
  "status": "ok",
  "fqdn": "_acme-challenge.example.com."
}
```

DELETE is idempotent — a nonexistent FQDN, or a `?value=` with no match, both return `200`. Deleting the same FQDN multiple times is also safe.

---

## Querying TXT records

Ephemeral TXT records are retrieved directly through standard DNS queries — no separate API is needed. When the same FQDN holds multiple ephemeral values, the DNS response synthesizes each one as an **independent TXT RR** (rather than packing multiple strings into a single RR), and each RR carries its own dynamically computed remaining TTL (floor `1`).

```bash
dig @127.0.0.1 _acme-challenge.example.com TXT +short
# "token-apex"
# "token-wildcard"
```

### Precedence relative to the zone file

If the zone file already has a TXT record under the same name, **the zone file wins** — the ephemeral store is never consulted, preventing accidental shadowing of authoritative data through the API.

DNS query dispatch order (TXT qtype):

1. Zone exact `(qname, TXT)` match → on hit, return the zone TXT
2. **Ephemeral store overlay** → on hit, return the ephemeral TXT (see the next section)
3. RFC 1034 §3.6.2 CNAME fallback
4. RFC 4592 wildcard synthesis

This order is identical for root zones and backup (alias) zones.

### Ephemeral TXT overrides an exact CNAME (TXT qtype only)

A typical ACME DNS-01 delegation CNAMEs `_acme-challenge.<domain>` to an external acme-dns provider:

```
_acme-challenge.foo.example.com. IN CNAME acme-dns.external.net.
```

In this situation, PUTting a TXT for `_acme-challenge.foo.example.com.` through the API means **a DNS TXT query returns the ephemeral TXT value, not the CNAME in the zone**. This is a deliberate deviation from RFC 1034 §3.6.2 ("CNAME exclusively owns the owner name"), narrowly scoped to:

- **`TXT` qtype only**: `dig CNAME`, `dig A`, `dig AAAA` and all other qtypes still follow standard CNAME fallback behavior, unaffected by the ephemeral store
- **Only while the ephemeral store holds an unexpired entry**: once the entry expires or was never written, behavior automatically falls back to the standard CNAME chain (RFC 1034 §3.6.2)
- **Zero state-transition cost**: once all ephemeral entries are gone, `dig TXT` immediately reverts to the CNAME-following result, with no manual intervention required

```bash
# Write an ephemeral TXT
curl -X PUT http://127.0.0.1:8053/v1/txt/_acme-challenge.foo.example.com \
  -d '{"value":"token-xyz","ttl":120}'

# A TXT query returns the ephemeral value (not the CNAME)
dig @127.0.0.1 _acme-challenge.foo.example.com TXT +short
# "token-xyz"

# A CNAME query still returns the CNAME configured in the zone
dig @127.0.0.1 _acme-challenge.foo.example.com CNAME +short
# acme-dns.external.net.
```

This override behavior applies equally to backup (alias) zones: if you PUT `_acme-challenge.foo.backup.com.` through the API, a TXT query for the backup name returns the ephemeral TXT even when the root zone has a CNAME at `_acme-challenge.foo.example.com.`.

---

## Error reference

| Scenario | HTTP code | Response body |
|------|-----------|-----------|
| Source IP not on the `allow` list | `403` | `{"status":"error","error":"source IP not in allow list"}` |
| Token configured but header missing / malformed | `401` | `{"status":"error","error":"missing or malformed Authorization header"}` |
| Token configured but value mismatched | `401` | `{"status":"error","error":"invalid token"}` |
| Empty body, non-JSON, or unknown fields | `400` | `{"status":"error","error":"invalid JSON body: ..."}` |
| Body missing the `value` field | `400` | `{"status":"error","error":"missing required field: value"}` |
| PUT body `value` longer than 255 bytes | `400` | `{"status":"error","error":"value exceeds 255-byte limit (got N)"}` |
| DELETE `?value=` (empty string) | `400` | `{"status":"error","error":"empty value query parameter"}` |
| DELETE `?value=` longer than 255 bytes | `400` | `{"status":"error","error":"value exceeds 255-byte limit (got N)"}` |
| PUT FQDN does not fall under any loaded zone | `422` | `{"status":"error","error":"FQDN \"...\" does not belong to any zone served by this server"}` |

### Zone membership check (PUT only)

Before writing to the ephemeral store, PUT checks whether the canonical FQDN falls under any loaded zone origin (across all views, matching both root and backup roles). On no match, it returns `422 Unprocessable Entity` and the store is not modified.

This design catches silent failures caused by caller-side typos (e.g. mistyping `_acme-challenge.exmaple.com`): older versions returned `200` while subsequent DNS queries came back empty; the new version reports an explicit error.

The check runs after the IP ACL, token, FQDN canonicalization, JSON parsing, value-length check, and TTL clamp; format errors and authentication failures therefore still return their respective `400` / `401` / `403`, rather than being shadowed by `422`.

`DELETE` is exempt from this check, preserving its original idempotent semantics — a DELETE for an FQDN outside any zone still returns `200`.

After a SIGHUP reload, added or removed zone origins take effect immediately on the next PUT — no API server restart required.

Token comparison uses `crypto/subtle.ConstantTimeCompare`, which is resistant to timing attacks.

---

## TTL behavior and cleanup

| Trigger | Effect |
|------|------|
| TTL expiry | Lazy eviction (checked at query time) + a periodic GC every 30 seconds that actively sweeps |
| SIGHUP reload of the unified config | On successful reload, `Store.Clear()` is called to clear all ephemeral records; on failed reload, they are kept |
| Process restart | All records vanish (in-memory, not persisted) |

The `3600`-second cap on `ttl` is a deliberate safeguard preventing forgotten records from occupying memory long-term.

---

## ACME client integration tips

Most ACME clients can push challenges through a custom hook:

```bash
# certbot --manual --preferred-challenges dns --manual-auth-hook ./put-txt.sh
# put-txt.sh:
curl -X PUT "http://127.0.0.1:8053/v1/txt/_acme-challenge.${CERTBOT_DOMAIN}" \
  -H "Authorization: Bearer ${SHADOWDNS_TOKEN}" \
  -H 'Content-Type: application/json' \
  -d "{\"value\":\"${CERTBOT_VALIDATION}\",\"ttl\":120}"
```

The corresponding cleanup hook — single-client scenario (`certbot` validating only one of apex or wildcard):

```bash
curl -X DELETE "http://127.0.0.1:8053/v1/txt/_acme-challenge.${CERTBOT_DOMAIN}" \
  -H "Authorization: Bearer ${SHADOWDNS_TOKEN}"
```

**Parallel-validation scenarios** (running apex + wildcard simultaneously, or two clients sharing `_acme-challenge.<domain>`) should clean up with `?value=` instead, collecting only their own token — a wipe-all would accidentally delete the other client's challenge that is still mid-validation:

```bash
curl -X DELETE "http://127.0.0.1:8053/v1/txt/_acme-challenge.${CERTBOT_DOMAIN}?value=${CERTBOT_VALIDATION}" \
  -H "Authorization: Bearer ${SHADOWDNS_TOKEN}"
```

`lego` can wrap these two calls by implementing the `Provider` interface; `acme.sh` can do so via a custom `dns_shadowdns.sh` plugin.
