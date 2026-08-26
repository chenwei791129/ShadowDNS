## ADDED Requirements

### Requirement: Expand environment expressions before strict unified-config decoding

The unified ShadowDNS configuration loader SHALL parse `shadowdns.yaml` into a YAML node tree, expand supported environment expressions only in value-side string scalar nodes, and then strictly decode the resulting tree into the existing configuration data shape. It SHALL preserve unknown-field rejection, alias-entry shape checks, and all section-specific semantic validation.

#### Scenario: Expanded valid unified configuration loads

- **GIVEN** `API_LISTEN` is set to `127.0.0.1:8053`, `ALLOW_PREFIX` is set to `192.0.2.0/24`, and `API_TOKEN` is set to a non-empty value
- **WHEN** the corresponding `ephemeral_api` string fields reference those variables
- **THEN** the loader SHALL return a populated Ephemeral API configuration with the expanded and validated values

#### Scenario: Expansion cannot bypass strict decoding

- **GIVEN** a valid environment expression appears in a recognized field
- **WHEN** the same YAML document contains an unknown top-level key or unknown field inside a recognized section
- **THEN** the loader SHALL reject the configuration and identify the unknown field

#### Scenario: Environment content cannot inject configuration fields

- **GIVEN** an environment value contains YAML text resembling a new top-level section
- **WHEN** the value is expanded in a recognized string field
- **THEN** the loader SHALL treat the entire environment value as scalar data and SHALL NOT decode an injected section
