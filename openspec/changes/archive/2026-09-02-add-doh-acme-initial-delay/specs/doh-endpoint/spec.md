## MODIFIED Requirements

### Requirement: DoH server listens on a configured address

The DoH server SHALL listen for HTTPS connections on the address specified in the `doh.listen` field of the unified ShadowDNS YAML configuration loaded via `--config`. The server SHALL start only when the `doh` section is present in the configuration. When the section is absent, no DoH server, no ACME client, and no ACME HTTP-01 listener SHALL be started.

The `doh` section SHALL use the following YAML fields. Loading SHALL fail with an error naming the missing field when any required field is absent (consistent with the existing strict `KnownFields(true)` decoding). An optional field SHALL be accepted when absent and SHALL take its documented default.

| Field | Required | Meaning |
|-------|----------|---------|
| `doh.listen` | yes | host:port the DoH HTTPS service binds (for example `203.0.113.10:443`) |
| `doh.acme.directory_url` | yes | ACME directory URL of the issuing CA |
| `doh.acme.ip` | yes | the IP address the certificate is issued for |
| `doh.acme.http01_listen` | yes | host:port the ACME HTTP-01 challenge responder binds; MUST be reachable from the public Internet as port 80 |
| `doh.acme.initial_delay` | no | a Go duration string (for example `30s`) that delays only the first certificate obtain attempt of the process; absent or empty means no delay |

The `doh.acme.initial_delay` field SHALL be parsed as a Go duration string. When the field is absent or its value is an empty string, the effective delay SHALL be zero. When the value cannot be parsed as a Go duration, configuration loading SHALL fail with an error naming the `initial_delay` field and including the rejected value. When the parsed value is negative, configuration loading SHALL fail with an error naming the `initial_delay` field. Configuration loading SHALL NOT reject a positive value for being large; no upper bound SHALL be enforced.

#### Scenario: DoH server starts on configured address

- **WHEN** ShadowDNS is started with a `--config` file containing a `doh` section whose `listen` is `203.0.113.10:443` and all required `doh.acme.*` fields are present
- **THEN** the DoH server SHALL accept HTTPS connections on `203.0.113.10:443`

#### Scenario: DoH server is not started when the doh section is absent

- **WHEN** ShadowDNS is started with a `--config` file that omits the `doh` section
- **THEN** no DoH HTTPS server SHALL be started, no port SHALL be bound for DoH, and no ACME HTTP-01 listener SHALL be started

#### Scenario: Missing required doh field fails the load

- **WHEN** ShadowDNS is started with a `--config` file whose `doh` section omits the required `acme.ip` field
- **THEN** configuration loading SHALL fail with an error naming the missing `acme.ip` field, and no DoH server SHALL be started

#### Scenario: Optional initial_delay is accepted, defaulted, and validated

- **WHEN** ShadowDNS loads a `--config` file whose `doh.acme` section carries a given `initial_delay` value, or omits the field entirely
- **THEN** loading SHALL either succeed with the effective delay shown below, or fail with an error naming the `initial_delay` field

##### Example: initial_delay parsing and validation

| `initial_delay` value in YAML | Outcome | Notes |
| ----------------------------- | ------- | ----- |
| field absent | load succeeds, effective delay `0s` | preserves pre-existing behavior |
| `""` | load succeeds, effective delay `0s` | empty string is treated as absent |
| `"0s"` | load succeeds, effective delay `0s` | explicit no-delay |
| `"30s"` | load succeeds, effective delay `30s` | typical propagation window |
| `"2m"` | load succeeds, effective delay `2m` | no upper bound is enforced |
| `"-1s"` | load fails naming `initial_delay` | negative value rejected |
| `"30"` | load fails naming `initial_delay` and including `30` | not a Go duration (no unit) |
| `"soon"` | load fails naming `initial_delay` and including `soon` | unparseable |

## ADDED Requirements

### Requirement: Initial ACME certificate issuance can be delayed at startup

When `doh.acme.initial_delay` resolves to a positive duration, the DoH server SHALL wait that duration after the certificate management loop starts and before it makes the first certificate obtain attempt of the process. The DoH HTTPS listener and the ACME HTTP-01 challenge listener SHALL be started and bound exactly as they are when no delay is configured; the delay SHALL NOT postpone, reorder, gate, or otherwise alter listener startup. Listener startup and the delay proceed concurrently: the delay SHALL NOT wait for either listener to finish binding, and neither listener SHALL wait for the delay to elapse, so once bound the HTTP-01 listener serves for the remainder of the delay window.

The delay SHALL apply only to the first obtain attempt of the process. A retry after a failed obtain SHALL use the existing retry interval, and a renewal after a successful obtain SHALL use the existing renewal lead time and minimum renewal interval; neither SHALL be lengthened, shortened, or replaced by the initial delay.

The wait SHALL be cancellable through the same context that drives shutdown. When that context is cancelled while the delay is elapsing, the certificate management loop SHALL return without attempting to obtain a certificate, SHALL NOT record a certificate renewal failure, and SHALL NOT emit an ACME error log for that cancellation.

When the effective delay is positive, the DoH server SHALL emit one informational log entry at the start of the wait stating that initial ACME issuance is being delayed and carrying the configured duration. That log entry SHALL NOT contain any challenge token, key authorization, or account key material. When the effective delay is zero, the server SHALL attempt the first obtain immediately and SHALL NOT emit that log entry.

During the delay, TLS handshake behavior SHALL remain unchanged from the current behavior when no certificate has been obtained yet: a handshake against the DoH HTTPS listener SHALL fail because no certificate is available.

#### Scenario: Zero delay preserves immediate initial issuance

- **WHEN** ShadowDNS starts with a `doh` section whose `doh.acme.initial_delay` is absent or set to `0s`
- **THEN** the certificate management loop SHALL attempt the first certificate obtain immediately, without waiting and without emitting the initial-delay log entry

#### Scenario: Positive delay postpones only the first obtain attempt

- **WHEN** ShadowDNS starts with `doh.acme.initial_delay` set to a positive duration
- **THEN** the first certificate obtain attempt SHALL NOT be made before that duration has elapsed since the certificate management loop started, and an informational log entry naming the configured duration SHALL be emitted at the start of the wait

##### Example: 30s delay with a failing first obtain

- **GIVEN** `doh.acme.initial_delay` is `30s` and the existing failed-obtain retry interval is 10 minutes
- **WHEN** the process starts, the first obtain attempt at T+30s fails, and the second attempt is scheduled
- **THEN** the first obtain attempt occurs at T+30s (not at T+0s), and the second attempt occurs 10 minutes after the first attempt failed (not 30 seconds after it)

#### Scenario: Listeners are unaffected by the delay

- **WHEN** ShadowDNS starts with a positive `doh.acme.initial_delay`, both listeners have finished binding, and the delay has not yet elapsed
- **THEN** the ACME HTTP-01 listener SHALL be serving on `doh.acme.http01_listen`, the DoH HTTPS listener SHALL be accepting connections on `doh.listen`, a TLS handshake against it SHALL fail because no certificate has been obtained yet, and neither bind SHALL have been held back waiting for the delay to elapse

#### Scenario: Cancellation during the delay exits without a spurious failure

- **WHEN** the shutdown context is cancelled while the initial delay is still elapsing
- **THEN** the certificate management loop SHALL return without calling the certificate obtain path, SHALL NOT increment the certificate renewal failure count, and SHALL NOT emit an ACME obtain error log

#### Scenario: Renewal timing after a successful first obtain is unchanged

- **WHEN** a positive `doh.acme.initial_delay` is configured and the first obtain attempt succeeds after the delay
- **THEN** the next renewal SHALL be scheduled from the installed certificate's lifetime using the existing renewal lead time and minimum renewal interval, and the initial delay SHALL NOT be added to that schedule
