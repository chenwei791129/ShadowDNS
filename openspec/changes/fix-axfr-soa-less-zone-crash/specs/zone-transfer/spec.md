## ADDED Requirements

### Requirement: AXFR refuses a zone without a usable SOA instead of crashing

The zone-transfer subsystem SHALL NOT attempt to stream or synthesize a transfer for a zone whose apex SOA is absent. When `HandleAXFR` is invoked for a zone whose SOA is nil, it SHALL return `RCODE=REFUSED` and SHALL NOT pass a nil SOA into the streaming routine. When `HandleAliasAXFR` is invoked and the backing root zone's SOA is nil, it SHALL return `RCODE=REFUSED` and SHALL NOT invoke backup-SOA synthesis with a nil SOA. The process SHALL NOT crash in either case.

#### Scenario: AXFR for a zone with no SOA is refused

- **WHEN** `HandleAXFR` is invoked over TCP for a loaded zone whose apex SOA is absent
- **THEN** the server returns `RCODE=REFUSED` and the process keeps serving other zones without crashing

#### Scenario: Alias AXFR with a SOA-less backing root zone is refused

- **WHEN** `HandleAliasAXFR` is invoked for a backup zone whose backing root zone has no apex SOA
- **THEN** the server returns `RCODE=REFUSED` and does not call backup-SOA synthesis with a nil SOA
