## ADDED Requirements

### Requirement: Collapse is a per-alias-group opt-in that defaults to off

CNAME chain collapsing SHALL apply only to zones whose root origin is declared in a `shadowdns.yaml` alias group with `collapse_cname_chain: true`. Every backup member of that group SHALL inherit the root's setting unconditionally. When the field is absent or `false`, the server SHALL emit CNAME chains exactly as it does today (full chain in the answer section), byte-identical including record order and TTLs.

#### Scenario: Flag absent preserves existing chain emission

- **WHEN** no alias group sets `collapse_cname_chain` AND a client queries `www.example.com. A` where `www` starts a two-hop in-zone CNAME chain ending at an A record
- **THEN** the answer SHALL contain the two CNAME records followed by the terminal A record, identical to the pre-feature response

#### Scenario: Backup zone inherits the root's collapse setting

- **WHEN** the alias group `example.com: {members: [example.net], collapse_cname_chain: true}` is loaded AND a client queries `www.example.net. A` whose rewritten root name starts an in-zone CNAME chain
- **THEN** the response SHALL be collapsed under the same unified rule as a query against `www.example.com.` itself

### Requirement: Collapse an in-zone CNAME chain to its terminal records

When collapsing is enabled for the matched zone and a query's resolution starts a CNAME chain at the query name, the server SHALL consume every chain hop that stays within the same zone (following the existing per-hop order: exact qtype, exact CNAME, wildcard qtype, wildcard CNAME; the depth budget is at most 8 consumed CNAME records including the initial one, and resolving a target into terminal records does not consume budget) and, when the chain terminates at in-zone records of the requested qtype, respond with ONLY those terminal records. When the query qtype is CNAME, CNAME records encountered during the chase are hops only and SHALL NOT terminate the chase as terminal records (see the direct CNAME-type query requirement). The answer SHALL contain no CNAME record. Every emitted record SHALL carry owner = the on-wire query name (preserving client-supplied case) and TTL = the minimum TTL across all consumed chain records including the terminal records. The owner rewrite applies uniformly to every qtype, including owner-sensitive types such as SOA and NS: a chain tail holding such records yields them with owner = the query name (for example a SOA RRset owned by a non-apex name), a protocol-odd but deliberate consequence of the unified rule, reachable only through pathological zone data that points a CNAME chain at the apex. For backup-zone queries the terminal records' RDATA name fields SHALL still receive the existing in-bailiwick / `rewrite_rdata_labels` rewrite, and the owner SHALL be the backup-namespace on-wire query name.

#### Scenario: Multi-hop chain collapses to a single A record

- **WHEN** collapsing is enabled and a client queries `www.example.com. A`
- **THEN** the answer SHALL contain exactly the terminal A records with owner `www.example.com.` and the chain-minimum TTL, and zero CNAME records

##### Example: TTL takes the chain minimum

- **GIVEN** zone records `www.example.com. 300 CNAME lb.example.com.`, `lb.example.com. 60 CNAME pool-a.example.com.`, `pool-a.example.com. 600 A 192.0.2.10`
- **WHEN** a client queries `www.example.com. A`
- **THEN** the answer SHALL be exactly `www.example.com. 60 A 192.0.2.10` (TTL = min(300, 60, 600))

#### Scenario: Backup-zone query collapses with backup-namespace owner

- **GIVEN** the zone records from the example above and alias group `example.com: {members: [example.net], collapse_cname_chain: true}`
- **WHEN** a client queries `www.example.net. A`
- **THEN** the answer SHALL be exactly `www.example.net. 60 A 192.0.2.10` with no CNAME records

#### Scenario: Intermediate chain names remain directly queryable and also collapse

- **WHEN** collapsing is enabled and a client directly queries an intermediate chain name such as `lb.example.com. A`
- **THEN** the server SHALL answer normally (existence is not hidden) AND the response SHALL itself be collapsed under the unified rule with owner `lb.example.com.`

#### Scenario: Wildcard-sourced chain collapses identically

- **WHEN** collapsing is enabled and the chain at the query name starts from a wildcard-synthesized CNAME (for example `*.example.com. CNAME pool-a.example.com.`)
- **THEN** the response SHALL follow the same unified rule, with owner = the on-wire query name and TTL = the chain minimum

### Requirement: Synthesize a single CNAME when the chain leaves the zone

When collapsing is enabled and the chain reaches a target name outside the matched zone's origin, or the chain depth limit is exhausted before resolution completes, the server SHALL respond with exactly one synthesized CNAME record: owner = the on-wire query name, target = the first unresolved name, TTL = the minimum TTL across all consumed chain records. No intermediate in-zone name other than the synthesized target SHALL appear in the response. For backup-zone queries the synthesized target SHALL receive the same RDATA rewrite as a stored CNAME target does today (label-anywhere when the group sets `rewrite_rdata_labels: true`, in-bailiwick suffix rule otherwise).

#### Scenario: Out-of-zone tail collapses to one synthesized CNAME

- **GIVEN** zone records `www.example.com. 300 CNAME lb.example.com.`, `lb.example.com. 60 CNAME pool-a.example.com.`, `pool-a.example.com. 600 CNAME cdn.external-vendor.example.org.`
- **WHEN** a client queries `www.example.com. A` with collapsing enabled
- **THEN** the answer SHALL be exactly `www.example.com. 60 CNAME cdn.external-vendor.example.org.` and the names `lb.example.com.` / `pool-a.example.com.` SHALL NOT appear anywhere in the response

#### Scenario: Depth-limit exhaustion is treated as an unresolved tail

- **WHEN** collapsing is enabled and the chase has consumed its full budget of 8 CNAME records (including the initial CNAME at the query name) while the 8th CNAME's target is still an unresolved in-zone name
- **THEN** the server SHALL respond with one synthesized CNAME whose target is that unresolved name (the 8th consumed CNAME's target), allowing the client to continue resolution from it

##### Example: depth budget boundary

| Chain shape (all in-zone) | Outcome |
|---|---|
| 8 CNAME records, the 8th targeting a name holding an A record | collapsed terminal A records (resolving terminal records does not consume budget) |
| 9 CNAME records ending at an A record | one synthesized CNAME, target = the 8th CNAME's target (the 9th CNAME's owner name) |
| 2 CNAME records forming a loop (a -> b -> a) | one synthesized CNAME after the budget is exhausted, target = the first unresolved name at cutoff |

When the loop cutoff lands on the query name itself, the synthesized record is self-referential (owner == target, e.g. `a.example.com. CNAME a.example.com.`). This is a documented artifact of a mis-configured loop: the server does not special-case it, and the client resolver's own chase limit terminates resolution.

#### Scenario: Synthesized target receives the templated-CNAME rewrite for backup queries

- **WHEN** a backup-zone query's collapsed chain exits the zone with a target that embeds the root origin as a middle label
- **THEN** the synthesized CNAME target SHALL receive the same rewrite a stored CNAME target receives today: label-anywhere when the group sets `rewrite_rdata_labels: true`, in-bailiwick suffix rule (a no-op for such targets) when it does not

##### Example: label-anywhere rewrite applies to the synthesized target

- **GIVEN** alias group `example.com: {members: [example.net], rewrite_rdata_labels: true, collapse_cname_chain: true}` and zone records `www.example.com. 300 CNAME edge.example.com.`, `edge.example.com. 120 CNAME www.example.com.cdn-vendor.example.org.`
- **WHEN** a client queries `www.example.net. A`
- **THEN** the answer SHALL be exactly `www.example.net. 120 CNAME www.example.net.cdn-vendor.example.org.` (target rewritten by the label-anywhere rule; TTL = min(300, 120); `edge.example.com.` does not appear)

##### Example: without rewrite_rdata_labels the out-of-zone target is emitted verbatim

- **GIVEN** the same zone records but alias group `example.com: {members: [example.net], collapse_cname_chain: true}` (`rewrite_rdata_labels` absent)
- **WHEN** a client queries `www.example.net. A`
- **THEN** the answer SHALL be exactly `www.example.net. 120 CNAME www.example.com.cdn-vendor.example.org.` (the in-bailiwick suffix rule does not match a name that only embeds the root origin as a middle label)

### Requirement: Respond NODATA when the chain ends in-zone without the requested type

When collapsing is enabled and the chain terminates at an in-zone name whose per-hop lookup yields neither records of the requested qtype nor a further CNAME, the server SHALL respond NODATA (RCODE NOERROR with the zone SOA in the authority section). This covers all three tail shapes: the name has an exact entry lacking the qtype, the name is covered by a wildcard that supplies neither the qtype nor a CNAME (the RFC 4592 wildcard-NODATA case), or the name does not exist in the zone at all (a dangling target — still NODATA, never NXDOMAIN, because the original query name exists). The server SHALL NOT emit any portion of the consumed chain and SHALL NOT fall through to wildcard synthesis for the original query name. This rule applies to any qtype whose zone lookup yields no records at the chain tail, including meta-qtypes such as ANY (which the zone's per-qtype lookup never matches).

#### Scenario: AAAA query over an A-only chain tail returns NODATA

- **GIVEN** the zone records from the multi-hop example where `pool-a.example.com.` holds only an A record
- **WHEN** a client queries `www.example.com. AAAA` with collapsing enabled
- **THEN** the response SHALL be NOERROR with zero answer records and the zone SOA in the authority section

#### Scenario: Wildcard-covered chain tail without the requested type returns NODATA

- **GIVEN** zone records `www.example.com. 300 CNAME host.pool.example.com.` and `*.pool.example.com. 600 TXT "pool"` (the wildcard covers the chain tail but holds neither an A record nor a CNAME)
- **WHEN** a client queries `www.example.com. A` with collapsing enabled
- **THEN** the response SHALL be NODATA (NOERROR with the zone SOA in the authority section), matching RFC 4592 wildcard-NODATA semantics at the tail

#### Scenario: Collapse NODATA does not trigger wildcard synthesis

- **GIVEN** zone records `www.app.example.com. 300 CNAME pool-a.example.com.`, `pool-a.example.com. 600 A 192.0.2.10`, and `*.app.example.com. 300 AAAA 2001:db8::1` (the wildcard covers the query name but not the chain tail)
- **WHEN** a client queries `www.app.example.com. AAAA` with collapsing enabled (the chain ends NODATA at `pool-a.example.com.`, which no wildcard covers)
- **THEN** the response SHALL be NODATA; the wildcard `*.app.example.com.` SHALL NOT be consulted for the original query name because `www.app.example.com.` exists

### Requirement: Direct CNAME-type queries follow the unified collapse rule

When collapsing is enabled, a query with qtype CNAME SHALL NOT reveal the stored in-zone CNAME target. The server SHALL apply the unified rule: when the chain leaves the zone, respond with the single synthesized CNAME (owner = query name, target = first unresolved name); when the chain terminates within the zone, respond NODATA. During this chase CNAME records are hops only — the per-hop qtype lookup steps SHALL NOT treat a CNAME record as terminal data when the query qtype is CNAME, so the outcome is always either the synthesized tail CNAME or NODATA.

#### Scenario: CNAME query over an out-of-zone tail returns the synthesized CNAME

- **GIVEN** the out-of-zone-tail zone records above
- **WHEN** a client queries `www.example.com. CNAME` with collapsing enabled
- **THEN** the answer SHALL be exactly `www.example.com. 60 CNAME cdn.external-vendor.example.org.`

#### Scenario: CNAME query over a fully in-zone chain returns NODATA

- **GIVEN** the multi-hop zone records whose chain terminates at the in-zone A record
- **WHEN** a client queries `www.example.com. CNAME` with collapsing enabled
- **THEN** the response SHALL be NOERROR with zero answer records and the zone SOA in the authority section

### Requirement: Zone transfers are never collapsed

AXFR responses SHALL carry the zone's stored records unchanged (including all CNAME records and, for alias transfers, their existing namespace rewrites), regardless of the `collapse_cname_chain` setting.

#### Scenario: AXFR carries the raw chain

- **WHEN** an ACL-permitted client performs AXFR of `example.com.` while `collapse_cname_chain: true` is set for that root
- **THEN** the transfer SHALL include `www.example.com. 300 CNAME lb.example.com.` and every other chain record exactly as stored
