## MODIFIED Requirements

### Requirement: Load unified ShadowDNS configuration from a YAML file

The shadowdns-config loader SHALL parse a single YAML file specified by the `--config` CLI flag. The file is a single YAML document containing the following top-level sections:

- `aliases` (`map[string][]string`, optional): Mapping from each root domain to its list of backup domains. When the key is absent or the map is empty, no aliases are loaded.
- `ephemeral_api` (object, optional): Configuration for the ephemeral TXT API server. When the key is absent, the API server is not started.

The loader SHALL use strict decoding: unknown top-level keys or unknown fields inside recognized sections SHALL cause a load error that identifies the offending key. A value under `aliases` whose type is not a list of strings (for example, a bare string such as `backup.com: root.com`) SHALL be rejected by the YAML decoder as a type mismatch.

#### Scenario: Valid config with both sections

- **WHEN** the config file contains `aliases: {root.com: [backup.com]}` and `ephemeral_api: {listen: "127.0.0.1:8053", allow: ["10.0.0.5"]}`
- **THEN** the loader SHALL return a config where the alias map has one entry `{backup.com. -> root.com.}` and `ephemeral_api` is populated

#### Scenario: Aliases-only config

- **WHEN** the config file contains `aliases: {root.com: [backup.com, mirror.com]}` and no `ephemeral_api` key
- **THEN** the loader SHALL return a config with the alias map populated with both backup entries and `ephemeral_api` marked as disabled

#### Scenario: Ephemeral-API-only config

- **WHEN** the config file contains only `ephemeral_api: {listen: "127.0.0.1:8053", allow: ["10.0.0.5"]}`
- **THEN** the loader SHALL return a config with an empty alias map and `ephemeral_api` populated

#### Scenario: Empty aliases map is accepted

- **WHEN** the config file contains `aliases: {}`
- **THEN** the loader SHALL return a config with an empty alias map and no error

#### Scenario: Legacy one-to-one aliases format is rejected

- **WHEN** the config file contains `aliases: {backup.com: root.com}` (a bare string value under a root key)
- **THEN** the loader SHALL return a YAML decoding error identifying the type mismatch; the server SHALL NOT start with the partial configuration

#### Scenario: Unknown top-level key fails

- **WHEN** the config file contains a top-level key that is not `aliases` or `ephemeral_api`
- **THEN** the loader SHALL return an error that names the unknown key

#### Scenario: Config file does not exist

- **WHEN** the path passed to `--config` does not exist
- **THEN** the loader SHALL return an error identifying the missing file path

### Requirement: Validate aliases section

The loader SHALL validate the `aliases` section with the same semantic rules that previously applied to the legacy `aliases.yaml` file. After YAML decoding, the loader SHALL flatten the `map[root][]backup` structure into a normalized backup-to-root map; during flattening the loader SHALL reject the following conditions:

- The same backup domain (after normalization) appears under two different root keys.
- A backup domain is listed under a root key whose value (after normalization) equals that backup domain (self-alias).
- Any backup entry or root key is empty or contains whitespace.

An empty list of backups under a root key SHALL be accepted and contribute no entries to the alias map.

#### Scenario: Duplicate backup under different roots fails

- **WHEN** the `aliases` section contains `root-a.com: [shared.com]` and `root-b.com: [shared.com]`
- **THEN** the loader SHALL return an error naming the duplicate backup domain and both root keys

#### Scenario: Self-alias entry fails

- **WHEN** the `aliases` section contains an entry where a backup equals its root (e.g., `example.com: [example.com]`)
- **THEN** the loader SHALL return an error naming the self-alias entry

#### Scenario: Empty backup list is accepted

- **WHEN** the `aliases` section contains `root.com: []`
- **THEN** the loader SHALL accept the entry and the resulting alias map SHALL contain no mappings for `root.com`

#### Scenario: Multiple backups under one root are all mapped to that root

- **WHEN** the `aliases` section contains `root.com: [backup.com, mirror.com, shadow.com]`
- **THEN** the loader SHALL return an alias map with three entries all pointing to `root.com.`
