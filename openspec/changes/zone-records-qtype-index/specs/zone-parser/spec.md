## ADDED Requirements

### Requirement: Index parsed records by owner and qtype

The zone-parser SHALL store parsed resource records in an owner-indexed map whose values are further indexed by RR type code (qtype), such that a `(owner, qtype)` pair resolves to its matching records via two map lookups and without per-query filtering. The outer key SHALL be the canonical owner name (lowercased, trailing-dot terminated, as produced by existing parsing rules). The inner key SHALL be the RR type code as defined by `miekg/dns.Type*` constants.

#### Scenario: Records at the same owner with different qtypes are stored separately

- **WHEN** a zone file contains `www.example.com. IN A 192.0.2.1` and `www.example.com. IN AAAA 2001:db8::1` and `www.example.com. IN TXT "v=spf1 -all"`
- **THEN** `Zone.Records["www.example.com."][TypeA]` contains exactly one A record, `Zone.Records["www.example.com."][TypeAAAA]` contains exactly one AAAA record, and `Zone.Records["www.example.com."][TypeTXT]` contains exactly one TXT record
- **AND** no single slice at `Zone.Records["www.example.com."]` mixes RRs of different qtypes

#### Scenario: Multiple records of the same qtype at one owner are preserved in insertion order

- **WHEN** a zone file contains `a.example.com. IN A 192.0.2.1` followed by `a.example.com. IN A 192.0.2.2` followed by `a.example.com. IN A 192.0.2.3`
- **THEN** `Zone.Records["a.example.com."][TypeA]` is a slice of length 3 whose elements appear in the order 192.0.2.1, 192.0.2.2, 192.0.2.3

#### Scenario: Owner with no records for a given qtype yields no inner entry

- **WHEN** a zone file contains only `www.example.com. IN A 192.0.2.1`
- **THEN** `Zone.Records["www.example.com."][TypeA]` is a non-empty slice
- **AND** `Zone.Records["www.example.com."][TypeAAAA]` is the zero value (nil slice) and the inner map has no entry for TypeAAAA

#### Scenario: Non-existent owner yields no outer entry

- **WHEN** a zone file contains records for `www.example.com.` only
- **THEN** `Zone.Records["nonexistent.example.com."]` is the zero value (nil inner map)

### Requirement: Lookup returns stored records as a shared-backing reference

`Zone.Lookup(owner, qtype)` and `Zone.LookupWildcard(qname, qtype)` SHALL return the slice stored at `Zone.Records[owner][qtype]` as a direct reference whose underlying array is shared with the stored record list. The caller SHALL NOT mutate the returned slice (no element assignment, no `append` that shares capacity, no sort). When the owner or qtype has no records, the return value SHALL be a nil or zero-length slice; callers SHALL check `len()` rather than relying on `!= nil`.

#### Scenario: Lookup result shares backing array with stored records

- **WHEN** `Zone.Records["a.example.com."][TypeA]` contains two A records
- **THEN** the slice returned by `Lookup("a.example.com.", TypeA)` has the same underlying array address as the stored slice
- **AND** iterating the return value yields the same records in the same order as the stored slice

#### Scenario: Lookup for missing qtype returns empty slice without error

- **WHEN** owner `a.example.com.` exists with records only at TypeA
- **THEN** `Lookup("a.example.com.", TypeAAAA)` returns a slice with `len() == 0`
- **AND** no panic or error occurs
