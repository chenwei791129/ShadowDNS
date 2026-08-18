# Installation

ShadowDNS can be built from source, installed as a `.deb` package on Debian/Ubuntu, or run from the official linux/amd64 container image published on GHCR.

## Building from Source

Prerequisite: Go 1.26+.

```bash
git clone https://github.com/chenwei791129/ShadowDNS.git
cd ShadowDNS
make build
```

The binary is produced at `bin/shadowdns-<GOOS>-<GOARCH>`. To cross-compile a linux/amd64 deployment binary on macOS:

```bash
make build-linux    # produces bin/shadowdns-linux-amd64
```

## .deb Package Installation

### Building the Package

```bash
make deb    # implicitly runs make build-linux and make completions
```

### Installing

```bash
sudo dpkg -i shadowdns_<version>_amd64.deb
```

### Package Contents

| Path | Contents |
|------|------|
| `/usr/bin/shadowdns` | Main binary |
| `/lib/systemd/system/shadowdns.service` | systemd service unit |
| `/etc/logrotate.d/shadowdns` | logrotate configuration (daily rotation of `/var/log/shadowdns/*.log`; postrotate sends SIGUSR1 so the daemon reopens its log files) |
| `/etc/shadowdns/named.conf.example` | `named.conf` example |
| `/etc/shadowdns/shadowdns.yaml.example` | `shadowdns.yaml` example |
| `/usr/share/bash-completion/completions/shadowdns` | bash completion |
| `/usr/share/zsh/vendor-completions/_shadowdns` | zsh completion |
| `/usr/share/fish/vendor_completions.d/shadowdns.fish` | fish completion |

The postinstall script automatically:

- Creates the `shadowdns` system user and group (if they do not exist)
- Creates the `/var/log/shadowdns` log directory (owner `shadowdns:shadowdns`, mode 0750)
- Runs `systemctl daemon-reload`

### systemd Service

The service unit shipped with the package starts with the following parameters:

```text
/usr/bin/shadowdns \
    --named-conf /etc/shadowdns/named.conf \
    --config     /etc/shadowdns/shadowdns.yaml \
    --log-file   /var/log/shadowdns/shadowdns.log
```

Therefore, before enabling the service, place the configuration files in `/etc/shadowdns/` (you can copy and modify the `.example` files in the same directory):

```bash
sudo cp /etc/shadowdns/named.conf.example     /etc/shadowdns/named.conf
sudo cp /etc/shadowdns/shadowdns.yaml.example /etc/shadowdns/shadowdns.yaml
# After editing both files to match your environment:
sudo systemctl enable --now shadowdns
```

Security hardening highlights of the service unit:

- Runs as the unprivileged user `shadowdns`, binding port 53 via `AmbientCapabilities=CAP_NET_BIND_SERVICE`
- `ProtectSystem=strict` sandbox; only `/var/log/shadowdns` is writable
- `RuntimeDirectory=shadowdns` creates `/run/shadowdns` on every start, used by the default `pid-file "/var/run/shadowdns/pid"`
- `ExecReload` maps to SIGHUP, so `systemctl reload shadowdns` hot-reloads the configuration

### Verifying the Installation

```bash
shadowdns --version
sudo systemctl status shadowdns
```

Application-level logs are located at `/var/log/shadowdns/shadowdns.log`.

## Container Image

The official image is published for **linux/amd64 only**. Production deployments should use an exact version tag (or digest); `latest` tracks the most recent release.

```bash
docker pull ghcr.io/OWNER/shadowdns:vX.Y.Z
```

The image runs as UID/GID `65532` in a Distroless nonroot runtime. It has no shell, package manager, diagnostic client, embedded configuration, or Docker `HEALTHCHECK`. Its defaults are:

```text
--named-conf /etc/shadowdns/named.conf
--config /etc/shadowdns/shadowdns.yaml
--listen 0.0.0.0:5353
--no-color
```

The explicit listener is IPv4-only and overrides host-specific `listen-on` and `listen-on-v6` values in mounted BIND configuration. First-class dual-stack container listening is not provided by this image default.

Prepare a complete `/etc/shadowdns` tree containing `named.conf`, `shadowdns.yaml`, all included files, zone files, and GeoIP databases. Include paths are resolved relative to the including file, but a relative `options { directory "zones"; };` is resolved from the container working directory, not automatically from `/etc/shadowdns`; use an absolute container path such as `/etc/shadowdns/zones`. Every other absolute path retained in either configuration file must also be mounted at the same absolute path inside the container, or rewritten to a path under `/etc/shadowdns`; mounting `/etc/shadowdns` alone does not remap paths such as `/srv/zones`. Replace `OWNER` and `vX.Y.Z` below with the package owner and an image tag that exists in GHCR, then run:

```bash
docker run --rm --name shadowdns \
  -p 53:5353/udp \
  -p 53:5353/tcp \
  -p 9153:9153/tcp \
  --mount type=bind,src=/srv/shadowdns/config,dst=/etc/shadowdns,readonly \
  ghcr.io/OWNER/shadowdns:vX.Y.Z
```

Operational logs go to stderr by default and should be collected by the container runtime. To reload mounted configuration, send SIGHUP directly to the container; SIGTERM performs graceful shutdown:

```bash
docker kill --signal HUP shadowdns
docker stop --time 10 shadowdns
```

Health probes are deployment-owned because the image contains no `HEALTHCHECK`. Use an external DNS client to query a record known to exist in your authoritative zones for readiness, and optionally probe `http://<container-address>:9153/metrics` for liveness.

### Writable State and Optional Listeners

The configuration mount can remain read-only. If DoH ACME is enabled, mount `/var/lib/shadowdns` from persistent storage and make it writable by UID `65532`; the account key is reused across container replacement. File-backed main or query logs similarly require an explicitly mounted writable path owned by UID `65532`; the image never falls back to root.

DoH HTTPS, ACME HTTP-01, and the ephemeral API must use container ports above 1023. Map or route external ports 443 and 80 to those unprivileged listener ports.

### First GHCR Publication

After the first release workflow pushes the package, the package owner must use GitHub's package settings to:

1. Change package visibility to **Public**.
2. Confirm the package is linked to the source repository.
3. Verify an anonymous pull from a logged-out environment.
4. Confirm the exact release tag and `latest` resolve to the same image digest.

GitHub's Packages API and `gh` CLI do not currently expose a supported operation for changing package visibility, so this is a one-time UI step. Later releases retain the package's public visibility.

## In-Container End-to-End Test (for Development)

```bash
make test-deb          # requires podman or docker
make container-image   # builds shadowdns:dev for linux/amd64
make verify-container  # verifies the local image contract
```
