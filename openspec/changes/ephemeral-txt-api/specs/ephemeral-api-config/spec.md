## ADDED Requirements

### Requirement: Load API configuration from a YAML file

The config loader SHALL parse a YAML file specified by the `-api-conf` CLI flag. The file SHALL contain the following fields:

- `listen` (string, required): The `host:port` address for the API server
- `allow` (list of strings, required): IP addresses or CIDR ranges for the ACL
- `token` (string, optional): Pre-shared bearer token for authentication

#### Scenario: Valid config with all fields

- **WHEN** the API config file contains `listen: "127.0.0.1:8053"`, `allow: ["10.0.0.5"]`, and `token: "secret"`
- **THEN** the loader SHALL return a config with listen address `127.0.0.1:8053`, one ACL entry for `10.0.0.5`, and token `secret`

#### Scenario: Valid config without token

- **WHEN** the API config file contains `listen` and `allow` but no `token` field
- **THEN** the loader SHALL return a config with an empty token (token validation disabled)

#### Scenario: Missing listen field fails

- **WHEN** the API config file omits the `listen` field
- **THEN** the loader SHALL return an error indicating the missing field

#### Scenario: Empty allow list fails

- **WHEN** the API config file has an empty `allow` list or omits the `allow` field
- **THEN** the loader SHALL return an error indicating that at least one ACL entry is required

### Requirement: Validate ACL entries at load time

The config loader SHALL validate each entry in the `allow` list as a valid IPv4/IPv6 address or CIDR notation. Invalid entries SHALL cause a load error with the offending entry identified.

#### Scenario: Invalid CIDR in allow list

- **WHEN** the API config file contains `allow: ["not-an-ip"]`
- **THEN** the loader SHALL return an error identifying `not-an-ip` as an invalid ACL entry

#### Scenario: Mixed valid IPv4 and CIDR entries

- **WHEN** the API config file contains `allow: ["10.0.0.5", "192.168.1.0/24"]`
- **THEN** the loader SHALL accept both entries without error
