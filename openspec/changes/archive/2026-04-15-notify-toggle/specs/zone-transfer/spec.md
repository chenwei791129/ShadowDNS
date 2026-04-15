## MODIFIED Requirements

### Requirement: Send NOTIFY on zone content change

On startup after all zones are loaded, and on every zone reload, the zone-transfer subsystem SHALL send DNS NOTIFY messages to each NS record target of the zone (excluding the zone's own primary, if identifiable via SOA MNAME) **unless NOTIFY is disabled**. NOTIFY SHALL be sent over UDP to port 53; NOTIFY SHALL be retried up to 3 times on failure with exponential backoff (1s, 2s, 4s).

NOTIFY is disabled when EITHER of the following holds:

1. The CLI flag `-no-notify` is explicitly passed to the `shadowdns` process. This takes effect for the process lifetime and is NOT affected by subsequent SIGHUP reloads.
2. The CLI flag is not passed AND `named.conf` contains `options { notify no; }`.

When NOTIFY is disabled, the zone-transfer subsystem SHALL NOT build NOTIFY messages, SHALL NOT spawn NOTIFY goroutines, and SHALL NOT perform any retries for any zone. The default behavior (when neither the flag nor the config directive sets it) SHALL be to send NOTIFY.

#### Scenario: NOTIFY sent to each NS target

- **WHEN** a zone has NS records `ns1.example.com.` and `ns2.example.com.`, neither equals the SOA MNAME, and NOTIFY is enabled
- **THEN** NOTIFY is sent to the resolved IP of each NS target

#### Scenario: NOTIFY retry on failure

- **WHEN** the first NOTIFY send returns no response within 5 seconds and NOTIFY is enabled
- **THEN** the server retries after 1 second, then 2 seconds, then 4 seconds; after three failed attempts it logs an error and gives up

#### Scenario: NOTIFY not sent to SOA MNAME

- **WHEN** the zone has NS records including a target that equals the SOA MNAME and NOTIFY is enabled
- **THEN** NOTIFY is not sent to that target (since it refers to the primary master itself)

#### Scenario: NOTIFY disabled by CLI flag suppresses all sends

- **WHEN** the `shadowdns` process is started with `-no-notify` and zones are loaded at startup
- **THEN** no NOTIFY message is sent for any zone, no NOTIFY goroutine is spawned, and no retry is attempted

#### Scenario: NOTIFY disabled by config suppresses all sends

- **WHEN** `named.conf` contains `options { notify no; };`, the `-no-notify` CLI flag is NOT passed, and zones are loaded at startup
- **THEN** no NOTIFY message is sent for any zone, no NOTIFY goroutine is spawned, and no retry is attempted

#### Scenario: NOTIFY enabled by default when neither flag nor config sets it

- **WHEN** the `shadowdns` process is started without `-no-notify` and `named.conf` contains no `notify` directive in its `options` block
- **THEN** NOTIFY is sent to each applicable NS target as defined by the other scenarios above

#### Scenario: CLI flag overrides config

- **WHEN** the `shadowdns` process is started with `-no-notify` and `named.conf` contains `options { notify yes; };`
- **THEN** no NOTIFY message is sent for any zone

#### Scenario: CLI flag effect persists across SIGHUP reload

- **WHEN** the `shadowdns` process is started with `-no-notify`, zones are initially loaded, the operator later edits `named.conf` to `options { notify yes; };` and sends SIGHUP to the process
- **THEN** after the reload completes, no NOTIFY message is sent for any zone

#### Scenario: Config change takes effect on SIGHUP reload

- **WHEN** the `shadowdns` process was started without `-no-notify`, initially ran with `options { notify yes; };`, the operator later edits `named.conf` to `options { notify no; };` and sends SIGHUP to the process
- **THEN** after the reload completes, no NOTIFY message is sent for any zone
