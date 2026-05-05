## ADDED Requirements

### Requirement: deb package SHALL install a logrotate configuration

The `shadowdns_<version>_amd64.deb` package SHALL install a logrotate configuration file at `/etc/logrotate.d/shadowdns` (owned by `root:root`, mode `0644`). The configuration file SHALL declare daily rotation of `/var/log/shadowdns/*.log`, retain 14 rotated copies, compress rotated files with `delaycompress`, tolerate missing files (`missingok`), skip rotation for empty files (`notifempty`), and recreate the active log file with mode `0640` owned by `shadowdns:shadowdns` after rotation.

The configuration SHALL include a `postrotate` script that sends `SIGUSR1` to the running daemon so the in-process file descriptor reopens onto the freshly created file. Because the daemon's pid-file path is configured in `named.conf` (operator-controlled, e.g. `/var/run/named/pid`) and is therefore not predictable from packaging, the script SHALL resolve the running PID via `systemctl show --property MainPID --value shadowdns.service` so only the systemd-managed instance is signalled. The script SHALL guard the signal-send so an inactive unit (`MainPID=0`), an environment without systemd available (the `systemctl` invocation fails), or a missing target process does not produce an error exit.

The logrotate configuration MUST be declared in `nfpm.yaml` so it is included in every produced `.deb` artifact (verifiable via `dpkg -L shadowdns | grep logrotate.d`).

#### Scenario: Installed package contains the logrotate config

- **WHEN** `shadowdns_<version>_amd64.deb` is installed via `dpkg -i`
- **THEN** `/etc/logrotate.d/shadowdns` exists, is owned by `root:root` with mode `0644`, and contains a `/var/log/shadowdns/*.log { ... }` block declaring `daily`, `rotate 14`, `compress`, `delaycompress`, `missingok`, `notifempty`, `create 0640 shadowdns shadowdns`, and a `postrotate` script that resolves the daemon PID via `systemctl show --property MainPID --value shadowdns.service` and sends `SIGUSR1` to that PID

#### Scenario: postrotate tolerates absent daemon

- **GIVEN** no `shadowdns` process is running on the host (or systemd is not available, e.g. inside a non-init container)
- **WHEN** `logrotate` runs against `/etc/logrotate.d/shadowdns`
- **THEN** the rotation completes with exit code 0 and no error is emitted from the `postrotate` block (the `systemctl show` invocation either returns `MainPID=0` for an inactive unit or exits non-zero in environments without systemd; both branches are absorbed by the `|| true` and the `[ "$pid" != "0" ]` guard around the `kill`)

### Requirement: systemd unit SHALL pass --log-file flag by default

The `packaging/shadowdns.service` unit's `ExecStart=` line SHALL include `--log-file /var/log/shadowdns/shadowdns.log` so that a fresh deb installation produces a daemon that writes to the rotated log file path without requiring operator changes to `override.conf`.

The unit's existing directives that grant write access to `/var/log/shadowdns` (`ReadWritePaths=/var/log/shadowdns`) and the `postinstall.sh` step that creates the directory with `shadowdns:shadowdns` ownership SHALL remain in place.

Operators MAY override the flag through a drop-in at `/etc/systemd/system/shadowdns.service.d/override.conf` to disable file logging (e.g., revert to stderr/journal) without modifying the packaged unit file.

#### Scenario: Default ExecStart writes to file

- **GIVEN** a freshly installed `shadowdns` deb with no override drop-in
- **WHEN** the daemon is started via `systemctl start shadowdns`
- **THEN** the running process command line (visible via `ps -p $MAINPID -o args=`) contains `--log-file /var/log/shadowdns/shadowdns.log` and log records appear in that file

#### Scenario: Operator can override via drop-in

- **GIVEN** an operator-supplied `/etc/systemd/system/shadowdns.service.d/override.conf` that resets `ExecStart=` and sets a new `ExecStart=` line without `--log-file`
- **WHEN** the daemon is restarted
- **THEN** the daemon runs without the `--log-file` flag and routes log output to `os.Stderr` (and therefore systemd-journal), demonstrating the package default is overridable
