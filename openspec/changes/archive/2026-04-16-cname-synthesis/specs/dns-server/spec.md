## ADDED Requirements

### Requirement: Synthesize CNAME response when qtype does not match but CNAME exists at the queried name

When the dns-server looks up records for a queried name and the requested qtype is not CNAME, but a CNAME record exists at that name, the dns-server SHALL return the CNAME record in the answer section with `RCODE=NOERROR` and `AA=1`, per RFC 1034 §3.6.2. The dns-server SHALL NOT follow (chase) the CNAME target, as it operates in authoritative-only mode and does not perform recursion.

This behavior SHALL apply to both root zone queries and backup (alias) zone queries. For backup zone queries, the CNAME record's owner name in the response SHALL use the backup-namespace qname (not the rewritten root-namespace name).

When qtype is explicitly CNAME, the existing exact-match lookup behavior SHALL continue to apply unchanged.

When both a CNAME record and other record types coexist at the same name (a configuration error per RFC 1034 §3.6.2, but possible in zone files), the CNAME SHALL take precedence for non-CNAME queries: the server SHALL return the CNAME rather than NODATA.

#### Scenario: A query at a CNAME name returns the CNAME

- **WHEN** a client queries `alias.root.com. A` AND the zone contains `alias.root.com. CNAME target.other.com.` AND no A record exists at `alias.root.com.`
- **THEN** the response has `RCODE=NOERROR`, `AA=1`, and the answer section contains `alias.root.com. CNAME target.other.com.`

#### Scenario: AAAA query at a CNAME name returns the CNAME

- **WHEN** a client queries `alias.root.com. AAAA` AND the zone contains `alias.root.com. CNAME target.other.com.`
- **THEN** the response has `RCODE=NOERROR`, `AA=1`, and the answer section contains `alias.root.com. CNAME target.other.com.`

#### Scenario: MX query at a CNAME name returns the CNAME

- **WHEN** a client queries `alias.root.com. MX` AND the zone contains `alias.root.com. CNAME target.other.com.`
- **THEN** the response has `RCODE=NOERROR`, `AA=1`, and the answer section contains `alias.root.com. CNAME target.other.com.`

#### Scenario: Explicit CNAME query returns the CNAME directly

- **WHEN** a client queries `alias.root.com. CNAME` AND the zone contains `alias.root.com. CNAME target.other.com.`
- **THEN** the response has `RCODE=NOERROR`, `AA=1`, and the answer section contains `alias.root.com. CNAME target.other.com.`

#### Scenario: Backup zone CNAME synthesis uses backup-namespace owner name

- **WHEN** `backup.com.` is a backup of `root.com.` AND the root zone contains `sub.root.com. CNAME target.other.com.` AND a client queries `sub.backup.com. A`
- **THEN** the response answer section contains a CNAME record with owner `sub.backup.com.` and target `target.other.com.` (owner is rewritten to backup namespace)

#### Scenario: Name with no records and no CNAME returns NXDOMAIN or NODATA as before

- **WHEN** a client queries `missing.root.com. A` AND no records of any type exist at `missing.root.com.`
- **THEN** the response has `RCODE=NXDOMAIN` with the zone SOA in the authority section (unchanged behavior)

#### Scenario: Name with A record but no AAAA and no CNAME returns NODATA as before

- **WHEN** a client queries `www.root.com. AAAA` AND `www.root.com.` has an A record but no AAAA record and no CNAME record
- **THEN** the response has `RCODE=NOERROR`, empty answer section, and the zone SOA in the authority section (unchanged behavior)
