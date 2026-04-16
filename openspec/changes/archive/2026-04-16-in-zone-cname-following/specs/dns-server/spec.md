## MODIFIED Requirements

### Requirement: Synthesize CNAME response when qtype does not match but CNAME exists at the queried name

When the dns-server looks up records for a queried name and the requested qtype is not CNAME, but a CNAME record exists at that name, the dns-server SHALL return the CNAME record in the answer section with `RCODE=NOERROR` and `AA=1`, per RFC 1034 §3.6.2.

When the CNAME target is within the same zone (in-bailiwick — the target FQDN equals the zone origin or has the zone origin as a suffix), the dns-server SHALL restart the query at the CNAME target per RFC 1034 §3.6.2: look up records for `(target, original qtype)` in the same zone and append the results to the answer section after the CNAME record. This in-zone CNAME following is NOT recursion; it uses only local zone data.

When the CNAME target results in another CNAME (CNAME chain), the dns-server SHALL continue following the chain as long as each intermediate target remains in-bailiwick. The dns-server SHALL stop following when:
1. A non-CNAME record is found at the current target (success — append to answer).
2. The current target is out-of-bailiwick (stop — return collected records so far).
3. No record of any type exists at the current target (stop — return collected CNAME chain).
4. The chain depth reaches 8 (stop — return collected CNAME chain to prevent infinite loops from circular zone configurations).

When the CNAME target is out-of-bailiwick (not within the same zone), the dns-server SHALL return only the CNAME record without following, as resolving external names requires recursion which the server does not perform.

This behavior SHALL apply to both root zone queries and backup (alias) zone queries. For backup zone queries, the CNAME record's owner name in the response SHALL use the backup-namespace qname (not the rewritten root-namespace name). In-zone CNAME following for backup zone queries SHALL operate on the root zone's data (since that is where the records originate), and all returned records SHALL have their owner names and in-bailiwick RDATA rewritten to the backup namespace.

This behavior SHALL also apply when the initial CNAME is found via wildcard matching. After synthesizing the wildcard CNAME with the original qname as owner, the dns-server SHALL follow the CNAME target using the same in-zone rules described above.

When qtype is explicitly CNAME, the existing exact-match lookup behavior SHALL continue to apply unchanged — no CNAME following is performed.

When both a CNAME record and other record types coexist at the same name (a configuration error per RFC 1034 §3.6.2, but possible in zone files), the CNAME SHALL take precedence for non-CNAME queries: the server SHALL return the CNAME rather than NODATA.

#### Scenario: A query at a CNAME name with in-zone target returns CNAME plus target records

- **WHEN** a client queries `alias.root.com. A` AND the zone contains `alias.root.com. CNAME target.root.com.` AND `target.root.com. A 1.2.3.4`
- **THEN** the response has `RCODE=NOERROR`, `AA=1`, and the answer section contains both `alias.root.com. CNAME target.root.com.` and `target.root.com. A 1.2.3.4` in that order

#### Scenario: A query at a CNAME name with out-of-bailiwick target returns only the CNAME

- **WHEN** a client queries `alias.root.com. A` AND the zone contains `alias.root.com. CNAME target.other.com.` AND `other.com.` is not a loaded zone
- **THEN** the response has `RCODE=NOERROR`, `AA=1`, and the answer section contains only `alias.root.com. CNAME target.other.com.`

#### Scenario: CNAME chain within the same zone is fully followed

- **WHEN** a client queries `a.root.com. A` AND the zone contains `a.root.com. CNAME b.root.com.` AND `b.root.com. CNAME c.root.com.` AND `c.root.com. A 5.6.7.8`
- **THEN** the response answer section contains `a.root.com. CNAME b.root.com.`, `b.root.com. CNAME c.root.com.`, and `c.root.com. A 5.6.7.8` in that order

#### Scenario: CNAME chain stops at out-of-bailiwick target

- **WHEN** a client queries `a.root.com. A` AND the zone contains `a.root.com. CNAME b.root.com.` AND `b.root.com. CNAME external.other.com.`
- **THEN** the response answer section contains `a.root.com. CNAME b.root.com.` and `b.root.com. CNAME external.other.com.` (no A record, as the external target cannot be resolved locally)

#### Scenario: CNAME chain is truncated at depth 8

- **WHEN** a client queries `c1.root.com. A` AND the zone contains a circular CNAME chain `c1 → c2 → c3 → ... → c8 → c9` (9 CNAMEs)
- **THEN** the response answer section contains the first 8 CNAME records and stops (no A record)

#### Scenario: AAAA query at a CNAME name with in-zone target returns CNAME plus AAAA

- **WHEN** a client queries `alias.root.com. AAAA` AND the zone contains `alias.root.com. CNAME target.root.com.` AND `target.root.com. AAAA 2001:db8::1`
- **THEN** the response answer section contains `alias.root.com. CNAME target.root.com.` and `target.root.com. AAAA 2001:db8::1`

#### Scenario: In-zone CNAME target has no records of requested type returns CNAME chain only

- **WHEN** a client queries `alias.root.com. AAAA` AND the zone contains `alias.root.com. CNAME target.root.com.` AND `target.root.com.` has an A record but no AAAA record
- **THEN** the response has `RCODE=NOERROR`, and the answer section contains only `alias.root.com. CNAME target.root.com.`

#### Scenario: Explicit CNAME query does not follow the target

- **WHEN** a client queries `alias.root.com. CNAME` AND the zone contains `alias.root.com. CNAME target.root.com.` AND `target.root.com. A 1.2.3.4`
- **THEN** the response answer section contains only `alias.root.com. CNAME target.root.com.` (no A record appended)

#### Scenario: Backup zone CNAME with in-zone target returns rewritten CNAME plus target records

- **WHEN** `backup.com.` is a backup of `root.com.` AND the root zone contains `sub.root.com. CNAME target.root.com.` AND `target.root.com. A 1.2.3.4` AND a client queries `sub.backup.com. A`
- **THEN** the response answer section contains `sub.backup.com. CNAME target.backup.com.` and `target.backup.com. A 1.2.3.4` (owner names and in-bailiwick CNAME RDATA rewritten to backup namespace)

#### Scenario: Backup zone CNAME with out-of-bailiwick target returns only CNAME

- **WHEN** `backup.com.` is a backup of `root.com.` AND the root zone contains `sub.root.com. CNAME target.other.com.` AND a client queries `sub.backup.com. A`
- **THEN** the response answer section contains only `sub.backup.com. CNAME target.other.com.` (owner is rewritten to backup namespace, out-of-bailiwick target preserved)

#### Scenario: Wildcard CNAME with in-zone target is followed

- **WHEN** the zone contains `*.root.com. CNAME service.root.com.` AND `service.root.com. A 10.0.0.1` AND a client queries `foo.root.com. A`
- **THEN** the response answer section contains `foo.root.com. CNAME service.root.com.` and `service.root.com. A 10.0.0.1`

#### Scenario: Name with no records and no CNAME returns NXDOMAIN or NODATA as before

- **WHEN** a client queries `missing.root.com. A` AND no records of any type exist at `missing.root.com.`
- **THEN** the response has `RCODE=NXDOMAIN` with the zone SOA in the authority section (unchanged behavior)

#### Scenario: Name with A record but no AAAA and no CNAME returns NODATA as before

- **WHEN** a client queries `www.root.com. AAAA` AND `www.root.com.` has an A record but no AAAA record and no CNAME record
- **THEN** the response has `RCODE=NOERROR`, empty answer section, and the zone SOA in the authority section (unchanged behavior)
