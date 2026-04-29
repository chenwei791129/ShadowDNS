## MODIFIED Requirements

### Requirement: Apply in-bailiwick rewrite to record values

The alias-resolver SHALL rewrite DNS name values inside RDATA of record types `CNAME` (Target), `NS` (Ns), `MX` (Mx), `PTR` (Ptr), `SRV` (Target), and `SOA` (Ns, Mbox) when the value equals the root zone origin or has the root zone origin as a suffix. When the alias group declares `rewrite_rdata_labels: true`, the alias-resolver SHALL additionally rewrite occurrences of the root zone origin appearing as a contiguous label sequence elsewhere within those RDATA name values, using label-boundary matching (the matched root sequence MUST be preceded by a label boundary or the start of the name, and MUST be followed by a label boundary). When the flag is absent or false, RDATA values that do not point into the root zone SHALL be preserved byte-for-byte. Record types `A`, `AAAA`, and `TXT` SHALL NOT have their RDATA modified regardless of the flag.

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

- **WHEN** the root zone record is `ns1.root.com. A 1.2.3.4` and the query is `ns1.backup.com. A`
- **THEN** the response record is `ns1.backup.com. A 1.2.3.4`

#### Scenario: TXT RDATA is never rewritten

- **WHEN** the root zone record is `root.com. TXT "v=spf1 include:_spf.root.com ~all"` and the query is for `backup.com. TXT` with no override present
- **THEN** the response record is `backup.com. TXT "v=spf1 include:_spf.root.com ~all"` with the TXT string unchanged

#### Scenario: First match wins when root sequence appears multiple times

- **WHEN** the root zone CNAME target is `root.com.foo.root.com.bar.com.` and `rewrite_rdata_labels: true`
- **THEN** only the first contiguous root label sequence is rewritten and the response RDATA becomes `backup.com.foo.root.com.bar.com.`

### Requirement: Rewrite owner names in the answer to the original backup zone

For each resource record returned from the root zone during a backup-zone query, the alias-resolver SHALL rewrite the record's owner name so that the response carries the original backup domain. The rule: if the record owner equals the root zone origin, replace it with the backup zone origin; if the owner has the root zone as a suffix, replace that suffix with the backup zone origin. The owner-name rewrite rule SHALL remain in-bailiwick suffix-only and SHALL NOT be affected by the `rewrite_rdata_labels` flag.

#### Scenario: Owner equals root zone origin

- **WHEN** a record returned from root has owner `root.com.` and the query was for `backup.com.`
- **THEN** the response record has owner `backup.com.`

#### Scenario: Owner is subdomain of root

- **WHEN** a record returned from root has owner `www.root.com.` and the query was under `backup.com.`
- **THEN** the response record has owner `www.backup.com.`

#### Scenario: Owner rewrite ignores RDATA flag

- **WHEN** a record returned from root has owner `www.root.com.`, the query is under `backup.com.`, and the alias group declares `rewrite_rdata_labels: true`
- **THEN** the response record owner is `www.backup.com.` (suffix-only rule applied)
