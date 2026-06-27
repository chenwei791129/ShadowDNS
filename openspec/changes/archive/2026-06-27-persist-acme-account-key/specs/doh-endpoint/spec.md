## ADDED Requirements

### Requirement: ACME account key is persisted and reused across restarts

When the `doh` section is present, the DoH server SHALL load its ACME account private key from the file path configured in `doh.acme.account_key_file` and SHALL reuse the same key across process restarts and registration retries, so that account registration is idempotent and does not mint a new ACME account each time. Because the ACME directory returns the existing account for a previously registered key (RFC 8555 §7.3), reusing a persisted key SHALL NOT consume the per-source-IP new-account rate limit on restart or retry.

The `doh.acme.account_key_file` configuration field SHALL be required whenever the `doh` section is present and SHALL be an absolute path. Configuration loading SHALL fail, naming the `account_key_file` field, when it is missing or not an absolute path, and no DoH server SHALL be started in that case.

When the configured key file does not exist, the server SHALL generate a new account key, create any missing parent directory, and persist the key to the configured path with file permissions `0600` before use. When the configured key file exists but cannot be parsed as a valid account key, the server SHALL fail loudly with an error and SHALL NOT silently generate a replacement key or register a new account.

#### Scenario: First run generates and persists the account key

- **WHEN** ShadowDNS starts with a `doh.acme.account_key_file` path that does not yet exist
- **THEN** the server SHALL generate a new ACME account key, persist it to that path with `0600` permissions, and use it to register the ACME account

#### Scenario: Restart reuses the persisted account key

- **WHEN** ShadowDNS restarts with a `doh.acme.account_key_file` path that holds a previously persisted account key
- **THEN** the server SHALL load the same key and the ACME directory SHALL return the existing account rather than registering a new one

#### Scenario: Persistent registration failure does not mint new accounts

- **WHEN** ACME account registration fails repeatedly and the renewal loop retries
- **THEN** each retry SHALL reuse the same persisted account key and SHALL NOT create a new ACME account

#### Scenario: Corrupt key file fails loudly

- **WHEN** ShadowDNS starts with a `doh.acme.account_key_file` path that exists but does not contain a parseable account key
- **THEN** the server SHALL return an error identifying the key file and SHALL NOT overwrite the file, generate a new key, or register a new account

#### Scenario: Missing or relative account_key_file is rejected at load

- **WHEN** ShadowDNS is started with a `doh.acme` section whose `account_key_file` is absent or is a relative path
- **THEN** configuration loading SHALL fail naming the `account_key_file` field, and no DoH server SHALL be started
