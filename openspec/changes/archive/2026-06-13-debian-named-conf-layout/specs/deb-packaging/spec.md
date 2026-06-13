## MODIFIED Requirements

### Requirement: Example configuration files

The `.deb` package SHALL install example configuration files under `/etc/shadowdns/` to assist first-time setup.

#### Scenario: Example named.conf is installed

- **WHEN** the package is installed
- **THEN** `/etc/shadowdns/named.conf.example` SHALL exist and contain a valid `named.conf` skeleton consisting of `include "named.conf.options";` and `include "named.conf.local";` directives (Debian/Ubuntu include split)

#### Scenario: Example named.conf.options is installed

- **WHEN** the package is installed
- **THEN** `/etc/shadowdns/named.conf.options.example` SHALL exist and contain the `options` block including a `directory` and `geoip-directory` setting

#### Scenario: Example named.conf.local is installed

- **WHEN** the package is installed
- **THEN** `/etc/shadowdns/named.conf.local.example` SHALL exist and contain at least one `view` block with `match-clients` and a `zone` declaration

#### Scenario: Example shadowdns.yaml is installed

- **WHEN** the package is installed
- **THEN** `/etc/shadowdns/shadowdns.yaml.example` SHALL exist and contain a valid unified config skeleton including the `aliases:` section (one-to-many `root: [backups]` format) and the `ephemeral_api:` section

### Requirement: Container testdata generator

The project SHALL include `scripts/gen-container-testdata.go` that prepares a ready-to-use ShadowDNS configuration directory with mock GeoIP mmdb files for container testing.

#### Scenario: Generator produces complete testdata

- **WHEN** `go run scripts/gen-container-testdata.go -out <dir> -target <container-path>` is executed
- **THEN** the output directory SHALL contain `named.conf`, `named.conf.options`, `named.conf.local`, `aliases.yaml`, `db.<zone>` / `db.<zone>-<view>` zone files plus any nested `$INCLUDE` fragments under `cnames/` (Debian/Ubuntu naming, no `master/` subdirectory and no `master.zones` file), and `geoip/GeoLite2-Country.mmdb` and `geoip/GeoLite2-ASN.mmdb` with all path placeholders replaced by the target path
