## MODIFIED Requirements

### Requirement: Index parsed records by owner and qtype

The zone-parser SHALL store parsed resource records in an owner-indexed map whose values are further indexed by RR type code (qtype), such that a `(owner, qtype)` pair resolves to its matching records via two map lookups and without per-query filtering. The outer key SHALL be the canonical owner name (lowercased, trailing-dot terminated, as produced by existing parsing rules). The inner key SHALL be the RR type code as defined by `miekg/dns.Type*` constants.

The zone-parser SHALL NOT store byte-identical duplicate records within a single `(owner, qtype)` RRset. When a record is inserted whose owner, type, and RDATA match a record already stored at that `(owner, qtype)` (TTL excluded from the comparison, as determined by `miekg/dns.IsDuplicate`), the parser SHALL keep the first-stored record (including its TTL) and discard the later duplicate. This invariant SHALL hold regardless of whether the duplicate originates from inline records, a `$INCLUDE`-expanded fragment, or any combination thereof, and SHALL be enforced by the record-insertion primitive so that every load path is covered. Records that differ in RDATA (distinct records) SHALL all be retained.

#### Scenario: Records at the same owner with different qtypes are stored separately

- **WHEN** a zone file contains `www.example.com. IN A 192.0.2.1` and `www.example.com. IN AAAA 2001:db8::1` and `www.example.com. IN TXT "v=spf1 -all"`
- **THEN** `Zone.Records["www.example.com."][TypeA]` contains exactly one A record, `Zone.Records["www.example.com."][TypeAAAA]` contains exactly one AAAA record, and `Zone.Records["www.example.com."][TypeTXT]` contains exactly one TXT record
- **AND** no single slice at `Zone.Records["www.example.com."]` mixes RRs of different qtypes

#### Scenario: Multiple distinct records of the same qtype at one owner are preserved in insertion order

- **WHEN** a zone file contains `a.example.com. IN A 192.0.2.1` followed by `a.example.com. IN A 192.0.2.2` followed by `a.example.com. IN A 192.0.2.3`
- **THEN** `Zone.Records["a.example.com."][TypeA]` is a slice of length 3 whose elements appear in the order 192.0.2.1, 192.0.2.2, 192.0.2.3

#### Scenario: Byte-identical duplicate of the same qtype at one owner is stored once

- **WHEN** a zone file contains `a.example.com. IN A 192.0.2.1` followed by a second `a.example.com. IN A 192.0.2.1`
- **THEN** `Zone.Records["a.example.com."][TypeA]` is a slice of length 1 containing 192.0.2.1

#### Scenario: Duplicate CNAME declared inline and via $INCLUDE is collapsed

- **WHEN** a zone file declares `host.example.com. IN CNAME target.example.com.` inline and also `$INCLUDE`s a fragment that declares the identical `host.example.com. IN CNAME target.example.com.`
- **THEN** `Zone.Records["host.example.com."][TypeCNAME]` is a slice of length 1
- **AND** a query that follows the chain through `host.example.com.` emits the CNAME exactly once

#### Scenario: TTL is excluded from duplicate identity

- **WHEN** a zone file contains `a.example.com. 300 IN A 192.0.2.1` followed by `a.example.com. 60 IN A 192.0.2.1`
- **THEN** `Zone.Records["a.example.com."][TypeA]` is a slice of length 1
- **AND** the retained record carries the first occurrence's TTL of 300

#### Scenario: Owner with no records for a given qtype yields no inner entry

- **WHEN** a zone file contains only `www.example.com. IN A 192.0.2.1`
- **THEN** `Zone.Records["www.example.com."][TypeA]` is a non-empty slice
- **AND** `Zone.Records["www.example.com."][TypeAAAA]` is the zero value (nil slice) and the inner map has no entry for TypeAAAA

#### Scenario: Non-existent owner yields no outer entry

- **WHEN** a zone file contains records for `www.example.com.` only
- **THEN** `Zone.Records["nonexistent.example.com."]` is the zero value (nil inner map)

## ADDED Requirements

### Requirement: Log resource-record deduplication at load

When the zone-parser discards a duplicate record (per the deduplication invariant above), it SHALL record the event for operability without flooding logs at production scale, where a single zone-view can contain thousands of duplicates. Per-duplicate detail SHALL be emitted at DEBUG level only, and the parser SHALL guard the DEBUG call so that a disabled DEBUG level incurs no per-record formatting cost. For each parsed zone that discarded at least one duplicate, the parser SHALL emit exactly one aggregated WARN summary that includes the zone origin, the total number of duplicates discarded, and a by-RR-type histogram of the discarded records. A zone that discarded no duplicates SHALL NOT emit a summary.

#### Scenario: Each discarded duplicate is logged at DEBUG

- **WHEN** a zone load discards a duplicate `host.example.com. IN CNAME target.example.com.`
- **AND** the logger has DEBUG level enabled
- **THEN** a DEBUG entry is emitted carrying the zone origin, the owner `host.example.com.`, and the type `CNAME`

#### Scenario: Per-record DEBUG is skipped when DEBUG is disabled

- **WHEN** a zone load discards one or more duplicates
- **AND** the logger has DEBUG level disabled
- **THEN** no per-duplicate DEBUG entry is emitted
- **AND** the aggregated summary is still emitted

#### Scenario: Per-zone WARN summary is emitted whenever any duplicate was discarded

- **WHEN** a zone load discards 3 duplicate A records and 1 duplicate CNAME record at various owners
- **THEN** exactly one WARN entry is emitted for that zone
- **AND** it reports the zone origin, a total of 4, and a histogram of `{A: 3, CNAME: 1}`

#### Scenario: Zone with zero duplicates emits no summary

- **WHEN** a zone load discards no duplicate records
- **THEN** no deduplication summary entry is emitted for that zone
