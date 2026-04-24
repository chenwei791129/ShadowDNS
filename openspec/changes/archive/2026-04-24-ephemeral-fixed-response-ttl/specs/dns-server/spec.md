## MODIFIED Requirements

### Requirement: Listen for DNS queries on UDP and TCP port 53

The dns-server SHALL bind both UDP and TCP listeners on the configured address (default `0.0.0.0:53`) and serve DNS queries on both transports. The TCP listener SHALL remain required even when zone transfer is disabled, because TCP is the RFC 7766 fallback for responses larger than the UDP payload limit.

When an ephemeral record store is attached to the server, the DNS handler SHALL consult the ephemeral store at a defined point in the lookup chain so that ephemeral TXT entries overlay zone data in two specific cases:

1. **Zone miss overlay**: when the exact `(qname, qtype)` lookup on zone data produces no results, the ephemeral store SHALL be consulted before falling back to CNAME synthesis (RFC 1034 §3.6.2) and before wildcard synthesis (RFC 4592). If the ephemeral store returns a live TXT RRSet, the server SHALL return that RRSet and SHALL NOT perform CNAME fallback or wildcard synthesis.
2. **Exact-qtype zone precedence**: when the zone contains a record whose owner name AND type exactly matches the query (e.g., zone has an explicit TXT at the qname), the zone record SHALL take precedence and the ephemeral store SHALL NOT be consulted for that response.

The ephemeral overlay SHALL only apply when `qtype == TXT`. For all other qtypes (A, AAAA, CNAME, MX, etc.), the ephemeral store SHALL NOT be consulted and the standard RFC 1034 §3.6.2 CNAME fallback plus RFC 4592 wildcard synthesis SHALL apply unchanged.

This overlay SHALL apply equally to root zone queries and backup (alias) zone queries. For backup zone queries, the ephemeral lookup SHALL use the backup-namespace qname (matching the name under which API callers PUT entries).

When the ephemeral store holds multiple unexpired TXT entries for the queried FQDN, the server SHALL return them as a single TXT RRSet — one TXT RR per entry, all sharing the same owner name. Each entry's TXT value SHALL be encoded as a single string inside its own RR rather than concatenated into one RR.

The TTL value in ephemeral record responses SHALL be fixed at **30 seconds** for every ephemeral TXT RR, independent of the record's remaining lifetime in the Store. The API-supplied TTL SHALL control only the record's lifespan in the Store (its expiration timestamp) and SHALL NOT leak into the DNS response TTL field. The Authoritative Answer (AA) flag SHALL be set on responses containing ephemeral records, consistent with zone-based responses.

#### Scenario: UDP query receives response

- **WHEN** a client sends a valid DNS query over UDP on port 53
- **THEN** the server responds over UDP within the same 5-tuple

#### Scenario: TCP query receives response

- **WHEN** a client sends a valid DNS query over TCP on port 53
- **THEN** the server accepts the connection, reads the 2-byte length prefix, and writes a length-prefixed response

#### Scenario: Response exceeding UDP limit sets TC flag

- **WHEN** a response over UDP would exceed 512 bytes (or the negotiated EDNS0 UDP size) and cannot be truncated to fit
- **THEN** the server sets the TC (truncated) flag in the UDP response header so the client falls back to TCP

#### Scenario: Ephemeral TXT record is returned when zone has no match

- **WHEN** a DNS TXT query arrives for `_acme-challenge.example.com.` and the zone file has no record for that name, but the ephemeral store contains a TXT record for that FQDN with 90 seconds remaining lifetime
- **THEN** the server SHALL respond with the ephemeral TXT record, TTL set to 30, and the AA flag set

#### Scenario: Zone file record takes precedence over ephemeral record

- **WHEN** a DNS TXT query arrives for `_acme-challenge.example.com.` and both the zone file AND the ephemeral store contain a TXT record for that name
- **THEN** the server SHALL respond with the zone file record only

#### Scenario: Ephemeral TXT overrides exact CNAME at the same qname for TXT queries

- **WHEN** the zone contains `_acme-challenge.foo.example.com. CNAME acme-dns.other.com.` AND the ephemeral store contains a TXT record `token-xyz` for `_acme-challenge.foo.example.com.` with 120 seconds remaining lifetime AND a DNS TXT query arrives for `_acme-challenge.foo.example.com.`
- **THEN** the server SHALL respond with the ephemeral TXT `token-xyz` (TTL 30, AA set), SHALL NOT emit the CNAME record in the answer section, and SHALL NOT follow the CNAME target

##### Example: ACME delegation with local ephemeral override

- **GIVEN** zone `example.com.` contains `_acme-challenge.foo.example.com. CNAME acme-dns.external.net.` AND ephemeral store holds `_acme-challenge.foo.example.com. → "token-xyz"` with 120 s remaining lifetime
- **WHEN** client runs `dig +short TXT _acme-challenge.foo.example.com.`
- **THEN** client receives `"token-xyz"` only (no CNAME, no followed records); the response RR TTL SHALL be 30

#### Scenario: CNAME query at the same qname still returns the zone CNAME when ephemeral TXT exists

- **WHEN** the zone contains `_acme-challenge.foo.example.com. CNAME acme-dns.other.com.` AND the ephemeral store contains a live TXT record for `_acme-challenge.foo.example.com.` AND a DNS CNAME query arrives for `_acme-challenge.foo.example.com.`
- **THEN** the server SHALL respond with the zone CNAME `acme-dns.other.com.` only, with the AA flag set

#### Scenario: TXT query falls back to CNAME when ephemeral store has no live entry

- **WHEN** the zone contains `_acme-challenge.foo.example.com. CNAME target.example.com.` AND `target.example.com. TXT "zone-txt"` AND the ephemeral store contains NO entry (or only expired entries) for `_acme-challenge.foo.example.com.` AND a DNS TXT query arrives for `_acme-challenge.foo.example.com.`
- **THEN** the server SHALL perform standard RFC 1034 §3.6.2 CNAME synthesis and return the CNAME followed by `target.example.com. TXT "zone-txt"`

#### Scenario: Non-TXT qtype at a CNAME name is unaffected by ephemeral store

- **WHEN** the zone contains `foo.example.com. CNAME target.example.com.` AND `target.example.com. A 1.2.3.4` AND the ephemeral store contains a TXT entry for `foo.example.com.` AND a DNS A query arrives for `foo.example.com.`
- **THEN** the server SHALL perform standard CNAME synthesis and return the CNAME followed by `target.example.com. A 1.2.3.4`; the ephemeral TXT entry SHALL NOT appear in the response

#### Scenario: Ephemeral TXT overrides exact CNAME on backup (alias) zone queries

- **WHEN** `backup.com.` is a backup of `example.com.` AND the root zone contains `_acme-challenge.foo.example.com. CNAME acme-dns.other.com.` AND the ephemeral store contains a TXT entry for `_acme-challenge.foo.backup.com.` (backup-namespace qname) AND a DNS TXT query arrives for `_acme-challenge.foo.backup.com.`
- **THEN** the server SHALL respond with the ephemeral TXT record only, with owner `_acme-challenge.foo.backup.com.`, AA flag set, and SHALL NOT emit the CNAME synthesized from the root zone

#### Scenario: Expired ephemeral record is not returned

- **WHEN** a DNS TXT query arrives for an FQDN whose ephemeral record has expired
- **THEN** the server SHALL NOT return the expired record and SHALL send a negative reply (NXDOMAIN or NODATA) as if the record did not exist

#### Scenario: Non-TXT query type is not matched by ephemeral store

- **WHEN** a DNS A query arrives for an FQDN that only has an ephemeral TXT record
- **THEN** the ephemeral store lookup SHALL return no result and the server SHALL send a negative reply

#### Scenario: Multiple ephemeral TXT entries are returned as separate RRs

- **WHEN** two ephemeral TXT entries exist for `_acme-challenge.example.com.` with values `token-A` (90 s remaining lifetime) and `token-B` (250 s remaining lifetime), and the zone file has no record for that name, and a DNS TXT query arrives
- **THEN** the server SHALL return both entries in the answer section as two separate TXT RRs — one for each value — each with TTL 30 and the AA flag set

#### Scenario: Response TTL does not decrement across repeated queries

- **WHEN** an ephemeral TXT entry for `_acme-challenge.example.com.` is inserted via the API with `ttl=3600` and a client issues the same DNS TXT query at T+0, T+500, and T+3599
- **THEN** every response SHALL carry the same RR TTL value of 30

##### Example: fixed response TTL with long API TTL

- **GIVEN** API PUT `{"value": "tok", "ttl": 3600}` at time T
- **WHEN** clients observe `dig +noall +answer TXT _acme-challenge.example.com.` at T+0, T+500, T+3599
- **THEN** each response SHALL show the same RR TTL value of 30
- **AND** at T+3600 the record SHALL have expired and the query SHALL receive NODATA

## REMOVED Requirements
