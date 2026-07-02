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
- **THEN** the server SHALL retain the longest leading prefix of the Answer section whose packed size is ≤ 4096 bytes, drop the remaining trailing Answer RRs, and set TC=1 in the final packet
- **AND** the set of retained Answer RRs SHALL be identical to what a one-RR-at-a-time drop-and-repack loop would retain for the same input

#### Scenario: UDP response without EDNS0 falls back to 512-byte budget

- **WHEN** a client sends a UDP query with no EDNS0 OPT record
- **THEN** the server's UDP response SHALL have a packed wire size ≤ 512 bytes (RFC 1035 §2.3.4) and SHALL set TC=1 when RRs are dropped to meet that limit

#### Scenario: UDP truncation cost is bounded logarithmically in the number of dropped RRs

- **GIVEN** a UDP query for a name holding a large single-owner RRset of N Answer RRs that must be trimmed to fit the UDP budget
- **WHEN** the server truncates the response to fit the budget
- **THEN** the number of `dns.Msg.Pack()` invocations performed by the truncation routine SHALL be logarithmic in the number of dropped RRs (O(log N)), not linear (O(N))
- **AND** the final packed wire size SHALL be ≤ the budget with TC=1 set whenever any Answer RR was dropped

#### Scenario: Truncation leaves the message unchanged on a pack failure

- **GIVEN** a response whose `dns.Msg.Pack()` returns an error during truncation
- **WHEN** the truncation routine encounters that error
- **THEN** it SHALL leave the message's Answer section exactly as it was on entry and return, so the subsequent write surfaces the same error through the normal path
