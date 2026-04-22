## ADDED Requirements

### Requirement: Store ephemeral TXT records in memory with expiration

The ephemeral record store SHALL hold TXT records in memory, keyed by lowercased FQDN (with trailing dot). Each FQDN MAY have multiple independent entries; each entry SHALL store the TXT value and an absolute expiration timestamp. Entries for the same FQDN are distinguished by their value. The store SHALL be safe for concurrent access from multiple goroutines.

#### Scenario: Store and retrieve a TXT record

- **WHEN** a TXT record for `_acme-challenge.example.com.` with value `abc123` and TTL 120 is inserted
- **THEN** a subsequent lookup for `_acme-challenge.example.com.` SHALL return a non-empty list whose single element has value `abc123` and a TTL equal to the remaining seconds until expiration (minimum 1)

#### Scenario: Lookup returns empty for unknown FQDN

- **WHEN** a lookup is performed for an FQDN that has no ephemeral record
- **THEN** the store SHALL return nil or an empty list

#### Scenario: Put appends a new value under an existing FQDN

- **WHEN** a TXT record for `_acme-challenge.example.com.` with value `abc123` already exists and a new Put is issued for the same FQDN with a different value `def456`
- **THEN** the store SHALL hold both entries and a subsequent lookup for `_acme-challenge.example.com.` SHALL return both values, each with its own remaining TTL

#### Scenario: Put refreshes TTL when value already exists

- **WHEN** a TXT record for `_acme-challenge.example.com.` with value `abc123` already exists and a new Put is issued for the same FQDN with the identical value `abc123` and a new TTL
- **THEN** the store SHALL NOT create a second entry and SHALL update the existing entry's expiration using the new TTL

### Requirement: Expired records are not returned on lookup

The store SHALL NOT return records whose expiration timestamp is in the past. Expired records SHALL be treated as non-existent during lookup (lazy eviction).

#### Scenario: Lookup after TTL expiration returns empty

- **WHEN** a TXT record was inserted with TTL 60 and 61 seconds have elapsed
- **THEN** a lookup for that FQDN SHALL return nil or an empty list

#### Scenario: TTL in response is dynamically computed

- **WHEN** a TXT record was inserted with TTL 120 and 30 seconds have elapsed
- **THEN** a lookup SHALL return an entry with TTL 90

#### Scenario: Per-entry expiration under the same FQDN

- **WHEN** two entries exist for the same FQDN — one with TTL 30 and one with TTL 300 — and 31 seconds have elapsed
- **THEN** a lookup SHALL return only the second entry (with TTL ~269); the first entry SHALL NOT be returned

### Requirement: Periodic garbage collection removes expired records

The store SHALL run a background goroutine that periodically scans all entries and removes those whose expiration timestamp is in the past. The default scan interval SHALL be 30 seconds. The GC goroutine SHALL stop when the provided context is cancelled.

#### Scenario: Expired record is removed by GC

- **WHEN** a TXT record has expired and the GC cycle runs
- **THEN** the expired entry SHALL be removed from the store's internal map

#### Scenario: GC stops on context cancellation

- **WHEN** the context passed to the store is cancelled
- **THEN** the GC goroutine SHALL exit without leaking

### Requirement: Clear removes all ephemeral records

The store SHALL provide a Clear method that removes all entries unconditionally. This is used during SIGHUP reload.

#### Scenario: Clear removes all records

- **WHEN** the store contains 5 ephemeral records and Clear is called
- **THEN** all subsequent lookups SHALL return nil or empty results

### Requirement: Delete removes all ephemeral entries for an FQDN

The store SHALL provide a Delete method that removes every ephemeral entry associated with the given FQDN in a single operation. Delete SHALL NOT accept a specific value argument — the operation is always whole-FQDN. Deleting a non-existent FQDN SHALL be a no-op (no error). Delete SHALL only touch the ephemeral store; records served from zone files SHALL NOT be affected.

#### Scenario: Delete removes all entries for the FQDN

- **WHEN** three ephemeral entries exist for `_acme-challenge.example.com.` with different values, and Delete is called for that FQDN
- **THEN** a subsequent lookup for that FQDN SHALL return nil or an empty list

#### Scenario: Delete of non-existent FQDN is a no-op

- **WHEN** Delete is called for an FQDN that has no ephemeral record
- **THEN** no error SHALL be returned
