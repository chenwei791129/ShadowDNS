## MODIFIED Requirements

### Requirement: Listen for DNS queries on UDP and TCP port 53

The dns-server SHALL bind both UDP and TCP listeners on the configured address (default `0.0.0.0:53`) and serve DNS queries on both transports. The TCP listener SHALL remain required even when zone transfer is disabled, because TCP is the RFC 7766 fallback for responses larger than the UDP payload limit.

When an ephemeral record store is attached to the server, the DNS handler SHALL consult the ephemeral store after a zone lookup returns no results and before sending a negative reply. Zone file records SHALL take precedence over ephemeral records: the ephemeral store is only consulted when the zone lookup produces no matching records for the queried name and type.

When the ephemeral store holds multiple unexpired TXT entries for the queried FQDN, the server SHALL return them as a single TXT RRSet — one TXT RR per entry, all sharing the same owner name, each with its own remaining-TTL value. Each entry's TXT value SHALL be encoded as a single string inside its own RR rather than concatenated into one RR.

The TTL value in ephemeral record responses SHALL be the remaining seconds until expiration (minimum 1 second). The Authoritative Answer (AA) flag SHALL be set on responses containing ephemeral records, consistent with zone-based responses.

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

- **WHEN** a DNS TXT query arrives for `_acme-challenge.example.com.` and the zone file has no record for that name, but the ephemeral store contains a TXT record for that FQDN with 90 seconds remaining
- **THEN** the server SHALL respond with the ephemeral TXT record, TTL set to 90, and the AA flag set

#### Scenario: Zone file record takes precedence over ephemeral record

- **WHEN** a DNS TXT query arrives for `_acme-challenge.example.com.` and both the zone file and the ephemeral store contain a TXT record for that name
- **THEN** the server SHALL respond with the zone file record only

#### Scenario: Expired ephemeral record is not returned

- **WHEN** a DNS TXT query arrives for an FQDN whose ephemeral record has expired
- **THEN** the server SHALL NOT return the expired record and SHALL send a negative reply (NXDOMAIN or NODATA) as if the record did not exist

#### Scenario: Non-TXT query type is not matched by ephemeral store

- **WHEN** a DNS A query arrives for an FQDN that only has an ephemeral TXT record
- **THEN** the ephemeral store lookup SHALL return no result and the server SHALL send a negative reply

#### Scenario: Multiple ephemeral TXT entries are returned as separate RRs

- **WHEN** two ephemeral TXT entries exist for `_acme-challenge.example.com.` with values `token-A` (90 s remaining) and `token-B` (250 s remaining), and the zone file has no record for that name, and a DNS TXT query arrives
- **THEN** the server SHALL return both entries in the answer section as two separate TXT RRs — one for each value — each with its own remaining TTL (90 and 250 respectively) and the AA flag set
