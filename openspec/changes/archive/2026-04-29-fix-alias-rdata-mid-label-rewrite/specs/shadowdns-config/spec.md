## MODIFIED Requirements

### Requirement: Load unified ShadowDNS configuration from a YAML file

The shadowdns-config loader SHALL parse a single YAML file specified by the `--config` CLI flag. The file is a single YAML document containing the following top-level sections:

- `aliases` (optional): Mapping from each root domain to either (a) a list of backup domain strings, or (b) an object with `members` (list of backup domain strings) and `rewrite_rdata_labels` (bool, default `false`). When the key is absent or the map is empty, no aliases are loaded. The list form is equivalent to the object form with `rewrite_rdata_labels: false`.
- `ephemeral_api` (object, optional): Configuration for the ephemeral TXT API server. When the key is absent, the API server is not started.

The loader SHALL use strict decoding: unknown top-level keys or unknown fields inside recognized sections SHALL cause a load error that identifies the offending key. A value under `aliases` whose YAML node type is neither a sequence of strings nor a mapping with the documented fields (for example, a bare string such as `backup.com: root.com`) SHALL be rejected by the YAML decoder as a type mismatch.

#### Scenario: Valid config with both sections

- **WHEN** the config file contains `aliases: {root.com: [backup.com]}` and `ephemeral_api: {listen: "127.0.0.1:8053", allow: ["10.0.0.5"]}`
- **THEN** the loader SHALL return a config where the alias map has one entry `{backup.com. -> root.com.}` with `rewrite_rdata_labels: false` and `ephemeral_api` is populated

#### Scenario: Aliases-only config (list form)

- **WHEN** the config file contains `aliases: {root.com: [backup.com, mirror.com]}` and no `ephemeral_api` key
- **THEN** the loader SHALL return a config with the alias map populated with both backup entries (each with `rewrite_rdata_labels: false`) and `ephemeral_api` marked as disabled

#### Scenario: Aliases object form with rewrite_rdata_labels enabled

- **WHEN** the config file contains `aliases: {root.com: {members: [backup.com, mirror.com], rewrite_rdata_labels: true}}`
- **THEN** the loader SHALL return a config where both `backup.com.` and `mirror.com.` map to root `root.com.` with `rewrite_rdata_labels: true`

#### Scenario: Aliases object form omits rewrite_rdata_labels

- **WHEN** the config file contains `aliases: {root.com: {members: [backup.com]}}`
- **THEN** the loader SHALL return a config where `backup.com.` maps to root `root.com.` with `rewrite_rdata_labels: false`

#### Scenario: List and object forms coexist across different roots

- **WHEN** the config file contains `aliases: {root-a.net: [alias-a.net], root-b.net: {members: [alias-b.net], rewrite_rdata_labels: true}}`
- **THEN** the loader SHALL return a config where `alias-a.net.` maps to `root-a.net.` with the flag false, and `alias-b.net.` maps to `root-b.net.` with the flag true

#### Scenario: Ephemeral-API-only config

- **WHEN** the config file contains only `ephemeral_api: {listen: "127.0.0.1:8053", allow: ["10.0.0.5"]}`
- **THEN** the loader SHALL return a config with an empty alias map and `ephemeral_api` populated

#### Scenario: Empty aliases map is accepted

- **WHEN** the config file contains `aliases: {}`
- **THEN** the loader SHALL return a config with an empty alias map and no error

#### Scenario: Legacy one-to-one aliases format is rejected

- **WHEN** the config file contains `aliases: {backup.com: root.com}` (a bare string value under a root key)
- **THEN** the loader SHALL return a YAML decoding error identifying the type mismatch; the server SHALL NOT start with the partial configuration

#### Scenario: Aliases object form with unknown field is rejected

- **WHEN** the config file contains `aliases: {root.com: {members: [backup.com], unknown_flag: true}}`
- **THEN** the loader SHALL return an error that names the unknown field `unknown_flag`

#### Scenario: Aliases object form missing members is rejected

- **WHEN** the config file contains `aliases: {root.com: {rewrite_rdata_labels: true}}`
- **THEN** the loader SHALL return an error indicating that `members` is required when the alias value is an object

#### Scenario: Unknown top-level key fails

- **WHEN** the config file contains a top-level key that is not `aliases` or `ephemeral_api`
- **THEN** the loader SHALL return an error that names the unknown key

#### Scenario: Config file does not exist

- **WHEN** the path passed to `--config` does not exist
- **THEN** the loader SHALL return an error identifying the missing file path
