## ADDED Requirements

### Requirement: DeleteValue removes a single ephemeral entry by value

The store SHALL provide a `DeleteValue(fqdn, value string) bool` method that removes at most one entry under the given FQDN whose stored value is byte-exact-equal to the supplied value. The comparison SHALL be case-sensitive and SHALL NOT apply any normalization. The method SHALL return `true` when an entry was removed and `false` when no entry matched. When removing the last remaining entry under an FQDN, the method SHALL also remove the FQDN key from the internal map so no empty slice is retained. DeleteValue SHALL only touch the ephemeral store; records served from zone files SHALL NOT be affected. DeleteValue SHALL be safe for concurrent use.

#### Scenario: DeleteValue removes only the matching entry

- **WHEN** two entries exist for `_acme-challenge.example.com.` with values `token-A` and `token-B`, and DeleteValue is called for `_acme-challenge.example.com.` with value `token-A`
- **THEN** DeleteValue SHALL return `true`
- **THEN** a subsequent lookup SHALL return a single entry with value `token-B`

#### Scenario: DeleteValue returns false when no entry matches

- **WHEN** an entry with value `token-A` exists for `_acme-challenge.example.com.` and DeleteValue is called for that FQDN with value `token-X`
- **THEN** DeleteValue SHALL return `false`
- **THEN** a subsequent lookup SHALL still return the `token-A` entry unchanged

#### Scenario: DeleteValue removes the FQDN key when the last entry is deleted

- **WHEN** a single entry with value `token-A` exists for `_acme-challenge.example.com.` and DeleteValue is called for that FQDN with value `token-A`
- **THEN** DeleteValue SHALL return `true`
- **THEN** the FQDN key SHALL no longer be present in the store's internal map (no empty slice retained)

#### Scenario: DeleteValue on unknown FQDN returns false

- **WHEN** DeleteValue is called for an FQDN that has no ephemeral record
- **THEN** DeleteValue SHALL return `false` and SHALL NOT return an error

#### Scenario: DeleteValue canonicalizes the FQDN

- **WHEN** an entry with value `v` exists for `foo.example.com.` and DeleteValue is called with FQDN `FOO.EXAMPLE.COM` and value `v`
- **THEN** DeleteValue SHALL return `true` (the FQDN is canonicalized to lowercased trailing-dot form before matching)
