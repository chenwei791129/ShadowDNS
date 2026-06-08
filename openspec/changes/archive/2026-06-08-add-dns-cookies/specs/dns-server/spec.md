## ADDED Requirements

### Requirement: Responses echo an EDNS0 OPT record when the query carries one

When a query contains an EDNS0 OPT record, every query response path (successful answers, negative answers such as NXDOMAIN/NODATA, and error rcodes such as FORMERR, NOTIMP, REFUSED, SERVFAIL — including the panic-recovery SERVFAIL and error responses to refused transfer requests) SHALL include an OPT record in the Additional section with EDNS version 0 and a sender UDP payload size of 1232 bytes. When the query carries no OPT record, the response SHALL NOT contain an OPT record. OPT echo SHALL behave identically over UDP and TCP; the UDP truncation budget remains UDP-only and TCP responses are never truncated (current behavior, unchanged). AXFR/IXFR data-stream responses produced by the dedicated transfer subsystem are out of scope and keep their current behavior.

#### Scenario: EDNS query receives OPT echo

- **WHEN** a client sends a query with an EDNS0 OPT record advertising any buffer size
- **THEN** the response SHALL contain exactly one OPT record with version 0 and UDP payload size 1232

#### Scenario: Non-EDNS query receives no OPT

- **WHEN** a client sends a query without an EDNS0 OPT record
- **THEN** the response SHALL NOT contain an OPT record

#### Scenario: Error responses also carry OPT

- **WHEN** a client sends an EDNS0 query that results in a non-success rcode (e.g., REFUSED for CHAOS class)
- **THEN** the response SHALL still contain an OPT record with version 0

#### Scenario: Negative responses carry OPT

- **WHEN** a client sends an EDNS0 query for a non-existent name in a served zone (NXDOMAIN) or an existing name with no records of the queried type (NODATA)
- **THEN** the response SHALL contain an OPT record with version 0 alongside the SOA in the Authority section

#### Scenario: OPT echo over TCP

- **WHEN** a client sends the same EDNS0 query over TCP instead of UDP
- **THEN** the response SHALL contain the same OPT record (version 0, UDP payload size 1232) as the UDP response, and the response SHALL NOT be truncated regardless of its size

### Requirement: Unsupported EDNS version receives BADVERS

When a query carries an EDNS0 OPT record with a version greater than 0, the dns-server SHALL respond with the BADVERS extended rcode per RFC 6891 Section 6.1.3. The BADVERS response SHALL echo the question section, SHALL carry an OPT record with version 0, and SHALL NOT answer the question. The version check SHALL take precedence over all COOKIE option processing: a BADVERS response SHALL NOT contain a COOKIE option regardless of what the query carried.

#### Scenario: EDNS version 1 query

- **WHEN** a client sends a query with an EDNS0 OPT record of version 1
- **THEN** the response SHALL have the BADVERS extended rcode encoded in the OPT record, the OPT version field SHALL be 0, the question section SHALL be echoed, and the Answer section SHALL be empty

#### Scenario: BADVERS takes precedence over COOKIE processing

- **WHEN** a client sends a query with an EDNS0 OPT record of version 1 that also contains a malformed 7-byte COOKIE option
- **THEN** the response SHALL be BADVERS (not FORMERR) and SHALL NOT contain a COOKIE option

### Requirement: OPT record persists through UDP truncation and counts toward the size budget

The OPT record SHALL be included in the packed wire size measured against the UDP payload budget, and UDP truncation SHALL only drop Answer-section RRs — the OPT record SHALL never be removed to satisfy the budget. The truncated response SHALL set TC=1 and still carry the OPT record.

#### Scenario: Truncated EDNS response keeps its OPT record

- **WHEN** an EDNS0 UDP query produces a response whose packed size exceeds the advertised buffer size
- **THEN** the server SHALL drop trailing Answer RRs until the packed size including the OPT record fits the budget, SHALL set TC=1, and the final response SHALL still contain the OPT record
