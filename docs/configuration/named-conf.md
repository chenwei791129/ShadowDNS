# named.conf Compatibility

ShadowDNS reads your existing BIND `named.conf` directly — no format conversion needed. This page covers the supported directive scope, view matching semantics, RRL and query logging configuration, and the directives that are rejected.

## Supported options directives

The `options` block supports: `directory`, `geoip-directory`, `listen-on`, `listen-on-v6`, `allow-transfer`, `recursion`, `minimal-responses`, `version`, `hostname`, `transfer-format`, `notify`.

### listen-on (IPv4)

- Supports `listen-on { any; };` and explicit IPv4 address lists, binding each address individually.
- If an individual address fails to bind (e.g. a `127.0.0.x` alias occupied by `systemd-resolved`), it is logged as WARN and skipped; as long as at least one listener binds successfully, the server can start.
- The precedence rules between the `--listen` flag and `listen-on` are detailed in [Migrating from BIND](../migration.md).

### listen-on-v6 (IPv6)

- Same per-address binding model as IPv4.
- Supported tokens: `any` (enumerates local IPv6 interface addresses, excluding link-local `fe80::/10` which requires a zone index, but including loopback `::1`), `none`, and explicit IPv6 address literals (e.g. `2001:db8::1`).
- IPv6 is **opt-in**: without a `listen-on-v6` block no IPv6 listener is opened, so IPv4-only deployments are unaffected.
- Unsupported tokens (IPv4 literals, the exclusion syntax `!addr`, ACL names, `port N`) are logged as WARN and skipped, and do not cause startup failure.

## Views and match-clients

```text
view "<name>" {
    match-clients { ... };
    ...
};
```

- Uses **first-match** semantics (same as BIND): rules are evaluated left to right in declaration order, and the first matching rule determines the view.
- When no view matches, the response is **REFUSED**.
- Supported rule types:

| Rule type | Example |
|----------|------|
| GeoIP country | `geoip country TW` |
| GeoIP ASN | `geoip asnum "AS64500 Example ISP"` |
| Single IPv4 address | `192.0.2.10` |
| IPv4 CIDR | `198.51.100.0/24` |
| Any source | `any` |

!!! warning "The `any` view must be declared last"
    A view whose `match-clients` contains `any;` matches **all** clients. If it precedes more specific views (such as GeoIP views), those will never be evaluated. ShadowDNS logs a WARN at startup when `any` is used by a view that is not the last one, but does not block startup.

!!! warning "ASN description string format"
    The `geoip asnum` string must match the `"AS<number> <description>"` format (the parsing rule is `^AS(\d+)\s`); the description text is ignored. A string not starting with `AS` + digits + whitespace (e.g. `"64500"` missing the `AS` prefix) causes startup failure.

## Response Rate Limiting (RRL)

RRL is configured through the BIND-compatible `rate-limit { ... }` block, and is **only supported inside the global `options`** — placing it inside a `view` block is warned about and ignored (v1 does not support per-view rate limiting).

RRL applies only to **UDP responses**; TCP responses are never rate-limited.

Supported sub-options (defaults match BIND):

| Sub-option | Description |
|--------|------|
| `responses-per-second` | Maximum response rate per client prefix |
| `referrals-per-second` | Parsed only for BIND compatibility; never triggers (ShadowDNS is a purely authoritative server and issues no referrals) |
| `nodata-per-second` | NODATA response rate cap |
| `nxdomains-per-second` | NXDOMAIN response rate cap |
| `errors-per-second` | Error response (SERVFAIL, REFUSED, etc.) rate cap |
| `all-per-second` | Global cap across all response categories |
| `window` | Tracking window (seconds) |
| `slip` | Fraction of rate-limited responses answered with a truncated reply instead of being dropped outright |
| `ipv4-prefix-length` | IPv4 prefix length for client grouping |
| `ipv6-prefix-length` | IPv6 prefix length for client grouping |
| `exempt-clients` | Client ACL exempt from rate limiting |
| `log-only` | Log only, without actually dropping |
| `max-table-size` | Upper bound on the number of tracked client prefixes |
| `min-table-size` | Minimum allocated table size |

`qps-scale` is **not supported**; it is warned about and ignored.

## Query logging (BIND format)

ShadowDNS parses the standard `logging{}` block (a `channel`'s `file`/`severity`/`print-*` plus `category queries`) and, for every query that completes view matching, writes one line in the **exact same format** as BIND's queries category — existing downstream log parsers need no changes at all.

- Rotation is delegated to logrotate + SIGUSR1; BIND's built-in `versions`/`size` parameters are warned about and ignored.
- SIGUSR1 reopens the query log file along with `--log-file`.
- A SIGHUP reload re-applies `logging{}` changes: modifications to the path and `print-*` options take effect without a restart.

## Unsupported / rejected directives

| Directive | Behavior |
|------|------|
| `type slave`, `type forward` zones | Fatal error at startup |
| `allow-update`, `dnssec-enable` | Rejected at startup |
| `rate-limit` inside a view | Warned about and ignored |
| `qps-scale` | Warned about and ignored |

Recursion is always off (`recursion no` is always in effect); ShadowDNS is a purely authoritative server.

## Example

```text
options {
    directory           "/etc/namedb";
    geoip-directory     "/usr/local/share/GeoIP/";
    listen-on           { any; };
    listen-on-v6        { none; };
    recursion           no;
    minimal-responses   yes;
    version             none;
    hostname            none;
    allow-transfer      { 192.0.2.10; 192.0.2.11; };
};

include "master.zones";
```

Zone files use the RFC 1035 master file format (`$TTL`, `$ORIGIN`, `@`, multi-line `(...)`, `;` comments) and support the `$INCLUDE` / `$include` directive (both bare paths and BIND-style double-quoted paths; the directive name is case-insensitive). Limitations: the path itself **must not contain whitespace** (an underlying limitation of the miekg/dns scanner that quoting cannot work around), and the quoted form is only valid in the top-level zone file — fragments pulled in via `$INCLUDE` are read directly by the underlying parser and must use the bare-path form internally.
