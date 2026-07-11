## MODIFIED Requirements

### Requirement: Apply in-bailiwick rewrite to record values

The alias-resolver SHALL rewrite DNS name values inside RDATA of record types `CNAME` (Target), `NS` (Ns), `MX` (Mx), `PTR` (Ptr), `SRV` (Target), `SOA` (Ns, Mbox), `HTTPS` (Target), `SVCB` (Target), `DNAME` (Target), `NAPTR` (Replacement), `RP` (Mbox, Txt), `KX` (Exchanger), `AFSDB` (Hostname), `PX` (Map822, Mapx400), and `RT` (Host) when the value equals the root zone origin or has the root zone origin as a suffix. When the alias group declares `rewrite_rdata_labels: true`, the alias-resolver SHALL additionally rewrite occurrences of the root zone origin appearing as a contiguous label sequence elsewhere within those RDATA name values, using label-boundary matching (the matched root sequence MUST be preceded by a label boundary or the start of the name, and MUST be followed by a label boundary). When the flag is absent or false, RDATA values that do not point into the root zone SHALL be preserved byte-for-byte. Record types `A`, `AAAA`, and `TXT` SHALL NOT have their RDATA modified regardless of the flag. Record types that carry a domain name in their RDATA but are NOT in the list above SHALL NOT be emitted in backup-zone answers; the alias-resolver SHALL withhold them per the requirement "Withhold uncovered name-bearing records from backup answers" rather than emit an unrewritten RDATA name.

#### Scenario: CNAME pointing within root zone is rewritten

- **WHEN** the root zone record is `blog.root.com. CNAME service.root.com.` and the query is under `backup.com.`
- **THEN** the response record is `blog.backup.com. CNAME service.backup.com.`

#### Scenario: CNAME pointing to a third party is preserved when flag is false

- **WHEN** the root zone record is `app.root.com. CNAME abc.us-east-1.elb.amazonaws.com.` and the query is under `backup.com.` with `rewrite_rdata_labels: false`
- **THEN** the response record is `app.backup.com. CNAME abc.us-east-1.elb.amazonaws.com.`

#### Scenario: CNAME with mid-label root sequence is rewritten when flag is true

- **WHEN** the root zone record is `host.root.com. CNAME host.root.com.cdn.example.net.`, the query is under `backup.com.`, and the alias group declares `rewrite_rdata_labels: true`
- **THEN** the response record is `host.backup.com. CNAME host.backup.com.cdn.example.net.`

##### Example: label-boundary protection

| RDATA value (root=`root.com.`, backup=`backup.com.`) | flag | Rewritten value |
| --- | --- | --- |
| `host.root.com.cdn.example.net.` | true | `host.backup.com.cdn.example.net.` |
| `host.root.com.cdn.example.net.` | false | `host.root.com.cdn.example.net.` |
| `myroot.com.foo.com.` | true | `myroot.com.foo.com.` |
| `prefixroot.com.foo.com.` | true | `prefixroot.com.foo.com.` |
| `root.com.cdn.example.net.` | true | `backup.com.cdn.example.net.` |
| `service.root.com.` | true | `service.backup.com.` |
| `service.root.com.` | false | `service.backup.com.` |
| `abc.us-east-1.elb.amazonaws.com.` | true | `abc.us-east-1.elb.amazonaws.com.` |

#### Scenario: NS value within root zone is rewritten

- **WHEN** the root zone record is `root.com. NS ns1.root.com.` and the query is under `backup.com.`
- **THEN** the response record is `backup.com. NS ns1.backup.com.`

#### Scenario: NS value to external nameserver is preserved

- **WHEN** the root zone record is `root.com. NS ns1.externaldns.net.` and the alias group has `rewrite_rdata_labels: false`
- **THEN** the response record value is `ns1.externaldns.net.` unchanged

#### Scenario: SOA MNAME and RNAME within root zone are rewritten

- **WHEN** the root zone SOA is `root.com. SOA ns1.root.com. root.ns1.root.com. (...)` and the query is for `backup.com. SOA`
- **THEN** the response is `backup.com. SOA ns1.backup.com. root.ns1.backup.com. (...)` with all numeric fields preserved byte-for-byte

#### Scenario: A and AAAA RDATA are never rewritten

- **WHEN** the root zone record is `ns1.root.com. A 192.0.2.4` and the query is `ns1.backup.com. A`
- **THEN** the response record is `ns1.backup.com. A 192.0.2.4`

#### Scenario: TXT RDATA is never rewritten

- **WHEN** the root zone record is `root.com. TXT "v=spf1 include:_spf.root.com ~all"` and the query is for `backup.com. TXT` with no override present
- **THEN** the response record is `backup.com. TXT "v=spf1 include:_spf.root.com ~all"` with the TXT string unchanged

#### Scenario: First match wins when root sequence appears multiple times

- **WHEN** the root zone CNAME target is `root.com.foo.root.com.bar.com.` and `rewrite_rdata_labels: true`
- **THEN** only the first contiguous root label sequence is rewritten and the response RDATA becomes `backup.com.foo.root.com.bar.com.`

#### Scenario: HTTPS Target within root zone is rewritten

- **WHEN** the root zone record is `www.root.com. HTTPS 1 svc.root.com. alpn="h2"` and the query is `www.backup.com. HTTPS`
- **THEN** the response record is `www.backup.com. HTTPS 1 svc.backup.com. alpn="h2"` with the SvcParams preserved and no `root.com.` remaining in the RDATA

#### Scenario: SVCB Target to a third party is preserved

- **WHEN** the root zone record is `_dns.root.com. SVCB 1 doh.externalcdn.net.` and the query is under `backup.com.` with `rewrite_rdata_labels: false`
- **THEN** the response record Target is `doh.externalcdn.net.` unchanged

#### Scenario: DNAME Target within root zone is rewritten

- **WHEN** the root zone record is `sub.root.com. DNAME target.root.com.` and the query is `sub.backup.com. DNAME`
- **THEN** the response record is `sub.backup.com. DNAME target.backup.com.`

#### Scenario: NAPTR Replacement within root zone is rewritten

- **WHEN** the root zone record is `root.com. NAPTR 100 10 "" "" "" svc.root.com.` and the query is `backup.com. NAPTR`
- **THEN** the response record Replacement is `svc.backup.com.` with the order, preference, flags, service, and regexp fields preserved byte-for-byte

#### Scenario: RP Mbox and Txt within root zone are rewritten

- **WHEN** the root zone record is `root.com. RP admin.root.com. info.root.com.` and the query is `backup.com. RP`
- **THEN** the response record is `backup.com. RP admin.backup.com. info.backup.com.`

## ADDED Requirements

### Requirement: Withhold uncovered name-bearing records from backup answers

The alias-resolver SHALL NOT emit, in any backup-zone answer, a resource record whose type carries a domain name in its RDATA but whose type is not covered by the requirement "Apply in-bailiwick rewrite to record values". Such a record SHALL be withheld from the response (producing an empty answer for that name and type, not `NXDOMAIN` and not `SERVFAIL`), and the alias-resolver SHALL log a warning identifying the withheld record type and owner name. Records of covered types and records that carry no domain name in their RDATA (such as `A`, `AAAA`, `TXT`) in the same answer SHALL still be returned. Whether a record type carries a domain name in its RDATA SHALL be determined authoritatively from the record type's RDATA name-field metadata (not a hand-maintained list), so that any name-bearing type absent from the covered list is withheld by default. This rule SHALL apply identically to the live query path and to the synthesized alias zone-transfer (AXFR) path.

#### Scenario: DNSSEC record under an alias is withheld

- **WHEN** the root zone contains an `RRSIG` (or `NSEC`) record at a name reachable under `backup.com.` and a client queries that name and type through the backup zone
- **THEN** the record is not included in the response, the answer is empty (NODATA) for that name and type, and a warning is logged naming the withheld record type and owner

#### Scenario: Uncovered name-bearing record does not leak the backend origin

- **WHEN** a backup-zone answer would otherwise contain a name-bearing record type not in the covered list
- **THEN** no record whose RDATA still references the root zone origin is emitted for that name and type

#### Scenario: Other records in the same answer are unaffected

- **WHEN** a backup-zone response would contain both a covered record (for example `A`) and an uncovered name-bearing record for the same owner
- **THEN** the covered record is returned normally and only the uncovered name-bearing record is withheld

#### Scenario: Withholding applies to synthesized alias AXFR

- **WHEN** an authorized peer performs an AXFR of a backup alias zone whose root zone contains an uncovered name-bearing record type
- **THEN** the transferred zone omits that record and includes the fully rewritten records of covered types
