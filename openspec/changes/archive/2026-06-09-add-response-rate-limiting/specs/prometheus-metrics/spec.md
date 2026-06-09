## ADDED Requirements

### Requirement: Expose response rate limiting counters

When response rate limiting is configured, the prometheus-metrics endpoint SHALL expose a counter tracking rate-limit decisions, labeled by response category (`responses`, `nxdomains`, `nodata`, `errors`) and by action (`dropped`, `slipped`, `exempted`, `logonly_would_drop`). The counter SHALL increment once per UDP response for which the limiter took a rate-limit-relevant action. Responses that are allowed without being over-limit SHALL NOT increment this counter. When rate limiting is unconfigured, the counter MAY be absent or remain at zero.

#### Scenario: Dropped response increments the dropped counter

- **WHEN** the limiter drops an over-limit NXDOMAIN response over UDP
- **THEN** the rate-limit counter labeled category `nxdomains` and action `dropped` SHALL increment by one

#### Scenario: Slipped response increments the slipped counter

- **WHEN** the limiter truncates (slips) an over-limit positive response over UDP
- **THEN** the rate-limit counter labeled category `responses` and action `slipped` SHALL increment by one

#### Scenario: Log-only would-drop increments the logonly counter

- **WHEN** `log-only` is enabled and a response that would have been dropped is delivered unchanged
- **THEN** the rate-limit counter labeled action `logonly_would_drop` SHALL increment by one and no `dropped` increment SHALL occur
