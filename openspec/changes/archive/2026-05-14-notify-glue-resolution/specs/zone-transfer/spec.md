## MODIFIED Requirements

### Requirement: Send NOTIFY on zone content change

On startup after all zones are loaded, and on every zone reload, the zone-transfer subsystem SHALL send DNS NOTIFY messages to each NS record target of the zone (excluding the zone's own primary, if identifiable via SOA MNAME). NOTIFY SHALL be sent over UDP to port 53; NOTIFY SHALL be retried up to 3 times on failure with exponential backoff (1s, 2s, 4s).

The target IP address for each NOTIFY send SHALL be resolved **exclusively from in-zone glue records** — that is, from the A and AAAA records for the NS target name present in the same `*zone.Zone` instance that declares the NS record. The zone-transfer subsystem SHALL NOT invoke the operating system resolver, SHALL NOT perform recursive DNS queries, and SHALL NOT consult other loaded zones when resolving NS target names.

When an NS target has **multiple** in-zone glue IPs (e.g., one A and one AAAA record, or multiple A records), NOTIFY SHALL be sent to each IP independently; each `(zone, NS-hostname, IP)` tuple SHALL be treated as its own NOTIFY send subject to the retry and backoff policy above.

When an NS target has **no** in-zone glue (the target name has no A or AAAA record within the same zone), the zone-transfer subsystem SHALL skip that target: it SHALL NOT build a NOTIFY message, SHALL NOT spawn a send goroutine, and SHALL NOT fall back to any other resolution mechanism. The skip SHALL be recorded in the logs at debug severity with a `source` field whose value is `"skipped-no-glue"`.

Every NOTIFY log record (whether for an attempt, retry, or final failure) SHALL include a `source` field whose value is `"glue"` when the destination IP originated from an in-zone glue record.

Cross-view deduplication of NOTIFY sends SHALL be keyed by the tuple `(zone-origin, NS-hostname, IP)`. A given tuple SHALL result in at most one NOTIFY send sequence (including retries) per startup or reload event, even when the same zone appears in multiple views.

#### Scenario: NOTIFY sent to each in-zone glue IP of an NS target

- **WHEN** a zone `example.com.` has NS record `ns2.example.com.` with in-zone A record `ns2.example.com. A 10.0.0.2` and `ns2.example.com.` does not equal the SOA MNAME
- **THEN** NOTIFY is sent to `10.0.0.2:53` without invoking the operating system resolver

#### Scenario: NOTIFY sent to every glue IP when multiple exist

- **WHEN** a zone has NS record `ns21.example.com.` with in-zone records `ns21.example.com. A 10.0.0.21` and `ns21.example.com. AAAA 2001:db8::21`
- **THEN** one NOTIFY send sequence targets `10.0.0.21:53` and a separate NOTIFY send sequence targets `[2001:db8::21]:53`

#### Scenario: NS target without in-zone glue is skipped

- **WHEN** a zone `example.com.` has NS record `ns.other.tld.` and no A or AAAA record for `ns.other.tld.` exists within the `example.com.` zone data
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
