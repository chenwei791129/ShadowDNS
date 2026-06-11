# CLI Reference

All flags are parsed only once, at startup. SIGHUP re-reads `named.conf`, the unified configuration file (`--config`), and the zone files using the paths recorded at startup, but does **not** re-parse flags — changing a flag value requires restarting the process.

## Startup Flags

| Flag | Default | Required | Description |
|------|--------|------|------|
| `--named-conf` | — | Yes | Path to `named.conf`. Its `geoip-directory` option controls where mmdb files are read from. |
| `--config` | — | Yes | Path to the unified ShadowDNS YAML configuration file (`aliases` + `ephemeral_api` sections); see [shadowdns.yaml](../configuration/shadowdns-yaml.md). |
| `--listen` | `:53` | No | UDP/TCP listen address. A form including a host (e.g., `127.0.0.1:53`, or the IPv6 bracket literal `[::1]:53`) overrides `listen-on`/`listen-on-v6` in `named.conf` and binds only that single address; the host-less `:PORT` form applies the port to the union of `listen-on` (IPv4) and `listen-on-v6` (IPv6) addresses — when `listen-on` is absent, all IPv4 interface addresses are used, while `listen-on-v6` is opt-in (no IPv6 listener if absent). |
| `--log-file` | (empty; output to stderr) | No | Write output to the specified file (`O_APPEND\|O_CREATE`, mode 0640). Send SIGUSR1 to make the daemon reopen the file (for logrotate postrotate use). |
| `--metrics-addr` | `:9153` | No | Prometheus `/metrics` HTTP listen address; an empty string disables it. |
| `--pprof-enable` | `false` | No | Expose `/debug/pprof/` endpoints on the metrics HTTP server; requires a non-empty `--metrics-addr`. Read only at startup; SIGHUP does not change its value. **Should only be enabled on a trusted network or a loopback binding**: pprof has no authentication, returns debugger-level runtime state, and the CPU/trace profile endpoints can be used to stall the process for an arbitrary duration. |
| `--ecs-enable` | `false` | No | Enable RFC 7871 EDNS Client Subnet processing. A valid ECS option in a query drives GeoIP view selection (country/ASN rules only; IP/CIDR ACL rules always evaluate the real source IP), and responses echo the ECS option with a scope equal to the source prefix length. Disabled by default: ECS options in queries are ignored and responses never carry one, matching BIND. Read only at startup; SIGHUP does not change its value. |
| `--reload-verify` | `hash` | No | Zone file change-detection strategy on SIGHUP reload: `hash` (default, safe against `rsync -avc --inplace`), `size` (compares mtime+size only, does not read files), `none` (always rebuild everything). |
| `--dry-run` | `false` | No | Load the configuration and zones, print a summary, then exit without opening any listener. |
| `--no-notify` | (unset) | No | Disable NOTIFY sending for the entire process lifetime. When unset, NOTIFY follows `options.notify` in `named.conf` (enabled by default); when set, it overrides the configuration directive and persists across SIGHUP reloads. |
| `--no-color` | `false` | No | Force colorless log output. The [`NO_COLOR`](https://no-color.org) environment variable is also honored; a non-TTY stderr is auto-detected and color is disabled as well. |
| `-v`, `--version` | `false` | No | Print the version and exit. |

### NOTIFY Precedence

Rules when both the flag and the configuration directive are present:

1. `--no-notify` flag explicitly set → NOTIFY is disabled for the process lifetime
2. Otherwise, `options.notify` in `named.conf` takes effect (`yes` or `no`)
3. Otherwise, NOTIFY defaults to enabled

`--no-notify` is deliberately designed as "off-only" — to re-enable NOTIFY, remove the flag and restart, avoiding the double-negative confusion of `--no-notify=false`.

## Subcommands

### shadowdns reload

```bash
shadowdns reload --named-conf /etc/shadowdns/named.conf
```

Locates the running server via the pid-file configured in `named.conf` and sends SIGHUP. Only `--named-conf` is accepted; server startup flags are rejected.

### shadowdns prune-backup

```bash
shadowdns prune-backup \
    --named-conf /etc/shadowdns/named.conf \
    --config /etc/shadowdns/shadowdns.yaml \
    [--apply]
```

Compares backup zone files offline against the root zone of their alias and reports redundant records (dry-run by default); with `--apply`, rewrites the files. Opens no sockets and sends no signals to any running server.

Comparison logic:

- For every `(view, backup-zone)` pair declared in `named.conf`, compares against the corresponding root zone in the same view.
- Non-overridable record types (everything except `TXT`/`MX`/`SRV`) are always flagged as redundant.
- Overridable types are flagged only when the entire RRSet is identical to the root (ignoring TTL and ordering).
- The `SOA` and apex `NS` RRSets are always preserved, ensuring the zone file remains RFC 1035 valid.
- With `--apply`, each rewritten file is replaced atomically, and the pre-rewrite copy is kept at `<path>.bak`.

### shadowdns completion

```bash
shadowdns completion bash|zsh|fish
```

Generates the autocompletion script for the specified shell. The `.deb` package installation already ships completions for all three shells.

## Signals

| Signal | Behavior |
|--------|------|
| `SIGHUP` | Hot reload: re-reads `named.conf`, `shadowdns.yaml`, zone files, and the GeoIP mmdb files. On failure, the previous state is kept. |
| `SIGUSR1` | Reopens the `--log-file` and query log file descriptors (for logrotate postrotate use). |
