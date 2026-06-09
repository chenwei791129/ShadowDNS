# response-rate-limiting Specification

## Purpose

TBD - created by archiving change 'add-response-rate-limiting'. Update Purpose after archive.

## Requirements

### Requirement: Apply response rate limiting only to UDP responses

The response-rate-limiting capability SHALL evaluate rate limits only for responses sent over UDP transport. Responses sent over TCP SHALL bypass the limiter entirely and SHALL be delivered unchanged, because TCP source addresses cannot be spoofed and the connection has already completed a three-way handshake. When no `rate-limit` configuration is present, the limiter SHALL NOT be installed and the response path SHALL behave identically to a build without this capability.

#### Scenario: TCP response bypasses the limiter

- **WHEN** a response is written over TCP transport while rate limiting is configured and active
- **THEN** the response SHALL be delivered unchanged and SHALL NOT consume any credit from any account

#### Scenario: UDP response is evaluated

- **WHEN** a response is written over UDP transport while rate limiting is configured with a non-zero limit for its category
- **THEN** the limiter SHALL evaluate the matching account before the response is delivered

#### Scenario: No configuration means no limiting

- **WHEN** the configuration contains no `rate-limit` block
- **THEN** every response SHALL be delivered unchanged over both UDP and TCP and no account state SHALL be created


<!-- @trace
source: add-response-rate-limiting
updated: 2026-06-09
code:
  - internal/ratelimit/exempt.go
  - cmd/shadowdns/main.go
  - internal/config/options.go
  - internal/server/handler.go
  - internal/ratelimit/writer.go
  - testdata/integration/named.conf
  - internal/config/ratelimit.go
  - internal/config/zones.go
  - internal/ratelimit/slip.go
  - internal/ratelimit/table.go
  - internal/ratelimit/classify.go
  - internal/ratelimit/limiter.go
  - internal/metrics/metrics.go
  - internal/server/server.go
  - README.md
  - internal/ratelimit/key.go
tests:
  - internal/config/ratelimit_test.go
  - internal/ratelimit/classify_test.go
  - internal/ratelimit/table_test.go
  - internal/ratelimit/limiter_decide_test.go
  - internal/server/handler_ratelimit_test.go
  - internal/metrics/metrics_ratelimit_test.go
  - internal/ratelimit/writer_test.go
  - internal/ratelimit/slip_test.go
  - internal/ratelimit/limiter_credit_test.go
  - internal/ratelimit/key_test.go
  - internal/config/ratelimit_warn_test.go
-->

---
### Requirement: Account credit accounting over a rolling window

The limiter SHALL maintain, for each account, a credit balance that regenerates at the configured per-second rate for the account's category and is capped at `window × rate` credits. Each matching response SHALL debit one credit. A response SHALL be considered over-limit when debiting would drive the balance below zero. A configured rate of `0` for a category SHALL mean that category is not limited. The `all-per-second` limit, when greater than `0`, SHALL apply as an additional aggregate account that every UDP response debits regardless of its category; a response SHALL be treated as over-limit when either its category account or the aggregate account is over-limit.

#### Scenario: Responses within the rate are allowed

- **WHEN** the configured `responses-per-second` is `R` and fewer than `R` matching responses for one account occur within one second
- **THEN** each such response SHALL be allowed and SHALL debit one credit

#### Scenario: Responses exceeding the rate are over-limit

- **WHEN** the configured `responses-per-second` is `R` and more than `R` matching responses for one account occur within one second
- **THEN** the responses beyond `R` SHALL be treated as over-limit

##### Example: credit regeneration over a window

- **GIVEN** `responses-per-second = 5` and `window = 15`, so the balance caps at 75 credits
- **WHEN** 5 matching responses arrive in the first second, then no traffic for 2 seconds, then more traffic arrives
- **THEN** the first 5 are allowed, and after the 2-second idle the account SHALL have regenerated up to 10 additional credits (5 per second), capped at 75

#### Scenario: Zero rate disables a category

- **WHEN** `nxdomains-per-second` is `0`
- **THEN** NXDOMAIN responses SHALL never be treated as over-limit on the NXDOMAIN category account


<!-- @trace
source: add-response-rate-limiting
updated: 2026-06-09
code:
  - internal/ratelimit/exempt.go
  - cmd/shadowdns/main.go
  - internal/config/options.go
  - internal/server/handler.go
  - internal/ratelimit/writer.go
  - testdata/integration/named.conf
  - internal/config/ratelimit.go
  - internal/config/zones.go
  - internal/ratelimit/slip.go
  - internal/ratelimit/table.go
  - internal/ratelimit/classify.go
  - internal/ratelimit/limiter.go
  - internal/metrics/metrics.go
  - internal/server/server.go
  - README.md
  - internal/ratelimit/key.go
tests:
  - internal/config/ratelimit_test.go
  - internal/ratelimit/classify_test.go
  - internal/ratelimit/table_test.go
  - internal/ratelimit/limiter_decide_test.go
  - internal/server/handler_ratelimit_test.go
  - internal/metrics/metrics_ratelimit_test.go
  - internal/ratelimit/writer_test.go
  - internal/ratelimit/slip_test.go
  - internal/ratelimit/limiter_credit_test.go
  - internal/ratelimit/key_test.go
  - internal/config/ratelimit_warn_test.go
-->

---
### Requirement: Account key construction with name imputation

The limiter SHALL build each account key from the client address masked by `ipv4-prefix-length` (default 24) or `ipv6-prefix-length` (default 56), the response category, and an imputed name. The imputed name SHALL be derived per category: for `responses` (positive answers) the exact query name SHALL be used; for `nxdomains` and `nodata` the matched zone origin SHALL be used so that a flood of distinct non-existent names under one zone aggregates into a single account; for `errors` (including REFUSED for names outside all zones) an empty name SHALL be used so that all error responses to one client block aggregate into a single account.

#### Scenario: Positive answers key on the query name

- **WHEN** two UDP queries from the same client block request different existing names that both return positive answers
- **THEN** the two responses SHALL be accounted under distinct accounts keyed by their respective query names

#### Scenario: Random-subdomain NXDOMAIN flood aggregates per zone

- **WHEN** many UDP queries from the same client block request distinct non-existent names under one zone, all returning NXDOMAIN
- **THEN** all those NXDOMAIN responses SHALL be accounted under a single account keyed by the zone origin

##### Example: NXDOMAIN aggregation

- **GIVEN** zone origin `example.com.`, `nxdomains-per-second = 5`, and queries for `a1.example.com`, `a2.example.com`, … each non-existent
- **WHEN** 20 such queries arrive in one second from the same client block
- **THEN** all 20 share one account keyed by `(client-block, nxdomains, example.com.)` and responses beyond 5 SHALL be over-limit

#### Scenario: Client address is masked to the configured prefix

- **WHEN** `ipv4-prefix-length = 24` and two queries arrive from `192.0.2.10` and `192.0.2.200`
- **THEN** both SHALL map to the same masked client block `192.0.2.0/24` for accounting


<!-- @trace
source: add-response-rate-limiting
updated: 2026-06-09
code:
  - internal/ratelimit/exempt.go
  - cmd/shadowdns/main.go
  - internal/config/options.go
  - internal/server/handler.go
  - internal/ratelimit/writer.go
  - testdata/integration/named.conf
  - internal/config/ratelimit.go
  - internal/config/zones.go
  - internal/ratelimit/slip.go
  - internal/ratelimit/table.go
  - internal/ratelimit/classify.go
  - internal/ratelimit/limiter.go
  - internal/metrics/metrics.go
  - internal/server/server.go
  - README.md
  - internal/ratelimit/key.go
tests:
  - internal/config/ratelimit_test.go
  - internal/ratelimit/classify_test.go
  - internal/ratelimit/table_test.go
  - internal/ratelimit/limiter_decide_test.go
  - internal/server/handler_ratelimit_test.go
  - internal/metrics/metrics_ratelimit_test.go
  - internal/ratelimit/writer_test.go
  - internal/ratelimit/slip_test.go
  - internal/ratelimit/limiter_credit_test.go
  - internal/ratelimit/key_test.go
  - internal/config/ratelimit_warn_test.go
-->

---
### Requirement: Classify each response into a rate-limit category

The limiter SHALL classify each UDP response into exactly one category derived from the response message: a NOERROR response with a non-empty answer section SHALL be `responses`; a NOERROR response with an empty answer section SHALL be `nodata`; an NXDOMAIN response SHALL be `nxdomains`; any other error rcode (including SERVFAIL, FORMERR, REFUSED, NOTIMP) SHALL be `errors`. The `referrals` category SHALL be accepted in configuration for BIND compatibility but SHALL never be assigned, because this server is authoritative-only and does not emit referral responses.

#### Scenario: Classification by rcode and answer section

- **WHEN** the limiter classifies a response message
- **THEN** the assigned category SHALL follow the mapping below

##### Example: category mapping

| Rcode | Answer section | Category |
| ----- | -------------- | -------- |
| NOERROR | non-empty | responses |
| NOERROR | empty | nodata |
| NXDOMAIN | empty | nxdomains |
| SERVFAIL | empty | errors |
| REFUSED | empty | errors |
| FORMERR | empty | errors |


<!-- @trace
source: add-response-rate-limiting
updated: 2026-06-09
code:
  - internal/ratelimit/exempt.go
  - cmd/shadowdns/main.go
  - internal/config/options.go
  - internal/server/handler.go
  - internal/ratelimit/writer.go
  - testdata/integration/named.conf
  - internal/config/ratelimit.go
  - internal/config/zones.go
  - internal/ratelimit/slip.go
  - internal/ratelimit/table.go
  - internal/ratelimit/classify.go
  - internal/ratelimit/limiter.go
  - internal/metrics/metrics.go
  - internal/server/server.go
  - README.md
  - internal/ratelimit/key.go
tests:
  - internal/config/ratelimit_test.go
  - internal/ratelimit/classify_test.go
  - internal/ratelimit/table_test.go
  - internal/ratelimit/limiter_decide_test.go
  - internal/server/handler_ratelimit_test.go
  - internal/metrics/metrics_ratelimit_test.go
  - internal/ratelimit/writer_test.go
  - internal/ratelimit/slip_test.go
  - internal/ratelimit/limiter_credit_test.go
  - internal/ratelimit/key_test.go
  - internal/config/ratelimit_warn_test.go
-->

---
### Requirement: Slip behavior chooses between dropping and truncating over-limit responses

When a response is over-limit and `log-only` is not enabled, the limiter SHALL apply the `slip` parameter (default 2) to decide the action. A `slip` of `0` SHALL drop every over-limit response. A `slip` of `1` SHALL truncate every over-limit response. A `slip` of `n` where `n ≥ 2` SHALL truncate every nth over-limit response per account and drop the rest. A dropped response SHALL NOT be written to the client. A truncated response SHALL have its answer, authority, and additional sections cleared except for the OPT record echo, SHALL have the TC bit set, SHALL preserve the original rcode and question section, and SHALL be written to the client so a legitimate resolver retries over TCP.

#### Scenario: Slip of zero drops all over-limit responses

- **WHEN** `slip = 0` and a UDP response is over-limit
- **THEN** the response SHALL NOT be written to the client

#### Scenario: Slip of one truncates all over-limit responses

- **WHEN** `slip = 1` and a UDP response is over-limit
- **THEN** the response SHALL be written with the TC bit set, the answer section cleared, and the OPT record preserved

#### Scenario: Slip of two alternates truncate and drop

- **WHEN** `slip = 2` and a sequence of over-limit responses occurs for one account
- **THEN** every second over-limit response SHALL be truncated and the others SHALL be dropped

##### Example: slip=2 sequence

- **GIVEN** `slip = 2` and 4 consecutive over-limit responses for one account
- **WHEN** they are processed in order
- **THEN** the actions SHALL be: truncate, drop, truncate, drop


<!-- @trace
source: add-response-rate-limiting
updated: 2026-06-09
code:
  - internal/ratelimit/exempt.go
  - cmd/shadowdns/main.go
  - internal/config/options.go
  - internal/server/handler.go
  - internal/ratelimit/writer.go
  - testdata/integration/named.conf
  - internal/config/ratelimit.go
  - internal/config/zones.go
  - internal/ratelimit/slip.go
  - internal/ratelimit/table.go
  - internal/ratelimit/classify.go
  - internal/ratelimit/limiter.go
  - internal/metrics/metrics.go
  - internal/server/server.go
  - README.md
  - internal/ratelimit/key.go
tests:
  - internal/config/ratelimit_test.go
  - internal/ratelimit/classify_test.go
  - internal/ratelimit/table_test.go
  - internal/ratelimit/limiter_decide_test.go
  - internal/server/handler_ratelimit_test.go
  - internal/metrics/metrics_ratelimit_test.go
  - internal/ratelimit/writer_test.go
  - internal/ratelimit/slip_test.go
  - internal/ratelimit/limiter_credit_test.go
  - internal/ratelimit/key_test.go
  - internal/config/ratelimit_warn_test.go
-->

---
### Requirement: Exempt clients bypass rate limiting

The limiter SHALL accept an `exempt-clients` address-match-list. A response whose masked or unmasked client address matches the exempt list SHALL always be allowed, SHALL NOT consume credit, and SHALL be delivered unchanged. The exemption check SHALL occur before any account lookup.

#### Scenario: Exempt client is never limited

- **WHEN** `exempt-clients` contains `192.0.2.0/24` and a flood of UDP responses is sent to `192.0.2.5`
- **THEN** every response SHALL be delivered unchanged regardless of rate


<!-- @trace
source: add-response-rate-limiting
updated: 2026-06-09
code:
  - internal/ratelimit/exempt.go
  - cmd/shadowdns/main.go
  - internal/config/options.go
  - internal/server/handler.go
  - internal/ratelimit/writer.go
  - testdata/integration/named.conf
  - internal/config/ratelimit.go
  - internal/config/zones.go
  - internal/ratelimit/slip.go
  - internal/ratelimit/table.go
  - internal/ratelimit/classify.go
  - internal/ratelimit/limiter.go
  - internal/metrics/metrics.go
  - internal/server/server.go
  - README.md
  - internal/ratelimit/key.go
tests:
  - internal/config/ratelimit_test.go
  - internal/ratelimit/classify_test.go
  - internal/ratelimit/table_test.go
  - internal/ratelimit/limiter_decide_test.go
  - internal/server/handler_ratelimit_test.go
  - internal/metrics/metrics_ratelimit_test.go
  - internal/ratelimit/writer_test.go
  - internal/ratelimit/slip_test.go
  - internal/ratelimit/limiter_credit_test.go
  - internal/ratelimit/key_test.go
  - internal/config/ratelimit_warn_test.go
-->

---
### Requirement: Log-only mode records but does not enforce

When `log-only` is set to `yes`, the limiter SHALL compute the over-limit decision and SHALL record each would-be drop or truncate (via log and a dedicated counter), but SHALL allow every response to be delivered unchanged. Credit accounting SHALL still run so that recorded decisions reflect what enforcement would do.

#### Scenario: Log-only does not change responses

- **WHEN** `log-only = yes` and a UDP response is over-limit
- **THEN** the response SHALL be delivered unchanged AND the would-be action SHALL be recorded


<!-- @trace
source: add-response-rate-limiting
updated: 2026-06-09
code:
  - internal/ratelimit/exempt.go
  - cmd/shadowdns/main.go
  - internal/config/options.go
  - internal/server/handler.go
  - internal/ratelimit/writer.go
  - testdata/integration/named.conf
  - internal/config/ratelimit.go
  - internal/config/zones.go
  - internal/ratelimit/slip.go
  - internal/ratelimit/table.go
  - internal/ratelimit/classify.go
  - internal/ratelimit/limiter.go
  - internal/metrics/metrics.go
  - internal/server/server.go
  - README.md
  - internal/ratelimit/key.go
tests:
  - internal/config/ratelimit_test.go
  - internal/ratelimit/classify_test.go
  - internal/ratelimit/table_test.go
  - internal/ratelimit/limiter_decide_test.go
  - internal/server/handler_ratelimit_test.go
  - internal/metrics/metrics_ratelimit_test.go
  - internal/ratelimit/writer_test.go
  - internal/ratelimit/slip_test.go
  - internal/ratelimit/limiter_credit_test.go
  - internal/ratelimit/key_test.go
  - internal/config/ratelimit_warn_test.go
-->

---
### Requirement: Account table capacity is bounded

The limiter SHALL bound the number of tracked accounts between `min-table-size` (default 500) and `max-table-size` (default 20000). When the table is full and a new account is needed, the limiter SHALL evict the least-recently-used account rather than reject the new one or grow without limit. Table maintenance SHALL NOT block the response hot path.

#### Scenario: Full table evicts least-recently-used account

- **WHEN** the account table has reached `max-table-size` and a response requires a new account
- **THEN** the least-recently-used existing account SHALL be evicted to make room and the new account SHALL be created

<!-- @trace
source: add-response-rate-limiting
updated: 2026-06-09
code:
  - internal/ratelimit/exempt.go
  - cmd/shadowdns/main.go
  - internal/config/options.go
  - internal/server/handler.go
  - internal/ratelimit/writer.go
  - testdata/integration/named.conf
  - internal/config/ratelimit.go
  - internal/config/zones.go
  - internal/ratelimit/slip.go
  - internal/ratelimit/table.go
  - internal/ratelimit/classify.go
  - internal/ratelimit/limiter.go
  - internal/metrics/metrics.go
  - internal/server/server.go
  - README.md
  - internal/ratelimit/key.go
tests:
  - internal/config/ratelimit_test.go
  - internal/ratelimit/classify_test.go
  - internal/ratelimit/table_test.go
  - internal/ratelimit/limiter_decide_test.go
  - internal/server/handler_ratelimit_test.go
  - internal/metrics/metrics_ratelimit_test.go
  - internal/ratelimit/writer_test.go
  - internal/ratelimit/slip_test.go
  - internal/ratelimit/limiter_credit_test.go
  - internal/ratelimit/key_test.go
  - internal/config/ratelimit_warn_test.go
-->