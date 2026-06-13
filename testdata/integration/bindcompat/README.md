# bindcompat fixture

A Debian/BIND-style **viewless** configuration used by the BIND-compatibility
integration test (`test/integration/bind_compat_test.go`). It exercises the
tolerant ("skip-unknown") named.conf parser against the kind of config an
operator migrating off BIND points `--named-conf` at.

## Layout

Mirrors the stock `/etc/bind` split:

| File | Role |
|------|------|
| `named.conf` | Thin entry point; `include`s the three files below. |
| `named.conf.options` | `options { ... }` — no `directory`, no `geoip-directory`. |
| `named.conf.local` | Operator-added `acl` / `key` / `controls` blocks (skipped, not fatal). |
| `named.conf.default-zones` | Root `zone "." { type hint; }` (dropped) plus the `type master` localhost/reverse zones. |
| `db.local`, `db.127`, `db.0`, `db.255` | Zone files for the four `type master` default zones. |
| `shadowdns.yaml` | Empty alias map. |

## What it proves

- A real BIND config loads without a fatal error under the tolerant parser.
- The root `type hint` zone is **dropped** (not served). Its `file "db.root"`
  is intentionally **absent** — if the loader opened it, loading would fail, so
  the missing file doubles as proof the dropped zone's file is never opened.
- The `type master` localhost zone serves authoritatively.
- Top-level `acl` / `key` / `controls` blocks are skipped, not fatal.

The `match-clients` fail-closed behavior is covered by a separate view-based
config built inline in the test (a viewless config cannot also declare views).

## Sanitization

All addresses are loopback (`127.0.0.1`, `::1`) or RFC 5737 documentation
ranges (`192.0.2.0/24`, `198.51.100.0/24`); all names are `localhost` or RFC
2606 reserved domains. No real hosts, IPs, or zones appear here.
