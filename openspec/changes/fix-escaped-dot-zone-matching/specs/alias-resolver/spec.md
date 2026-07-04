## ADDED Requirements

### Requirement: Zone attribution honors RFC 1035 label escaping

The alias-resolver's longest-suffix zone attribution SHALL treat an escaped dot (`\.`) within a DNS label as a within-label character, not a label boundary, when deciding whether a query name falls within a loaded zone. A query name whose label contains a literal dot (presented in RFC 1035 form as `\.`) SHALL be attributed to the zone that encloses it at true label boundaries, and SHALL NOT be attributed to a longer loaded zone that only appears to be a suffix under byte-level matching. Zone attribution for query names that contain no escape sequence SHALL be unchanged.

#### Scenario: Escaped-dot label is attributed to the enclosing zone, not a longer look-alike zone

- **WHEN** the loaded zones are `example.com.` and `a.example.com.` AND a client queries the name whose single leftmost label is the literal `x.a` (presented as `x\.a.example.com.`)
- **THEN** the alias-resolver reports matched zone `example.com.` (the true enclosing zone) AND NOT `a.example.com.`

#### Scenario: Escaped dot is not a zone boundary

- **WHEN** the loaded zone is `a.example.com.` AND the query name is `x\.a.example.com.` (a single leftmost label `x.a`)
- **THEN** the name is NOT classified as within `a.example.com.` (the `\.` does not create a label boundary)

#### Scenario: Names without escape sequences are attributed unchanged

- **WHEN** the loaded zones are `example.com.` and `a.example.com.` AND a client queries `x.a.example.com.` (three separate labels, no escape)
- **THEN** the alias-resolver reports matched zone `a.example.com.` exactly as before
