## ADDED Requirements

### Requirement: Preserve query case in the response Question section

The dns-server SHALL copy the Question section of the request into the response byte-for-byte, including the case of every label in the QNAME. The server SHALL NOT alter the case of QNAME bytes between request reception and response emission. This requirement enforces compatibility with DNS-0x20 case-randomization clients (Google Public DNS, Unbound `use-caps-for-id`, dnsmasq ≥2.91rc4) that drop responses whose Question section does not match the case of their query verbatim.

#### Scenario: Lowercase query echoed in lowercase

- **WHEN** a client sends a query for `www.example.com.`
- **THEN** the response's Question section contains `www.example.com.` byte-for-byte

#### Scenario: Mixed-case query echoed in mixed case

- **WHEN** a client sends a query for `WwW.eXaMpLe.CoM.`
- **THEN** the response's Question section contains `WwW.eXaMpLe.CoM.` byte-for-byte

#### Scenario: Uppercase query echoed in uppercase

- **WHEN** a client sends a query for `WWW.EXAMPLE.COM.`
- **THEN** the response's Question section contains `WWW.EXAMPLE.COM.` byte-for-byte

### Requirement: Preserve owner-name case in answer, authority, and additional sections

The dns-server SHALL emit owner names in the Answer, Authority, and Additional sections using the case of the data source: zone-file case for records served from a root zone, alias-rewrite output case for records served from a backup zone (which combines query-case prefix and alias-config-case suffix per the alias-resolver capability), and zone-file case for SOA / NS records in the Authority section. The server SHALL NOT lowercase owner names during response assembly.

#### Scenario: Root-zone owner case preserved from zone file

- **WHEN** a root zone file contains `Service.Root.Com. IN A 1.2.3.4` and a client queries `service.root.com. A`
- **THEN** the response Answer section owner name is `Service.Root.Com.` (zone-file case)

#### Scenario: Wildcard-synthesized owner uses query case

- **WHEN** a root zone has `*.root.com. A 1.2.3.4` and a client queries `WWW.Root.Com. A`
- **THEN** the synthesized response Answer owner name is `WWW.Root.Com.` (query case, not lowercase)

#### Scenario: SOA in NXDOMAIN authority preserves case

- **WHEN** a query for `nonexistent.root.com.` returns NXDOMAIN and the SOA record in the zone file has owner `Root.Com.`
- **THEN** the Authority section SOA record has owner `Root.Com.`
