## ADDED Requirements

### Requirement: Viewless BIND-style example configuration file

The `.deb` package SHALL install a viewless BIND-style example configuration file at `/etc/shadowdns/named.conf.viewless.example` to help operators who want a Debian-style authoritative setup without GeoIP views. The file SHALL be a self-contained, valid viewless `named.conf` skeleton consisting of an `options` block and one or more top-level `zone "<domain>" { type master; file "<path>"; };` declarations (no `view` block), and SHALL carry a comment pointing to the migration guide for BIND `named.conf.default-zones` compatibility. This file is installed in addition to, and does not replace, the existing `named.conf.example`.

#### Scenario: Viewless example is installed

- **WHEN** the package is installed
- **THEN** `/etc/shadowdns/named.conf.viewless.example` SHALL exist AND contain a valid viewless `named.conf` skeleton with an `options` block and at least one top-level `zone` declaration of `type master` and no `view` block

#### Scenario: Viewless example loads without a fatal error

- **WHEN** ShadowDNS is started with `--named-conf` pointed at a copy of the installed `named.conf.viewless.example` whose zone `file` paths resolve to present zone files
- **THEN** the configuration loads without a fatal error AND the top-level zones are served via the synthesized default view
