## MODIFIED Requirements

### Requirement: Listen for DNS queries on UDP and TCP port 53

The dns-server SHALL bind both UDP and TCP listeners on the configured address (default `0.0.0.0:53`) and serve DNS queries on both transports. The TCP listener SHALL remain required even when zone transfer is disabled, because TCP is the RFC 7766 fallback for responses larger than the UDP payload limit.

#### Scenario: UDP query receives response

- **WHEN** a client sends a valid DNS query over UDP on port 53
- **THEN** the server responds over UDP within the same 5-tuple

#### Scenario: TCP query receives response

- **WHEN** a client sends a valid DNS query over TCP on port 53
- **THEN** the server accepts the connection, reads the 2-byte length prefix, and writes a length-prefixed response

#### Scenario: UDP response size SHALL NOT exceed the advertised EDNS0 buffer

- **WHEN** a client sends a UDP query with an EDNS0 OPT record advertising a buffer size of N bytes
- **THEN** the server's UDP response SHALL have a wire size (as produced by `dns.Msg.Pack()`) less than or equal to N bytes
- **AND** when the untruncated response would exceed N bytes, the server SHALL drop trailing Answer-section RRs and set the TC (truncated) flag until the packed response fits within N bytes

##### Example: Client advertises 4096, answer set would serialize to 6000 bytes

- **GIVEN** a DNS query with EDNS0 UDPSize=4096 asking for TXT at an FQDN with enough RRs to serialize to 6000 bytes after compression
- **WHEN** the server builds the reply
- **THEN** the server SHALL pack the reply, observe 6000 > 4096, drop trailing Answer RRs one at a time and re-pack until the packed size ≤ 4096 bytes, and set TC=1 in the final packet

#### Scenario: UDP response without EDNS0 falls back to 512-byte budget

- **WHEN** a client sends a UDP query with no EDNS0 OPT record
- **THEN** the server's UDP response SHALL have a packed wire size ≤ 512 bytes (RFC 1035 §2.3.4) and SHALL set TC=1 when RRs are dropped to meet that limit

## ADDED Requirements

### Requirement: Successful answer responses SHALL use DNS name compression

The dns-server SHALL produce successful authoritative answer responses with DNS name compression enabled per RFC 1035 §4.1.4. This applies to both UDP and TCP transports. `dns.Msg.Compress` SHALL be set to `true` before the message is packed or written to the transport.

#### Scenario: Reply with multiple RRs sharing an owner name uses compression pointers

- **GIVEN** a query answered with two or more RRs at the same owner name (for example ephemeral TXT RRset at `_acme-challenge.<zone>`)
- **WHEN** the server serializes the reply
- **THEN** the second and subsequent occurrences of the owner name in the wire format SHALL be encoded as 2-byte compression pointers, not full labels

##### Example: 48 TXT RRs at `_acme-challenge.example.com.` with 43-byte values

- **GIVEN** 48 TXT RRs sharing owner name `_acme-challenge.example.com.`, each carrying a 43-byte base64url challenge value, TTL 30
- **WHEN** the server packs an authoritative reply
- **THEN** the packed wire size SHALL be under 3000 bytes (compressed), not around 4000 bytes (uncompressed), enabling fit within a 4096-byte EDNS0 UDP buffer without truncation
