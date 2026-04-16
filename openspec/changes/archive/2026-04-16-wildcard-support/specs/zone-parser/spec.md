## ADDED Requirements

### Requirement: Parse and store wildcard owner names

The zone-parser SHALL correctly parse zone file entries with the `*` wildcard label (e.g., `*.example.com. A 1.2.3.4`) and store them in the Zone.Records map under the key `*.example.com.` (lowercased, FQDN with trailing dot). The `*` label SHALL be preserved as a literal `*` character in the map key, not expanded or interpreted during parsing.

#### Scenario: Wildcard A record is parsed and stored

- **WHEN** a zone file contains `* 300 IN A 1.2.3.4` with `$ORIGIN example.com.`
- **THEN** the parsed Zone.Records map contains an entry at key `*.example.com.` with one A record having RDATA `1.2.3.4`

#### Scenario: Wildcard CNAME record is parsed and stored

- **WHEN** a zone file contains `*.sub 300 IN CNAME target.other.com.` with `$ORIGIN example.com.`
- **THEN** the parsed Zone.Records map contains an entry at key `*.sub.example.com.` with one CNAME record targeting `target.other.com.`

#### Scenario: Multiple wildcard records at the same owner are stored together

- **WHEN** a zone file contains `* 300 IN A 1.2.3.4` and `* 300 IN A 5.6.7.8` with `$ORIGIN example.com.`
- **THEN** the parsed Zone.Records map contains an entry at key `*.example.com.` with two A records
