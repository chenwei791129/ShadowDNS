# named.conf Compatibility

ShadowDNS reads your existing BIND `named.conf` directly — no format conversion needed. This page covers the supported directive scope, view matching semantics, RRL and query logging configuration, the tiered tolerance contract, and the directives that are rejected.

## Tiered tolerance contract

ShadowDNS is a **drop-in** reader of a BIND `named.conf`: it loads any syntactically valid configuration and reacts to each construct at one of four tiers, so the behavior is predictable rather than discovered by trial. The governing principle is **fail-closed** — a construct ShadowDNS cannot evaluate is never allowed to *widen* access.

| Tier | What ShadowDNS does | Representative constructs |
|------|---------------------|---------------------------|
| **Silent** (DEBUG / no log) | Consumes and ignores the construct without operator-visible noise | Unrecognized non-access-control, non-control-plane directives at top level or view scope (`masters`, `dnssec-enable`, …); access-control directives inside a `zone` block (skipped, still not enforced); query logging disabled via a built-in channel (`default_syslog`, `null`, …) |
| **INFO** | Skips and logs once at INFO — informational, no action required | Recursion-family directives (`recursion`, `forwarders`, `dnssec-validation`); a zone whose `type` is not `master` (dropped, `file` never opened) |
| **WARN** | Skips/drops and logs at WARN — review recommended | Access-control directives at **top level or view scope** (`allow-query`, `allow-recursion`, `allow-transfer`, `allow-update`, `allow-notify`, `blackhole`) with a "does not enforce" message; control-plane / security directives ShadowDNS does not implement (`controls`, `key`, `server`, `statistics-channels`, `trusted-keys`, `managed-keys`, `trust-anchors`) with a "has no effect" message; an unknown `match-clients` token (dropped, fail-closed); an undefined or cyclic `acl` reference (fail-closed); an `acl` defined with a reserved built-in name (`any`/`none`/`localhost`/`localnets`, ignored — references resolve to the built-in); a directive that is a one-edit misspelling of a structural keyword (`view`, `zone`, `options`, …), skipped with a suggested correction; a duplicate `acl` / `options` / top-level-zone name (last wins); an unsupported `listen-on` token; `qps-scale` and a view-scope `rate-limit`; a non-last `any` view |
| **fail-closed (fatal)** | Aborts startup citing the offending file and line | Genuine syntax errors (unbalanced brace, missing `;`, unterminated block); a malformed `geoip asnum` value (no leading `AS<number>`); a `geoip country` code that is not a 2-letter ISO 3166-1 code; `geoip-directory` unset while a view uses `geoip` rules; mixing `view` blocks with top-level zones |

### Fail-closed doctrine

The fail-closed doctrine governs the WARN tier specifically: when an access-control element of a `match-clients` list (or an `acl` body) cannot be evaluated — an unknown token, a reference to an undefined named ACL, or a reference cycle — a **positive** element is dropped and treated as **never-matching**, never as **match-all**. A **negated** unevaluable element (e.g. `!undefined-acl`) is instead replaced by a list-rejecting marker: dropping it outright would let a following `any` match everyone, so its lost exclusion narrows the list (the whole list rejects) rather than widening it. The same replacement applies to a **negated reference whose acl resolves to an empty list** — whether that acl was declared empty or emptied because its own members were dropped fail-closed — since a silently discarded exclusion would likewise widen the list. A view built entirely from unevaluable elements therefore serves nothing rather than matching everyone. This guarantees a typo in a `match-clients` list can never silently expose a restricted view to every client.

The one access-control directive ShadowDNS **does** enforce is **options-scope `allow-transfer`**, which is the AXFR ACL (see [Migrating from BIND](../migration.md#access-control-model-differences) for the scope distinction). View- or zone-scope `allow-*` directives fall into the WARN tier above and are not enforced.

## Supported options directives

The `options` block supports: `directory`, `geoip-directory`, `listen-on`, `listen-on-v6`, `allow-transfer`, `recursion`, `minimal-responses`, `version`, `hostname`, `transfer-format`, `notify`.

`geoip-directory` is required **only when geo rules are used**: if any view's `match-clients` contains `geoip country` / `geoip asnum` rules, it must be set, and a config violating this fails at startup with an error naming the first offending view. When set (even without geo rules), the mmdb files are loaded and validated as usual; an absent `geoip-directory` and an empty one (`geoip-directory "";`) are equivalent and count as unset. See [GeoIP Databases](geoip.md) for details.

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

- Uses **first-match** semantics (same as BIND): the address-match-list is evaluated in declaration order, and the **first element that matches** the client decides the outcome — a positive element selects the view, while a negated element (`!`) that matches **rejects** the view (evaluation falls through to the next view). If no element matches, the view is not selected.
- When no view matches, the response is **REFUSED**.
- Supported element forms:

| Element | Example | Matches |
|----------|------|------|
| GeoIP country | `geoip country TW` | the geo lookup address's country |
| GeoIP ASN | `geoip asnum "AS64500 Example ISP"` | the geo lookup address's AS number |
| Single IPv4 address | `192.0.2.10` | the source IP |
| IPv4 CIDR | `198.51.100.0/24` | the source IP |
| Named acl reference | `internal` | whatever the referenced `acl` matches |
| Nested group | `{ 192.0.2.0/24; 198.51.100.0/24; }` | the group's own ordered list |
| Negation | `! 192.0.2.0/24` | inverts: a matching client is **rejected** |
| `any` | `any` | every client (catch-all) |
| `none` | `none` | no client |
| `localhost` | `localhost` | the server's own addresses |
| `localnets` | `localnets` | the networks attached to the server's interfaces |

GeoIP country/ASN elements are evaluated against the geo lookup address (the [ECS](../guides/ecs.md)-derived address when present, otherwise the source IP); every other element is evaluated against the transport source IP, so a forged ECS address can never satisfy an IP/CIDR/named-acl rule.

!!! warning "The `any` view must be declared last"
    A view whose `match-clients` contains `any;` matches **all** clients. If it precedes more specific views (such as GeoIP views), those will never be evaluated. ShadowDNS logs a WARN at startup when `any` is used by a view that is not the last one, but does not block startup.

!!! warning "ASN description string format"
    The `geoip asnum` string must match the `"AS<number> <description>"` format (the parsing rule is `^AS(\d+)\s`); the description text is ignored. A string not starting with `AS` + digits + whitespace (e.g. `"64500"` missing the `AS` prefix) causes startup failure.

!!! warning "Country code format"
    The `geoip country` code must be a 2-letter ISO 3166-1 alpha-2 code (e.g. `TW`, `US`); it is matched case-insensitively. A code that is not exactly two letters (e.g. `usa`, a digit, a CIDR) causes startup failure rather than degrading to a rule that silently never matches.

### Named ACLs

Define a reusable client group with a top-level `acl` block, then reference it by name from any view's `match-clients` (or from another `acl`):

```text
acl "internal" {
    10.0.0.0/8;
    192.0.2.0/24;
};

view "internal" {
    match-clients { internal; };
    // ...
};

view "external" {
    match-clients { ! internal; any; };   // everyone except internal
    // ...
};
```

- An `acl` body uses the **same element grammar** as `match-clients` — including `geoip` rules, `!` negation, nested groups, the built-in ACLs, and references to other named ACLs.
- A reference resolves to the named acl's list and is evaluated recursively; a leading `!` negates the whole reference.
- **Undefined references are fail-closed:** a positive reference to a name with no `acl` definition is dropped with a WARN and never matches — the enclosing view serves nothing rather than matching everyone. A *negated* undefined reference (`!name`) is replaced by a whole-list reject instead of being dropped, so the exclusion it expressed cannot silently widen access. A negated reference whose acl resolves to an **empty** list (declared empty, or emptied because its members were themselves dropped) is replaced the same way, for the same reason.
- A reference **cycle** (`a` → `b` → `a`) is broken with a WARN.
- A **duplicate** `acl` name keeps the **last** definition and logs a WARN.

!!! note "`localhost` / `localnets` are resolved at load time"
    The `localhost` (the server's own addresses) and `localnets` (the directly attached networks) built-ins are expanded from the host's network interfaces when the configuration is loaded, and re-enumerated on each reload.

## Viewless configurations (implicit `_default` view)

ShadowDNS does not require any `view` block. You can declare zones at the top level — outside every `view` block — directly in `named.conf` or any of its `include` files.

On Debian/Ubuntu the configuration is conventionally split across the `named.conf` / `named.conf.options` / `named.conf.local` include layout. The top-level `named.conf` just pulls in the other two:

```text
// named.conf
include "named.conf.options";
include "named.conf.local";
```

```text
// named.conf.options
options {
    directory   "/etc/bind";
    listen-on   { any; };
    recursion   no;
};
```

```text
// named.conf.local
zone "example.com" {
    type master;
    file "db.example.com";
};

zone "example.net" {
    type master;
    file "db.example.net";
};
```

The `directory "/etc/bind"` here is Debian-idiomatic (authoritative zone files alongside the config).

A top-level zone body follows the **exact same rules** as a zone inside a view: only `type master` is supported, and a relative `file` path keeps the same resolution semantics used during parsing.

!!! warning "Declare `options` before any top-level zone"
    Place the `options` block ahead of your top-level zone declarations. Otherwise a relative `file` path is resolved against the directory of the file in which the zone is declared, not against `options.directory`.

### How the `_default` view is synthesized

When the whole configuration (including every `include`) contains **no `view` block** but has **at least one top-level zone**, ShadowDNS synthesizes a view named `_default`:

- Its `match-clients` is equivalent to `{ any; }` — it matches every source IP.
- It contains all top-level zones, in declaration order.

This mirrors BIND's behavior when no views are configured.

### No GeoIP required

The synthesized `_default` view holds only an `any` rule and **no geo rules**, so a viewless configuration never needs `geoip-directory` and never needs any mmdb file. This is the conditional-requirement behavior described in [GeoIP Databases](geoip.md#running-without-geoip).

### Mixing views and top-level zones is a startup error

If the configuration contains **any `view` block** *and* **any top-level zone** — regardless of declaration order, and regardless of which files they are spread across — ShadowDNS fails to start with a fatal error. The message names the first top-level zone (its name, source file path, and line number). This mirrors BIND's rule that once views are used, every zone must live inside a view.

### Duplicate top-level zone names

Duplicate top-level zone names are **not fatal** — every entry is retained. During synthesis, ShadowDNS emits one Warn per duplicated name, listing the location of every declaration of that name and noting that the **last declaration wins** at serving time.

!!! warning "Two surface differences when migrating from a viewless BIND"
    - **Query log:** each line carries a `view _default:` clause, whereas a viewless BIND query log has no view clause. Downstream log parsers must account for this extra field.
    - **Prometheus metrics:** the view label takes the value `_default`.

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

## Directive handling summary

Every BIND directive ShadowDNS encounters resolves to one of the four tiers in the [Tiered tolerance contract](#tiered-tolerance-contract). The table below summarizes the cases operators ask about most:

| Directive | Tier | Behavior |
|------|------|------|
| `type slave`, `type forward` zones | INFO | Zone dropped (not served), its `file` never opened; loading continues |
| `allow-update`, `allow-notify`, `blackhole` | WARN | Skipped and logged as not enforced |
| `allow-query` / `allow-recursion` / `allow-transfer` at **view scope** | WARN | Skipped and logged as not enforced; the same directives inside a `zone` block are skipped silently — neither scope is enforced (see [access-control model](../migration.md#access-control-model-differences)) |
| `controls`, `key`, `server`, `statistics-channels`, `trusted-keys` | WARN | Skipped and logged as having no effect (ShadowDNS does not implement these control-plane / security features) |
| `dnssec-enable` | Silent | Skipped without operator-visible noise |
| `recursion`, `forwarders`, `dnssec-validation` | INFO | Skipped; ShadowDNS is authoritative-only |
| `rate-limit` inside a view | WARN | Skipped; RRL is supported only at `options` scope |
| `qps-scale` | WARN | Skipped; load-adaptive scaling is not implemented |
| Unbalanced brace / missing `;` / unterminated block | fail-closed | Startup aborts citing the file and line |
| `geoip asnum` without a leading `AS<number>` | fail-closed | Startup aborts |
| `geoip country` code that is not a 2-letter ISO 3166-1 code | fail-closed | Startup aborts |
| `view` blocks mixed with top-level zones | fail-closed | Startup aborts naming the first top-level zone |

Recursion is always off (`recursion no` is always in effect); ShadowDNS is a purely authoritative server. **Options-scope `allow-transfer` is the one access-control directive ShadowDNS does enforce** — it is the AXFR ACL (see [Migrating from BIND](../migration.md#access-control-model-differences)).

## Example

On Debian/Ubuntu the configuration is split across the include layout. The top-level `named.conf` only wires the pieces together:

```text
// named.conf
include "named.conf.options";
include "named.conf.local";
```

`named.conf.options` holds the global `options` (and `logging`) blocks:

```text
// named.conf.options
options {
    directory           "/etc/bind";
    geoip-directory     "/usr/local/share/GeoIP/";
    listen-on           { any; };
    listen-on-v6        { none; };
    recursion           no;
    minimal-responses   yes;
    version             none;
    hostname            none;
    allow-transfer      { 192.0.2.10; 192.0.2.11; };
};
```

`named.conf.local` holds the `view` blocks and their zones. Split-horizon zones that share a name across views use the `db.<zone>-<view>` hyphen naming convention so each view's copy lives in its own file:

```text
// named.conf.local
view "th" {
    match-clients { geoip country TH; };
    zone "example.com" {
        type master;
        file "db.example.com-th";
    };
};

view "other" {
    match-clients { any; };
    zone "example.com" {
        type master;
        file "db.example.com-other";
    };
};
```

The `directory "/etc/bind"` here is Debian-idiomatic (authoritative zone files alongside the config). A zone that exists in only one view needs no view suffix — use plain `db.<zone>`, e.g. `db.include-test.example`.

Zone files use the RFC 1035 master file format (`$TTL`, `$ORIGIN`, `@`, multi-line `(...)`, `;` comments) and support the `$INCLUDE` / `$include` directive (both bare paths and BIND-style double-quoted paths; the directive name is case-insensitive). Limitations: the path itself **must not contain whitespace** (an underlying limitation of the miekg/dns scanner that quoting cannot work around), and the quoted form is only valid in the top-level zone file — fragments pulled in via `$INCLUDE` are read directly by the underlying parser and must use the bare-path form internally.
