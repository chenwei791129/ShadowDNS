## MODIFIED Requirements

### Requirement: Store ephemeral TXT records in memory with expiration

The ephemeral record store SHALL hold TXT records in memory, keyed by lowercased FQDN (with trailing dot). An FQDN SHALL be allowed to hold multiple independent entries; each entry SHALL store the TXT value and an absolute expiration timestamp. Entries for the same FQDN SHALL be distinguished by their value. The store SHALL be safe for concurrent access from multiple goroutines.

Lookup SHALL return, for each unexpired entry, only the TXT value. The store SHALL NOT return a per-entry remaining-TTL field: TTL accounting on the response side belongs to the DNS handler, not the store.

#### Scenario: Store and retrieve a TXT record

- **WHEN** a TXT record for `_acme-challenge.example.com.` with value `abc123` and TTL 120 is inserted
- **THEN** a subsequent lookup for `_acme-challenge.example.com.` SHALL return a non-empty list whose single element has value `abc123`

#### Scenario: Lookup returns empty for unknown FQDN

- **WHEN** a lookup is performed for an FQDN that has no ephemeral record
- **THEN** the store SHALL return nil or an empty list

#### Scenario: Put appends a new value under an existing FQDN

- **WHEN** a TXT record for `_acme-challenge.example.com.` with value `abc123` already exists and a new Put is issued for the same FQDN with a different value `def456`
- **THEN** the store SHALL hold both entries and a subsequent lookup for `_acme-challenge.example.com.` SHALL return both values

#### Scenario: Put refreshes TTL when value already exists

- **WHEN** a TXT record for `_acme-challenge.example.com.` with value `abc123` already exists and a new Put is issued for the same FQDN with the identical value `abc123` and a new TTL
- **THEN** the store SHALL NOT create a second entry and SHALL update the existing entry's expiration using the new TTL

### Requirement: Expired records are not returned on lookup

The store SHALL NOT return records whose expiration timestamp is in the past. Expired records SHALL be treated as non-existent during lookup (lazy eviction). Expired entries SHALL NOT appear in Lookup output regardless of whether other entries under the same FQDN are still live.

#### Scenario: Lookup after TTL expiration returns empty

- **WHEN** a TXT record was inserted with TTL 60 and 61 seconds have elapsed
- **THEN** a lookup for that FQDN SHALL return nil or an empty list

#### Scenario: Per-entry expiration under the same FQDN

- **WHEN** two entries exist for the same FQDN — one with TTL 30 and one with TTL 300 — and 31 seconds have elapsed
- **THEN** a lookup SHALL return only the second entry; the first entry SHALL NOT be returned

