## ADDED Requirements

### Requirement: Wildcard label stepping honors RFC 1035 label escaping

When stripping the leftmost label during wildcard matching (per the RFC 4592 algorithm), the dns-server SHALL split the name at true label boundaries honoring RFC 1035 escaping, so that an escaped dot (`\.`) within a label is not treated as a label boundary. A query name whose leftmost label contains a literal dot SHALL step to the parent name obtained by removing that whole label, and the wildcard owner probed SHALL be `*.<true-parent>`. Wildcard label stepping for query names that contain no escape sequence SHALL be unchanged.

#### Scenario: Escaped-dot query steps to the true parent when probing wildcards

- **WHEN** the zone `example.com.` contains `*.example.com. A 192.0.2.4` AND no records exist at the name whose leftmost label is the literal `x.a` AND a client queries that name (presented as `x\.a.example.com.`)
- **THEN** wildcard stepping strips the whole `x.a` label to parent `example.com.` and matches `*.example.com.`, and the answer section contains `x\.a.example.com. A 192.0.2.4`

#### Scenario: Escaped dot does not cause a split into a look-alike wildcard

- **WHEN** the zone `example.com.` contains `*.a.example.com. A 192.0.2.5` AND `*.example.com. A 192.0.2.4` AND a client queries the name whose single leftmost label is the literal `x.a` (presented `x\.a.example.com.`)
- **THEN** the match is `*.example.com.` (answer `x\.a.example.com. A 192.0.2.4`) AND NOT `*.a.example.com.`, because the `\.` is not a label boundary

#### Scenario: Names without escape sequences match wildcards unchanged

- **WHEN** the zone `example.com.` contains `*.a.example.com. A 192.0.2.5` AND a client queries `x.a.example.com. A` (three separate labels, no escape)
- **THEN** the match is `*.a.example.com.` and the answer section contains `x.a.example.com. A 192.0.2.5`, exactly as before
