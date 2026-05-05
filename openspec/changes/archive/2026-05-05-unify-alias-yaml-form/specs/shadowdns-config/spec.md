## MODIFIED Requirements

### Requirement: Load unified ShadowDNS configuration from a YAML file

The shadowdns-config loader SHALL parse a single YAML file specified by the `--config` CLI flag. The file is a single YAML document containing the following top-level sections:

- `aliases` (optional): Mapping from each root domain to an object with `members` (non-empty list of backup domain strings) and `rewrite_rdata_labels` (bool, optional, default `false`). When the key is absent or the map is empty, no aliases are loaded.
- `ephemeral_api` (object, optional): Configuration for the ephemeral TXT API server. When the key is absent, the API server is not started.

The loader SHALL use strict decoding: unknown top-level keys or unknown fields inside recognized sections SHALL cause a load error that identifies the offending key. A value under `aliases` whose YAML node type is not a mapping with the documented fields (for example, a sequence of strings such as `root.com: [backup.com]`, or a bare string such as `backup.com: root.com`) SHALL be rejected by the YAML decoder as a type mismatch.

#### Scenario: Valid config with both sections

- **WHEN** the config file contains `aliases: {root.com: {members: [backup.com]}}` and `ephemeral_api: {listen: "127.0.0.1:8053", allow: ["10.0.0.5"]}`
- **THEN** the loader SHALL return a config where the alias map has one entry `{backup.com. -> root.com.}` with `rewrite_rdata_labels: false` and `ephemeral_api` is populated

#### Scenario: Aliases object form with rewrite_rdata_labels enabled

- **WHEN** the config file contains `aliases: {root.com: {members: [backup.com, mirror.com], rewrite_rdata_labels: true}}`
- **THEN** the loader SHALL return a config where both `backup.com.` and `mirror.com.` map to root `root.com.` with `rewrite_rdata_labels: true`

#### Scenario: Aliases object form omits rewrite_rdata_labels

- **WHEN** the config file contains `aliases: {root.com: {members: [backup.com]}}`
- **THEN** the loader SHALL return a config where `backup.com.` maps to root `root.com.` with `rewrite_rdata_labels: false`

#### Scenario: Multiple roots with mapping form

- **WHEN** the config file contains `aliases: {root-a.net: {members: [alias-a.net]}, root-b.net: {members: [alias-b.net], rewrite_rdata_labels: true}}`
- **THEN** the loader SHALL return a config where `alias-a.net.` maps to `root-a.net.` with the flag false, and `alias-b.net.` maps to `root-b.net.` with the flag true

#### Scenario: Ephemeral-API-only config

- **WHEN** the config file contains only `ephemeral_api: {listen: "127.0.0.1:8053", allow: ["10.0.0.5"]}`
- **THEN** the loader SHALL return a config with an empty alias map and `ephemeral_api` populated

#### Scenario: Empty aliases map is accepted

- **WHEN** the config file contains `aliases: {}`
- **THEN** the loader SHALL return a config with an empty alias map and no error

#### Scenario: Sequence form aliases value is rejected

- **WHEN** the config file contains `aliases: {root.com: [backup.com]}` (a sequence of backup strings under a root key)
- **THEN** the loader SHALL return a YAML decoding error identifying the type mismatch and naming `members` as the required field; the server SHALL NOT start with the partial configuration

#### Scenario: Legacy one-to-one aliases format is rejected

- **WHEN** the config file contains `aliases: {backup.com: root.com}` (a bare string value under a root key)
- **THEN** the loader SHALL return a YAML decoding error identifying the type mismatch; the server SHALL NOT start with the partial configuration

#### Scenario: Aliases object form with unknown field is rejected

- **WHEN** the config file contains `aliases: {root.com: {members: [backup.com], unknown_flag: true}}`
- **THEN** the loader SHALL return an error that names the unknown field `unknown_flag`

#### Scenario: Aliases object form missing members is rejected

- **WHEN** the config file contains `aliases: {root.com: {rewrite_rdata_labels: true}}`
- **THEN** the loader SHALL return an error indicating that `members` is required when the alias value is an object

#### Scenario: Aliases object form with empty members is rejected

- **WHEN** the config file contains `aliases: {root.com: {members: []}}`
- **THEN** the loader SHALL return an error indicating that `members` MUST be non-empty

#### Scenario: Unknown top-level key fails

- **WHEN** the config file contains a top-level key that is not `aliases` or `ephemeral_api`
- **THEN** the loader SHALL return an error that names the unknown key

#### Scenario: Config file does not exist

- **WHEN** the path passed to `--config` does not exist
- **THEN** the loader SHALL return an error identifying the missing file path

### Requirement: Validate aliases section

The loader SHALL validate the `aliases` section after YAML decoding. The loader SHALL flatten the `map[root]{members, rewrite_rdata_labels}` structure into a normalized backup-to-root map; during flattening the loader SHALL reject the following conditions:

- The same backup domain (after normalization) appears under two different root keys.
- A backup domain is listed under a root key whose value (after normalization) equals that backup domain (self-alias).
- Any backup entry or root key is empty or contains whitespace.

A root key whose `members` list is omitted entirely from a mapping value SHALL be rejected at YAML decoding time (covered by the schema requirement above). A root key with a present but empty `members` list SHALL also be rejected at decoding time.

#### Scenario: Duplicate backup under different roots fails

- **WHEN** the `aliases` section contains `root-a.com: {members: [shared.com]}` and `root-b.com: {members: [shared.com]}`
- **THEN** the loader SHALL return an error naming the duplicate backup domain and both root keys

#### Scenario: Self-alias entry fails

- **WHEN** the `aliases` section contains an entry where a backup equals its root (e.g., `example.com: {members: [example.com]}`)
- **THEN** the loader SHALL return an error naming the self-alias entry

#### Scenario: Multiple backups under one root are all mapped to that root

- **WHEN** the `aliases` section contains `root.com: {members: [backup.com, mirror.com, shadow.com]}`
- **THEN** the loader SHALL return an alias map with three entries all pointing to `root.com.`
