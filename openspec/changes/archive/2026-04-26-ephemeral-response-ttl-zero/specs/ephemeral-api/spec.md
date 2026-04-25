## MODIFIED Requirements

### Requirement: Ephemeral TXT entries override exact CNAME at the same qname for TXT queries

An ephemeral TXT entry written via the API for a qname SHALL become visible to DNS TXT queries for that qname even when the zone contains an exact (non-wildcard) CNAME record at the same qname. While any live ephemeral entry exists for the qname, DNS TXT queries SHALL receive the ephemeral TXT RRSet and SHALL NOT receive the zone's CNAME record in the answer section. When all ephemeral entries for that qname expire or are deleted, DNS TXT queries SHALL revert to the standard RFC 1034 §3.6.2 CNAME synthesis behavior without further operator action.

This override SHALL apply only to `TXT` DNS query type. Queries of other types (e.g., `CNAME`, `A`, `AAAA`) at the same qname SHALL observe the zone's CNAME as usual and SHALL NOT be affected by the ephemeral store.

The override SHALL apply equally when the API writes an entry into a root-zone qname and when it writes an entry into a backup (alias) zone qname. The lookup key SHALL be the same qname the API caller used in the PUT request.

#### Scenario: ACME delegation qname with ephemeral TXT returns the ephemeral value to TXT queries

- **WHEN** the zone `example.com.` contains `_acme-challenge.foo.example.com. CNAME acme-dns.external.net.` AND the API PUTs a TXT entry `token-xyz` for `_acme-challenge.foo.example.com.` with `ttl` 120 AND a DNS TXT query arrives for `_acme-challenge.foo.example.com.`
- **THEN** the server SHALL respond with `_acme-challenge.foo.example.com. TXT "token-xyz"` (RR TTL 0, AA set), and SHALL NOT include the CNAME record

##### Example: ACME-01 validation path with local override

- **GIVEN** zone has `_acme-challenge.foo.example.com. CNAME acme-dns.external.net.` AND API PUTs `{"name": "_acme-challenge.foo.example.com.", "value": "token-xyz", "ttl": 120}`
- **WHEN** ACME validator runs `dig +short TXT _acme-challenge.foo.example.com. @<shadowdns>`
- **THEN** response contains `"token-xyz"` and nothing else

#### Scenario: CNAME query at the same qname still receives the zone CNAME

- **WHEN** the zone contains `_acme-challenge.foo.example.com. CNAME acme-dns.external.net.` AND a live ephemeral TXT entry exists for that qname AND a DNS CNAME query arrives for `_acme-challenge.foo.example.com.`
- **THEN** the server SHALL respond with the zone CNAME `acme-dns.external.net.` only; the ephemeral TXT entry SHALL NOT be exposed via CNAME queries

#### Scenario: TXT query falls back to CNAME behavior once ephemeral entries are gone

- **WHEN** the zone contains `_acme-challenge.foo.example.com. CNAME target.example.com.` AND the API previously had a TXT entry for `_acme-challenge.foo.example.com.` that has since expired (or been deleted) AND a DNS TXT query arrives
- **THEN** the server SHALL perform standard CNAME synthesis as if the ephemeral entry had never existed

#### Scenario: Override applies to backup (alias) zone qnames using the API's qname

- **WHEN** `backup.com.` is a backup of `example.com.` AND the root zone contains `_acme-challenge.foo.example.com. CNAME acme-dns.external.net.` AND the API PUTs a TXT entry for `_acme-challenge.foo.backup.com.` AND a DNS TXT query arrives for `_acme-challenge.foo.backup.com.`
- **THEN** the server SHALL respond with the ephemeral TXT RR owned by `_acme-challenge.foo.backup.com.`; the backup-rewritten CNAME SHALL NOT be emitted

#### Scenario: Ephemeral TXT response carries TTL 0 to suppress downstream caching

- **WHEN** the API PUTs a TXT entry for any in-bailiwick qname with any `ttl` body value AND a DNS TXT query arrives for that qname
- **THEN** the server SHALL emit the ephemeral TXT RR with RR header TTL set to 0, regardless of the `ttl` body value used in the PUT request

##### Example: store-side ttl 120 still produces RR TTL 0

- **GIVEN** API PUTs `{"value": "token-A", "ttl": 120}` for `_acme-challenge.example.com.`
- **WHEN** a client runs `dig +noall +answer TXT _acme-challenge.example.com. @<shadowdns>`
- **THEN** the answer line shows TTL 0, e.g. `_acme-challenge.example.com. 0 IN TXT "token-A"`
