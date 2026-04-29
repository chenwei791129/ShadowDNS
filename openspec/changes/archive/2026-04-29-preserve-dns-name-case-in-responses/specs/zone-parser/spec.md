## ADDED Requirements

### Requirement: Preserve zone-file case in stored RRs while indexing on lowercase

The zone-parser SHALL store each parsed resource record in memory with its owner name field (`Header().Name`) and any name-bearing RDATA field (CNAME target, NS, MX exchange, PTR, SRV target, SOA MNAME, SOA RNAME) byte-for-byte as written in the zone file. The internal lookup index keyed on owner name SHALL use a lowercase-folded form of the name solely as the index key, without modifying the stored RR. Subsequent lookups SHALL fold the query name to the same lowercase form before comparing against index keys, satisfying RFC 4343 case-insensitive matching while keeping stored data case-preserving for response emission.

#### Scenario: Mixed-case owner in zone file is stored as written

- **WHEN** a zone file contains `Service.Root.Com. IN A 1.2.3.4`
- **THEN** the in-memory zone has at least one RR whose `Header().Name` equals `Service.Root.Com.` byte-for-byte

#### Scenario: Lookup with lowercase query finds the mixed-case stored record

- **WHEN** a zone file contains `Service.Root.Com. IN A 1.2.3.4` and a lookup is performed with key `service.root.com.`
- **THEN** the lookup returns the stored RR (case-insensitive index hit) whose `Header().Name` remains `Service.Root.Com.`

#### Scenario: Lookup with mixed-case query finds the same record

- **WHEN** a zone file contains `Service.Root.Com. IN A 1.2.3.4` and a lookup is performed with key `SERVICE.root.COM.`
- **THEN** the lookup returns the stored RR with `Header().Name` = `Service.Root.Com.`

#### Scenario: Mixed-case CNAME target is preserved

- **WHEN** a zone file contains `alias.root.com. IN CNAME Target.Root.Com.`
- **THEN** the stored CNAME RDATA `Target` field equals `Target.Root.Com.` byte-for-byte
