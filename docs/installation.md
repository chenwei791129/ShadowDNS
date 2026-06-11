# Installation

ShadowDNS offers two installation methods: building from source, or installing the `.deb` package on Debian/Ubuntu (which includes a systemd service, logrotate configuration, and shell completions).

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

## In-Container End-to-End Test (for Development)

```bash
make test-deb    # requires podman or docker
```
