## MODIFIED Requirements

### Requirement: Successful answer responses SHALL use DNS name compression

The dns-server SHALL produce successful authoritative answer responses with DNS name compression enabled per RFC 1035 §4.1.4. This applies to both UDP and TCP transports. `dns.Msg.Compress` SHALL be set to `true` before the message is packed or written to the transport.

#### Scenario: Reply with multiple RRs sharing an owner name uses compression pointers

- **GIVEN** a query answered with two or more RRs at the same owner name (for example ephemeral TXT RRset at `_acme-challenge.<zone>`)
- **WHEN** the server serializes the reply
- **THEN** the second and subsequent occurrences of the owner name in the wire format SHALL be encoded as 2-byte compression pointers, not full labels

##### Example: 48 TXT RRs at `_acme-challenge.example.com.` with 43-byte values

- **GIVEN** 48 TXT RRs sharing owner name `_acme-challenge.example.com.`, each carrying a 43-byte base64url challenge value, TTL 0
- **WHEN** the server packs an authoritative reply
- **THEN** the packed wire size SHALL be under 3000 bytes (compressed), not around 4000 bytes (uncompressed), enabling fit within a 4096-byte EDNS0 UDP buffer without truncation
