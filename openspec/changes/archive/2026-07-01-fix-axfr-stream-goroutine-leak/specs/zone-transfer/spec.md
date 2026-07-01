## ADDED Requirements

### Requirement: AXFR streaming survives a mid-stream peer abort without leaking

The zone-transfer subsystem SHALL complete its AXFR/IXFR streaming routine and release all per-transfer resources even when the peer aborts the connection after the stream has begun. When a write to the peer fails partway through the envelope sequence, the streaming routine SHALL NOT block indefinitely, SHALL NOT leak the goroutine that produces envelopes, and SHALL NOT retain the materialized zone-record set beyond the request. The success-path behavior (SOA → records → SOA ordering, UDP refusal, transfer-ACL gating) SHALL remain unchanged.

#### Scenario: Peer aborts after the first envelope

- **WHEN** a permitted client begins an AXFR over TCP and then aborts the connection after receiving the first envelope, causing a subsequent write to fail
- **THEN** the streaming routine returns promptly without blocking, and neither the producer goroutine nor the zone-record set is retained after the request completes

#### Scenario: Repeated mid-stream aborts do not accumulate resources

- **WHEN** a permitted client repeatedly opens AXFR connections and aborts each one mid-stream
- **THEN** the number of live goroutines and the retained zone-record allocations do not grow without bound across the repeated attempts

#### Scenario: A panic while packing an envelope does not crash the process

- **WHEN** packing an envelope inside the transfer goroutine raises a panic
- **THEN** the panic is recovered, the transfer fails for that single request, and the server process continues serving other requests
