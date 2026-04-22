## MODIFIED Requirements

### Requirement: Example configuration files

The `.deb` package SHALL install example configuration files under `/etc/shadowdns/` to assist first-time setup.

#### Scenario: Example named.conf is installed

- **WHEN** the package is installed
- **THEN** `/etc/shadowdns/named.conf.example` SHALL exist and contain a valid `named.conf` skeleton with `options`, `geoip-directory`, and `view` blocks

#### Scenario: Example shadowdns.yaml is installed

- **WHEN** the package is installed
- **THEN** `/etc/shadowdns/shadowdns.yaml.example` SHALL exist and contain a valid unified config skeleton including the `aliases:` section (one-to-many `root: [backups]` format) and the `ephemeral_api:` section

#### Scenario: Example files are not overwritten on upgrade

- **WHEN** the package is upgraded to a newer version
- **THEN** the example files SHALL be replaced (they are examples, not user config), and no user confirmation SHALL be required
