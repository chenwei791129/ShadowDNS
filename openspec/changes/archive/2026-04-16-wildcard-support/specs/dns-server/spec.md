## ADDED Requirements

### Requirement: Match wildcard records per RFC 4592 when exact lookup fails

When a query name falls within a loaded zone and exact lookup produces no records, the dns-server SHALL attempt wildcard matching per RFC 4592. The wildcard matching algorithm SHALL:

1. Starting from the query name, strip the leftmost label to produce a parent name.
2. Check whether a wildcard owner `*.<parent>` exists in the zone. If it does, use those records as the match.
3. If no wildcard owner exists at this level, check whether `<parent>` itself exists in the zone's Records (as an empty non-terminal). If it does, stop — the wildcard MUST NOT match (RFC 4592 §2.2.1 ENT blocking rule).
4. If `<parent>` does not exist, strip the next leftmost label and repeat from step 2.
5. Stop when parent equals the zone origin. If no wildcard is found, fall through to NXDOMAIN or NODATA as before.

When a wildcard match is found, the dns-server SHALL synthesize the response with the original query name as the owner name in the answer section (not the `*` label), per RFC 4592 §2.2.

This behavior SHALL apply to both root zone queries and backup (alias) zone queries. For backup zone queries, the wildcard lookup SHALL operate on the root zone's records (after qname rewrite to root namespace), and the synthesized response SHALL use the backup-namespace qname as the owner name.

#### Scenario: Single-level wildcard matches subdomain query

- **WHEN** the zone contains `*.example.com. A 1.2.3.4` AND a client queries `foo.example.com. A`
- **THEN** the response has `RCODE=NOERROR`, `AA=1`, and the answer section contains `foo.example.com. A 1.2.3.4`

#### Scenario: Multi-level subdomain matches wildcard at closest encloser

- **WHEN** the zone contains `*.example.com. A 1.2.3.4` AND no records exist at `bar.example.com.` AND a client queries `foo.bar.example.com. A`
- **THEN** the response has `RCODE=NOERROR`, `AA=1`, and the answer section contains `foo.bar.example.com. A 1.2.3.4`

#### Scenario: Empty non-terminal blocks wildcard matching

- **WHEN** the zone contains `*.example.com. A 1.2.3.4` AND `sub.example.com. TXT "exists"` AND a client queries `other.sub.example.com. A`
- **THEN** the response has `RCODE=NXDOMAIN` because `sub.example.com.` is an ENT that blocks the wildcard

#### Scenario: More-specific wildcard takes precedence

- **WHEN** the zone contains `*.example.com. A 1.1.1.1` AND `*.sub.example.com. A 2.2.2.2` AND a client queries `foo.sub.example.com. A`
- **THEN** the response answer section contains `foo.sub.example.com. A 2.2.2.2` (the more-specific wildcard wins)

#### Scenario: Exact record takes precedence over wildcard

- **WHEN** the zone contains `*.example.com. A 1.1.1.1` AND `www.example.com. A 3.3.3.3` AND a client queries `www.example.com. A`
- **THEN** the response answer section contains `www.example.com. A 3.3.3.3` (exact match, wildcard not consulted)

#### Scenario: Wildcard CNAME is returned for non-CNAME query type

- **WHEN** the zone contains `*.example.com. CNAME target.other.com.` AND a client queries `foo.example.com. A` AND CNAME synthesis is active
- **THEN** the response answer section contains `foo.example.com. CNAME target.other.com.`

#### Scenario: Backup zone wildcard uses root zone wildcard with owner rewrite

- **WHEN** `backup.com.` is a backup of `root.com.` AND the root zone contains `*.root.com. A 1.2.3.4` AND a client queries `foo.backup.com. A`
- **THEN** the response answer section contains `foo.backup.com. A 1.2.3.4`

#### Scenario: No wildcard found falls through to NXDOMAIN

- **WHEN** the zone contains no wildcard records AND a client queries `missing.example.com. A` AND no records exist at that name
- **THEN** the response has `RCODE=NXDOMAIN` with the zone SOA in the authority section (unchanged behavior)

#### Scenario: Wildcard match with no records of requested type returns NODATA

- **WHEN** the zone contains `*.example.com. A 1.2.3.4` AND a client queries `foo.example.com. AAAA` AND no CNAME exists at the wildcard
- **THEN** the response has `RCODE=NOERROR`, empty answer section, and the zone SOA in the authority section
