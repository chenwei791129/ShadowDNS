## MODIFIED Requirements

### Requirement: Parse aliases.yaml

The config-loader SHALL obtain the root-to-backup alias map from the `aliases` section of the unified ShadowDNS YAML configuration file loaded via the `shadowdns-config` capability, not from a standalone `aliases.yaml` file. The `-aliases` CLI flag SHALL NOT be accepted: the server binary SHALL fail to start with a clear error if `-aliases` is passed. The alias-map data shape and the duplicate/self-alias rejection rules SHALL remain unchanged; only the source file and loader entry point move.

#### Scenario: Well-formed aliases section produces alias map

- **WHEN** the unified config file contains `aliases: {backup.com: root.com, mirror.com: root.com}`
- **THEN** the loader produces a map `{backup.com → root.com, mirror.com → root.com}`

#### Scenario: Backup appearing under two roots is rejected

- **WHEN** the `aliases` section declares the same backup domain under two different roots
- **THEN** the loader returns an error citing both root entries

#### Scenario: Backup domain equal to root domain is rejected

- **WHEN** the `aliases` section lists `root.com: root.com`
- **THEN** the loader returns an error

#### Scenario: Missing aliases section yields empty map

- **WHEN** the unified config file omits the `aliases` key
- **THEN** the loader returns an empty alias map AND logs an info message; the server still starts normally

#### Scenario: `-aliases` flag is rejected

- **WHEN** the server is started with `-aliases /etc/shadowdns/aliases.yaml`
- **THEN** the server SHALL fail to start with an error indicating that `-aliases` is no longer supported and that aliases must be provided via `-config`
