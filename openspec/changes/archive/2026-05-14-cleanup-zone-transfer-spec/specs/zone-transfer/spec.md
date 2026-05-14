## MODIFIED Requirements

### Requirement: Send NOTIFY on zone content change

On startup after all zones are loaded, and on every zone reload, the zone-transfer subsystem SHALL send DNS NOTIFY messages to each NS record target of the zone (excluding the zone's own primary, if identifiable via SOA MNAME) **unless NOTIFY is disabled**. NOTIFY SHALL be sent over UDP to port 53; NOTIFY SHALL be retried up to 3 times on failure with exponential backoff (1s, 2s, 4s).

NOTIFY is disabled when EITHER of the following holds:

1. The CLI flag `--no-notify` is explicitly passed to the `shadowdns` process. This takes effect for the process lifetime and SHALL NOT be affected by subsequent SIGHUP reloads.
2. The CLI flag is not passed AND `named.conf` contains `options { notify no; }`.

When NOTIFY is disabled, the zone-transfer subsystem SHALL NOT build NOTIFY messages, SHALL NOT spawn NOTIFY goroutines, and SHALL NOT perform any retries for any zone. The default behavior (when neither the flag nor the config directive sets it) SHALL be to send NOTIFY.

When NOTIFY is enabled, the target IP address for each NOTIFY send SHALL be resolved **exclusively from in-zone glue records** — that is, from the A and AAAA records for the NS target name present in the same `*zone.Zone` instance that declares the NS record. The zone-transfer subsystem SHALL NOT invoke the operating system resolver, SHALL NOT perform recursive DNS queries, and SHALL NOT consult other loaded zones when resolving NS target names.

When an NS target has **multiple** in-zone glue IPs (e.g., one A and one AAAA record, or multiple A records), NOTIFY SHALL be sent to each IP independently; each `(zone, NS-hostname, IP)` tuple SHALL be treated as its own NOTIFY send subject to the retry and backoff policy above.

When an NS target has **no** in-zone glue (the target name has no A or AAAA record within the same zone), the zone-transfer subsystem SHALL skip that target: it SHALL NOT build a NOTIFY message, SHALL NOT spawn a send goroutine, and SHALL NOT fall back to any other resolution mechanism. The skip SHALL be recorded in the logs at debug severity with a `source` field whose value is `"skipped-no-glue"`.

Every NOTIFY log record (whether for an attempt, retry, or final failure) SHALL include a `source` field whose value is `"glue"` when the destination IP originated from an in-zone glue record.

Cross-view deduplication of NOTIFY sends SHALL be keyed by the tuple `(zone-origin, NS-hostname, IP)`. A given tuple SHALL result in at most one NOTIFY send sequence (including retries) per startup or reload event, even when the same zone appears in multiple views.

#### Scenario: NOTIFY sent to each in-zone glue IP of an NS target

- **WHEN** a zone `example.com.` has NS record `ns2.example.com.` with in-zone A record `ns2.example.com. A 10.0.0.2` and `ns2.example.com.` does not equal the SOA MNAME and NOTIFY is enabled
- **THEN** NOTIFY is sent to `10.0.0.2:53` without invoking the operating system resolver

#### Scenario: NOTIFY sent to every glue IP when multiple exist

- **WHEN** a zone has NS record `ns21.example.com.` with in-zone records `ns21.example.com. A 10.0.0.21` and `ns21.example.com. AAAA 2001:db8::21` and NOTIFY is enabled
- **THEN** one NOTIFY send sequence targets `10.0.0.21:53` and a separate NOTIFY send sequence targets `[2001:db8::21]:53`

#### Scenario: NS target without in-zone glue is skipped

- **WHEN** a zone `example.com.` has NS record `ns.other.test.` and no A or AAAA record for `ns.other.test.` exists within the `example.com.` zone data and NOTIFY is enabled
- **THEN** no NOTIFY message is built, no goroutine is spawned, and no operating system resolution is attempted for that target; a log record at debug severity is emitted with field `source="skipped-no-glue"` identifying the zone and NS hostname

#### Scenario: NOTIFY retry on failure

- **WHEN** the first NOTIFY send to a resolved glue IP returns no response within 5 seconds
- **THEN** the server retries after 1 second, then 2 seconds, then 4 seconds; after three failed attempts it logs an error with field `source="glue"` and gives up

#### Scenario: NOTIFY not sent to SOA MNAME

- **WHEN** the zone has NS records including a target that equals the SOA MNAME
- **THEN** NOTIFY is not sent to that target, regardless of whether in-zone glue for the MNAME exists

#### Scenario: Cross-view deduplication by zone-host-IP tuple

- **WHEN** the same zone `example.com.` is loaded in two views and its NS record `ns2.example.com.` resolves via in-zone glue to the same IP `10.0.0.2` in both views
- **THEN** exactly one NOTIFY send sequence targeting `10.0.0.2:53` is executed for that zone during startup

#### Scenario: NOTIFY disabled by CLI flag suppresses all sends

- **WHEN** `shadowdns` is started with `--no-notify` and zones are loaded successfully
- **THEN** no NOTIFY messages are sent and no NOTIFY goroutines are spawned for any zone

#### Scenario: NOTIFY disabled by config suppresses all sends

- **WHEN** `--no-notify` is NOT passed and `named.conf` contains `options { notify no; };` and zones are loaded successfully
- **THEN** no NOTIFY messages are sent and no NOTIFY goroutines are spawned for any zone

#### Scenario: NOTIFY enabled by default when neither flag nor config sets it

- **WHEN** `--no-notify` is NOT passed and `named.conf` contains no `notify` directive (or `notify yes;`) and zones are loaded successfully
- **THEN** NOTIFY is sent to each in-zone glue IP of every non-MNAME NS target per the rules above

#### Scenario: CLI flag overrides config

- **WHEN** `shadowdns` is started with `--no-notify` and `named.conf` contains `options { notify yes; };`
- **THEN** no NOTIFY messages are sent (CLI flag wins over config)

#### Scenario: CLI flag effect persists across SIGHUP reload

- **WHEN** `shadowdns` is started with `--no-notify` and later receives SIGHUP triggering a zone reload
- **THEN** NOTIFY remains suppressed after the reload regardless of the post-reload `notify` directive value in `named.conf`

#### Scenario: Config change takes effect on SIGHUP reload

- **WHEN** `--no-notify` is NOT passed and `named.conf` previously contained `options { notify no; };` is edited to `options { notify yes; };` and SIGHUP is delivered
- **THEN** after the reload completes, the next NOTIFY-triggering event sends NOTIFY per the enabled-default rules
