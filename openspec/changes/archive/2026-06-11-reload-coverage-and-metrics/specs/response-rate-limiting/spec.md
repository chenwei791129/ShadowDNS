## ADDED Requirements

### Requirement: Rate limiter is rebuilt atomically on SIGHUP

On SIGHUP reload the server SHALL reconstruct the rate limiter from the reloaded configuration and install it atomically via `atomic.Pointer[ratelimit.Limiter]`. The old limiter's credit table SHALL be discarded; the new limiter SHALL start with an empty credit table. DNS handlers observing the pointer load SHALL see either the old limiter or the new limiter — never a torn or partially constructed state. If the new configuration has no `rate-limit` block, the pointer SHALL be set to nil (rate limiting disabled). If `ratelimit.NewLimiter` returns an error for the new configuration the reload SHALL fail and the old limiter pointer SHALL remain unchanged.

#### Scenario: Rate-limit config change takes effect on next reload

- **WHEN** the operator edits `responses-per-second` in the `rate-limit` block of named.conf and sends SIGHUP
- **THEN** the `atomic.Pointer[ratelimit.Limiter]` SHALL point to a new limiter built with the updated responses-per-second value after the swap
- **THEN** the old limiter's credit table SHALL be discarded and the new limiter's credit table SHALL start empty
- **THEN** in-flight DNS handler goroutines SHALL not observe a torn limiter state during the swap

#### Scenario: Removing rate-limit block disables rate limiting on reload

- **WHEN** the operator removes the `rate-limit { }` block from named.conf and sends SIGHUP
- **THEN** `srv.RateLimiter.Load()` SHALL return nil after the reload
- **THEN** subsequent UDP responses SHALL be sent without any rate-limit check

#### Scenario: Invalid rate-limit config causes reload failure

- **WHEN** the reloaded named.conf contains an invalid `rate-limit` block that causes `ratelimit.NewLimiter` to return an error
- **THEN** `reload()` SHALL return an error
- **THEN** the old limiter pointer SHALL remain active and `shadowdns_reload_total{result="failure"}` SHALL increment

#### Scenario: RRL metrics recorder is preserved across limiter rebuild

- **WHEN** the limiter is rebuilt on SIGHUP
- **THEN** the new `*ratelimit.Limiter` SHALL be initialised with the same `ratelimit.Recorder` (Prometheus metrics) as the old limiter so that `shadowdns_dns_rate_limit_total` continues to be recorded without interruption
