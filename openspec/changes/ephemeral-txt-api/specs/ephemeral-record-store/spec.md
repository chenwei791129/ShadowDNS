## ADDED Requirements

### Requirement: Store ephemeral TXT records in memory with expiration

The ephemeral record store SHALL hold TXT records in memory, keyed by lowercased FQDN (with trailing dot). Each entry SHALL store the TXT value, the absolute expiration timestamp, and the original TTL. The store SHALL be safe for concurrent access from multiple goroutines.

#### Scenario: Store and retrieve a TXT record

- **WHEN** a TXT record for `_acme-challenge.example.com.` with value `abc123` and TTL 120 is inserted
- **THEN** a subsequent lookup for `_acme-challenge.example.com.` SHALL return the TXT record with a TTL equal to the remaining seconds until expiration (minimum 1)

#### Scenario: Lookup returns empty for unknown FQDN

- **WHEN** a lookup is performed for an FQDN that has no ephemeral record
- **THEN** the store SHALL return nil or an empty result

#### Scenario: Put overwrites existing record for the same FQDN

- **WHEN** a TXT record for `_acme-challenge.example.com.` already exists and a new Put is issued for the same FQDN with a different value and TTL
- **THEN** the store SHALL replace the previous entry with the new value and TTL

### Requirement: Expired records are not returned on lookup

The store SHALL NOT return records whose expiration timestamp is in the past. Expired records SHALL be treated as non-existent during lookup (lazy eviction).

#### Scenario: Lookup after TTL expiration returns empty

- **WHEN** a TXT record was inserted with TTL 60 and 61 seconds have elapsed
- **THEN** a lookup for that FQDN SHALL return nil or an empty result

#### Scenario: TTL in response is dynamically computed

- **WHEN** a TXT record was inserted with TTL 120 and 30 seconds have elapsed
- **THEN** a lookup SHALL return the record with TTL 90

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

### Requirement: Delete removes a specific ephemeral record

The store SHALL provide a Delete method that removes the entry for a given FQDN. Deleting a non-existent FQDN SHALL be a no-op (no error).

#### Scenario: Delete removes a specific record

- **WHEN** a TXT record exists for `_acme-challenge.example.com.` and Delete is called for that FQDN
- **THEN** a subsequent lookup for that FQDN SHALL return nil or an empty result

#### Scenario: Delete of non-existent FQDN is a no-op

- **WHEN** Delete is called for an FQDN that has no ephemeral record
- **THEN** no error SHALL be returned
