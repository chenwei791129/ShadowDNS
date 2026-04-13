## ADDED Requirements

### Requirement: Parse RFC 1035 master zone files

The zone-parser SHALL accept text-format zone files conforming to RFC 1035, including the `$TTL` and `$ORIGIN` directives, the `@` shorthand for the current origin, multi-line records enclosed in `(` ... `)`, and `;` line comments. It SHALL NOT rely on a specific filename extension to identify zone files; the file path is supplied by the config-loader.

#### Scenario: Standard SOA with multi-line body

- **WHEN** a file contains `@ IN SOA ns1.root.com. root.ns1.root.com. (4230120512 ;serial\n300 ;refresh\n120 ;retry\n86400 ;expire\n3600 ) ;minimum`
- **THEN** the parser produces an SOA record with serial=4230120512, refresh=300, retry=120, expire=86400, minimum=3600 and MNAME=`ns1.root.com.`, RNAME=`root.ns1.root.com.`

#### Scenario: Records using `@` for origin

- **WHEN** the zone origin is `root.com.` and a line reads `@ IN NS ns1.root.com.`
- **THEN** the parser produces an NS record with owner name `root.com.`

#### Scenario: Comment-only and blank lines are skipped

- **WHEN** the file contains `;`-prefixed lines or blank lines
- **THEN** the parser ignores them and does not emit records

#### Scenario: Commented-out record is not emitted

- **WHEN** a line reads `;@ IN A 1.2.3.4`
- **THEN** the parser produces no record for that line

### Requirement: Build an in-memory zone structure

The zone-parser SHALL build an in-memory representation for each zone containing: (a) the zone origin, (b) the SOA record, (c) an index of records keyed by the fully-qualified owner name, and (d) the TTL applied to each record.

#### Scenario: Records are indexed by owner name for O(1) lookup

- **WHEN** a zone file defines records `www A 1.2.3.4`, `mail A 5.6.7.8`, `@ A 9.10.11.12` under origin `root.com.`
- **THEN** the resulting zone exposes lookups by keys `www.root.com.`, `mail.root.com.`, `root.com.`

#### Scenario: Default TTL applied from $TTL directive

- **WHEN** the file begins with `$TTL 300` and a record omits an explicit TTL
- **THEN** the parsed record carries TTL 300

#### Scenario: Per-record TTL overrides $TTL

- **WHEN** a record declares its own TTL (e.g., `www 600 IN A 1.2.3.4`) under `$TTL 300`
- **THEN** the parsed record carries TTL 600

### Requirement: Classify zones as root or backup override at load time

When loading all zone files, the zone-parser SHALL consult the alias map produced by the config-loader. Zones whose origin appears as a backup in the alias map SHALL be classified as backup-override zones and only records of type TXT, MX, or SRV SHALL be retained; records of other types in a backup-override zone SHALL be discarded with a warning. Zones whose origin does not appear as a backup SHALL be classified as root zones and loaded in full.

#### Scenario: Root zone retains all record types

- **WHEN** the zone origin `root.com.` is not listed as a backup in the alias map
- **THEN** all records in the zone file are retained

#### Scenario: Backup override zone retains only TXT/MX/SRV

- **WHEN** the zone origin `backup.com.` is listed as a backup of `root.com`, and its file contains A, CNAME, TXT, and MX records
- **THEN** only the TXT and MX records are retained AND a warning is logged for each discarded A and CNAME record with its owner name

#### Scenario: Backup override zone without its own file is allowed

- **WHEN** `aliases.yaml` declares `backup.com` as an alias but no zone file for `backup.com` exists
- **THEN** loading succeeds AND `backup.com` has an empty override set

### Requirement: Fail loudly on malformed zone data

The zone-parser SHALL return a fatal error that names the file path and line number when it encounters syntax that does not conform to RFC 1035, an unknown RR type, or a record whose owner name is outside the zone origin.

#### Scenario: Out-of-zone owner name is rejected

- **WHEN** a file with origin `root.com.` contains `example.org. IN A 1.2.3.4`
- **THEN** the parser returns a fatal error citing the file and line

#### Scenario: Unknown RR type is rejected

- **WHEN** a file contains an RR type that is not recognized by the miekg/dns library
- **THEN** the parser returns a fatal error citing the file and line
