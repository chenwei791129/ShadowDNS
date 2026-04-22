## MODIFIED Requirements

### Requirement: Parse aliases.yaml

The config-loader SHALL obtain the alias map from the `aliases` section of the unified ShadowDNS YAML configuration file loaded via the `shadowdns-config` capability, not from a standalone `aliases.yaml` file. The `aliases` section SHALL use a root-to-backups structure: each top-level key is a root domain, and each value is a list of backup domains for that root. The `--aliases` CLI flag SHALL NOT be accepted: because the flag is not registered in the cobra command, passing `--aliases` SHALL cause the server binary to fail to start with cobra's standard `unknown flag: --aliases` error. The resulting in-memory alias map data shape (backup-to-root) and the duplicate/self-alias rejection rules SHALL remain unchanged; only the YAML surface syntax and the loader entry point differ from the legacy `aliases.yaml` behavior.

#### Scenario: Well-formed aliases section produces alias map

- **WHEN** the unified config file contains `aliases: {root.com: [backup.com, mirror.com]}`
- **THEN** the loader SHALL produce a map `{backup.com → root.com, mirror.com → root.com}`

#### Scenario: Backup appearing under two roots is rejected

- **WHEN** the `aliases` section lists the same backup domain under two different root keys
- **THEN** the loader SHALL return an error citing the duplicate backup and both root entries

#### Scenario: Backup domain equal to root domain is rejected

- **WHEN** the `aliases` section lists `root.com: [root.com]`
- **THEN** the loader SHALL return an error identifying the self-alias

#### Scenario: Missing aliases section yields empty map

- **WHEN** the unified config file omits the `aliases` key
- **THEN** the loader SHALL return an empty alias map AND SHALL log an info message; the server SHALL still start normally

#### Scenario: Legacy one-to-one aliases format is rejected

- **WHEN** the `aliases` section contains a bare-string value such as `backup.com: root.com`
- **THEN** the loader SHALL return a YAML decoding error identifying the type mismatch and SHALL NOT build an alias map

#### Scenario: `--aliases` flag is rejected

- **WHEN** the server is started with `--aliases /etc/shadowdns/aliases.yaml`
- **THEN** the server SHALL fail to start with cobra's `unknown flag: --aliases` error; operators are expected to provide aliases via `--config` instead
