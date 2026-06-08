## Requirements

### Requirement: Answer queries carrying a well-formed COOKIE option with a complete server cookie

When a query contains an EDNS0 COOKIE option of valid length (exactly 8 bytes, or 16 to 40 bytes inclusive), the dns-server SHALL include a COOKIE option in the response OPT record consisting of the 8-byte client cookie echoed unmodified, followed by a 16-byte server cookie freshly computed for this response. This behavior SHALL be identical over UDP and TCP transports. The server SHALL NOT validate any server cookie received from the client; a fresh server cookie SHALL be computed for every cookied response regardless of what the client presented (RFC 7873 answer-only mode). When the OPT record contains more than one COOKIE option, the server SHALL process only the first one (closest to the DNS header) and SHALL silently ignore the rest per RFC 7873 Section 5.2; multiple COOKIE options SHALL NOT trigger FORMERR.

#### Scenario: Client-only cookie receives full cookie in response

- **WHEN** a query carries a COOKIE option containing only an 8-byte client cookie
- **THEN** the response OPT record SHALL contain a 24-byte COOKIE option whose first 8 bytes equal the client cookie and whose remaining 16 bytes are the server cookie

#### Scenario: Full cookie from a previous exchange receives a fresh server cookie

- **WHEN** a query carries a 24-byte COOKIE option (8-byte client cookie + 16-byte server cookie from any prior response)
- **THEN** the response SHALL contain a 24-byte COOKIE option with the same client cookie and a server cookie recomputed for this response, AND the rcode and Answer section SHALL be identical to those of the response for the same question sent with an OPT record but without a COOKIE option

#### Scenario: Multiple COOKIE options — first wins

- **WHEN** a query's OPT record carries two COOKIE options, the first valid (8 bytes) and the second malformed (7 bytes)
- **THEN** the response SHALL NOT be FORMERR, SHALL contain exactly one COOKIE option built from the first client cookie, and the second option SHALL have no effect on the response


<!-- @trace
source: add-dns-cookies
updated: 2026-06-08
code:
  - internal/cookie/cookie.go
  - README.md
  - internal/server/server.go
  - go.sum
  - go.mod
  - internal/server/handler.go
  - NOTES.md
tests:
  - internal/server/handler_bench_test.go
  - internal/cookie/cookie_test.go
  - internal/server/handler_cookie_test.go
  - internal/server/handler_opt_test.go
  - internal/server/handler_test.go
-->

---
### Requirement: Server cookie uses the RFC 9018 interoperable format

The server cookie SHALL be 16 bytes laid out as: Version (1 byte, value 1), Reserved (3 bytes, value 0), Timestamp (4 bytes, unsigned Unix seconds, big-endian), Hash (8 bytes). The Hash SHALL be SipHash-2-4 computed over the concatenation of: client cookie (8 bytes), Version, Reserved, Timestamp, and the client IP address (4 bytes for IPv4, 16 bytes for IPv6), keyed with the 128-bit server secret. The implementation SHALL reproduce the test vectors published in RFC 9018 Appendix A.

#### Scenario: Server cookie layout

- **WHEN** the server computes a server cookie at Unix time T for client cookie C and client IP A
- **THEN** byte 0 of the server cookie SHALL be 0x01, bytes 1-3 SHALL be 0x000000, bytes 4-7 SHALL encode T big-endian, and bytes 8-15 SHALL equal SipHash-2-4(C || 0x01 || 0x000000 || T || A, secret)

#### Scenario: RFC 9018 test vectors reproduce

- **WHEN** the cookie unit tests run the published RFC 9018 Appendix A test vectors (fixed secret, fixed timestamp, known client cookie and client IP)
- **THEN** the computed server cookie SHALL match the expected bytes from the RFC for both the IPv4 and IPv6 vectors


<!-- @trace
source: add-dns-cookies
updated: 2026-06-08
code:
  - internal/cookie/cookie.go
  - README.md
  - internal/server/server.go
  - go.sum
  - go.mod
  - internal/server/handler.go
  - NOTES.md
tests:
  - internal/server/handler_bench_test.go
  - internal/cookie/cookie_test.go
  - internal/server/handler_cookie_test.go
  - internal/server/handler_opt_test.go
  - internal/server/handler_test.go
-->

---
### Requirement: Malformed COOKIE option is rejected with FORMERR

When a query contains a COOKIE option whose length is not exactly 8 bytes and not between 16 and 40 bytes inclusive, the dns-server SHALL respond with rcode FORMERR per RFC 7873 Section 5.2.2. The FORMERR response SHALL carry an OPT record and SHALL NOT contain a COOKIE option.

#### Scenario: COOKIE option length boundaries

- **WHEN** a query carries a COOKIE option of length L bytes
- **THEN** the server SHALL respond with FORMERR for invalid L and process the query normally for valid L

##### Example: length boundary table

| COOKIE option length (bytes) | Response |
| ---------------------------- | -------- |
| 7                            | FORMERR  |
| 8                            | normal answer with full cookie |
| 9                            | FORMERR  |
| 15                           | FORMERR  |
| 16                           | normal answer with full cookie |
| 40                           | normal answer with full cookie |
| 41                           | FORMERR  |


<!-- @trace
source: add-dns-cookies
updated: 2026-06-08
code:
  - internal/cookie/cookie.go
  - README.md
  - internal/server/server.go
  - go.sum
  - go.mod
  - internal/server/handler.go
  - NOTES.md
tests:
  - internal/server/handler_bench_test.go
  - internal/cookie/cookie_test.go
  - internal/server/handler_cookie_test.go
  - internal/server/handler_opt_test.go
  - internal/server/handler_test.go
-->

---
### Requirement: Queries without a COOKIE option are answered unchanged

The dns-server SHALL NOT require cookies. A query without a COOKIE option SHALL be answered exactly as before this capability existed: same rcode, same answer content, and no COOKIE option in the response. The server SHALL NOT emit the BADCOOKIE extended rcode under any circumstance.

#### Scenario: Cookie-less EDNS query is unaffected

- **WHEN** a query carries an EDNS0 OPT record without a COOKIE option, for an answer small enough that no truncation occurs under either budget
- **THEN** the response SHALL contain no COOKIE option AND its rcode, Answer, and Authority sections SHALL be identical to those of the response for the same question sent without any OPT record


<!-- @trace
source: add-dns-cookies
updated: 2026-06-08
code:
  - internal/cookie/cookie.go
  - README.md
  - internal/server/server.go
  - go.sum
  - go.mod
  - internal/server/handler.go
  - NOTES.md
tests:
  - internal/server/handler_bench_test.go
  - internal/cookie/cookie_test.go
  - internal/server/handler_cookie_test.go
  - internal/server/handler_opt_test.go
  - internal/server/handler_test.go
-->

---
### Requirement: Server secret is generated at startup and held in memory only

The dns-server SHALL generate a 128-bit cookie secret using crypto/rand exactly once at process startup. The secret SHALL NOT be configurable, SHALL NOT be persisted to disk, and SHALL NOT change on SIGHUP reload. No error-handling path is required for secret generation: the Go runtime used by this project (Go 1.24+) guarantees crypto/rand.Read does not return an error and aborts the process itself on catastrophic entropy failure.

#### Scenario: Secret survives SIGHUP

- **WHEN** the server reloads configuration via SIGHUP, and the server cookie hash is computed for the same client cookie, client IP, and a fixed timestamp T before and after the reload
- **THEN** the 8 hash bytes computed after the reload SHALL be identical to the 8 hash bytes computed before the reload

#### Scenario: Secret changes across restarts

- **WHEN** the server process is restarted
- **THEN** server cookies issued before the restart SHALL NOT match cookies computed after the restart, AND queries presenting stale cookies SHALL still be answered normally with a fresh server cookie

##### Example: stale cookie after restart

- **GIVEN** a client obtained the 24-byte cookie `aabbccddeeff0011` (client) + `01000000663bf000` + hash `H1` from server instance 1
- **WHEN** the server restarts (new random secret) and the client re-sends the same question with that stale 24-byte cookie
- **THEN** the response rcode is NOERROR with the same Answer content as before the restart, and the response COOKIE option echoes client cookie `aabbccddeeff0011` followed by a fresh 16-byte server cookie whose 8-byte hash segment differs from `H1`


<!-- @trace
source: add-dns-cookies
updated: 2026-06-08
code:
  - internal/cookie/cookie.go
  - README.md
  - internal/server/server.go
  - go.sum
  - go.mod
  - internal/server/handler.go
  - NOTES.md
tests:
  - internal/server/handler_bench_test.go
  - internal/cookie/cookie_test.go
  - internal/server/handler_cookie_test.go
  - internal/server/handler_opt_test.go
  - internal/server/handler_test.go
-->

---
### Requirement: Query log output is unchanged by cookie processing

Query-log output format and flag semantics SHALL remain unchanged by the introduction of cookie processing: the K flag continues to indicate COOKIE presence, the V flag is never emitted, and all other fields keep their existing format. (The single-parse-per-query structure that feeds cookie processing, UDP payload sizing, and query-log extraction from one OPT parse is a design-level performance constraint recorded in design.md, not an externally observable behavior.)

#### Scenario: Query log unchanged for cookied queries

- **WHEN** query logging is enabled and a query carrying a valid COOKIE option is answered
- **THEN** the query log line SHALL include the K flag and SHALL NOT include a V flag, with all other fields formatted exactly as before this capability

##### Example: identical log line shape with and without this capability

- **GIVEN** query logging enabled and an A-record query for a served name carrying EDNS0 (DO=0) with an 8-byte COOKIE option
- **WHEN** the query is answered before and after this capability is deployed
- **THEN** both log lines carry the flag set including `K` and excluding `V`, and the two lines are byte-identical in every field except timestamp and duration (no new fields, no reordered fields, no format change)


<!-- @trace
source: add-dns-cookies
updated: 2026-06-08
code:
  - internal/cookie/cookie.go
  - README.md
  - internal/server/server.go
  - go.sum
  - go.mod
  - internal/server/handler.go
  - NOTES.md
tests:
  - internal/server/handler_bench_test.go
  - internal/cookie/cookie_test.go
  - internal/server/handler_cookie_test.go
  - internal/server/handler_opt_test.go
  - internal/server/handler_test.go
-->

---
### Requirement: Cookie and OPT processing meet the performance budget

The introduction of OPT echo and cookie processing SHALL NOT degrade serving performance beyond the agreed budget, measured by dnspyre running on a separate load-generating client host across the network against the test nameserver (running ShadowDNS): the baseline SHALL be measured on the nameserver before the change in multiple rounds, the post-change run SHALL reuse exactly the same dnspyre parameters, and the result SHALL show QPS regression < 2% and p99 latency ≤ baseline p99 + run-to-run noise, where run-to-run noise is defined as (max − min) of p99 across the baseline rounds.

#### Scenario: Before/after cross-network load test stays within budget

- **WHEN** dnspyre runs on the client host against the test nameserver with identical parameters before and after deploying this change, with the baseline taken in multiple rounds
- **THEN** post-change QPS SHALL be ≥ 98% of baseline QPS, AND post-change p99 SHALL be ≤ baseline p99 + (max − min of baseline-round p99 values)

<!-- @trace
source: add-dns-cookies
updated: 2026-06-08
-->

<!-- @trace
source: add-dns-cookies
updated: 2026-06-08
code:
  - internal/cookie/cookie.go
  - README.md
  - internal/server/server.go
  - go.sum
  - go.mod
  - internal/server/handler.go
  - NOTES.md
tests:
  - internal/server/handler_bench_test.go
  - internal/cookie/cookie_test.go
  - internal/server/handler_cookie_test.go
  - internal/server/handler_opt_test.go
  - internal/server/handler_test.go
-->