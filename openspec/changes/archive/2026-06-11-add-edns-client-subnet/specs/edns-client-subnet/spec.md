## ADDED Requirements

### Requirement: ECS support is disabled by default and gated by the --ecs-enable flag

The dns-server SHALL register a boolean CLI flag named `--ecs-enable` whose default value is `false`. When the flag is `false`, the server SHALL ignore any ECS (EDNS Client Subnet, RFC 7871) option that reaches the query handler for all purposes — view selection, validation, and response assembly — and SHALL NOT include an ECS option in any response (RFC 7871 requirement for servers without ECS enabled). With the flag disabled, response bytes and view selection SHALL be identical to the behavior before this capability existed. At startup the server SHALL emit an info-level log entry stating whether ECS processing is enabled or disabled, in both flag states, and this log entry SHALL also be emitted on `--dry-run` runs.

#### Scenario: Default-off ignores ECS in view selection and response

- **WHEN** the server runs without `--ecs-enable` and receives a query carrying a well-formed ECS option whose address maps to a different country than the source IP
- **THEN** the view is selected using the source IP only AND the response contains no ECS option

#### Scenario: Handler-detectable malformed ECS is silently ignored when disabled

- **WHEN** the server runs without `--ecs-enable` and receives a query carrying an ECS option whose SCOPE PREFIX-LENGTH is non-zero (a form that reaches the handler; see the malformed-ECS requirement)
- **THEN** the query is answered normally (no FORMERR caused by the ECS option) AND the response contains no ECS option

#### Scenario: Startup log states ECS state in both flag states

- **WHEN** the server starts with `--ecs-enable`
- **THEN** an info-level log entry records that ECS processing is enabled

#### Scenario: Startup log states ECS state when disabled

- **WHEN** the server starts without `--ecs-enable` (including `--dry-run` runs)
- **THEN** an info-level log entry records that ECS processing is disabled

### Requirement: Valid ECS address drives geo rule evaluation when enabled

When `--ecs-enable` is set and a query carries a valid ECS option with SOURCE PREFIX-LENGTH greater than 0, the dns-server SHALL use the ECS ADDRESS as the geo lookup address for view selection (country and ASN rules), while IP and CIDR rules SHALL continue to be evaluated against the real client source IP. Queries without an ECS option SHALL be processed exactly as if ECS were disabled. When the OPT record contains more than one ECS option, the server SHALL process only the first one and ignore the rest. ECS validation runs before the AXFR/IXFR zone-transfer dispatch: a transfer query carrying a handler-detectable malformed ECS option SHALL receive FORMERR like any other query, but the zone-transfer path SHALL NOT use ECS for its view resolution and transfer response streams are not required to echo ECS. When the geo databases contain no entry for the ECS address, geo rules SHALL evaluate to no-match; the server SHALL NOT fall back to re-evaluating geo rules with the source IP.

#### Scenario: ECS address overrides source IP for geo rules

- **WHEN** views declare `view-asia` (rule: country TW) before `view-global` (rule: any), the client source IP geolocates to country US, and the query carries ECS `203.0.113.0/24` which geolocates to country TW
- **THEN** the query is answered from `view-asia`

#### Scenario: Forged ECS cannot select an ACL-protected view

- **WHEN** views declare `view-internal` (rule: CIDR `192.0.2.0/24`) before `view-global` (rule: any), the client source IP is `203.0.113.7`, and the query carries ECS `192.0.2.5/32`
- **THEN** the query is answered from `view-global` because CIDR rules use the source IP, never the ECS address

#### Scenario: ECS address absent from geo databases is a geo no-match

- **WHEN** the query carries a valid ECS option whose address has no entry in the country mmdb
- **THEN** country rules evaluate to no-match AND matching proceeds to subsequent rules using the same addresses (no source-IP geo fallback)

##### Example: address selection per rule type

| Rule type | Address evaluated |
| --------- | ----------------- |
| country   | ECS address `203.0.113.0` |
| ASN       | ECS address `203.0.113.0` |
| IP        | source IP `198.51.100.1` |
| CIDR      | source IP `198.51.100.1` |
| any       | matches regardless of address |

### Requirement: Responses echo the ECS option with a scope equal to the source prefix length

When `--ecs-enable` is set and a query carries a valid ECS option with SOURCE PREFIX-LENGTH greater than 0, the response OPT record SHALL contain exactly one ECS option whose FAMILY, SOURCE PREFIX-LENGTH, and ADDRESS are identical to those in the query (RFC 7871 echo requirement) and whose SCOPE PREFIX-LENGTH equals the query's SOURCE PREFIX-LENGTH. This echo SHALL apply to every response assembled by the standard query-answer path at or after the ECS processing point — including NOERROR, NXDOMAIN, and REFUSED responses for clients matching no view. Responses produced before the ECS processing point are exempt from the echo: NOTIMP for unsupported opcodes, FORMERR for malformed question counts, BADVERS for unsupported EDNS versions, and FORMERR for malformed COOKIE options. Zone-transfer response streams and panic-recovery SERVFAIL responses are likewise exempt (matching the existing COOKIE precedent, whose echo is also dropped on the panic-recovery path). Responses to queries that carry no ECS option SHALL NOT contain an ECS option.

#### Scenario: IPv4 ECS is echoed with matching scope

- **WHEN** the query carries ECS FAMILY 1, SOURCE PREFIX-LENGTH 24, ADDRESS `203.0.113.0`
- **THEN** the response ECS option carries FAMILY 1, SOURCE PREFIX-LENGTH 24, ADDRESS `203.0.113.0`, SCOPE PREFIX-LENGTH 24

#### Scenario: IPv6 ECS is echoed with matching scope

- **WHEN** the query carries ECS FAMILY 2, SOURCE PREFIX-LENGTH 56, ADDRESS `2001:db8:ab::`
- **THEN** the response ECS option carries FAMILY 2, SOURCE PREFIX-LENGTH 56, ADDRESS `2001:db8:ab::`, SCOPE PREFIX-LENGTH 56

#### Scenario: No ECS in query means no ECS in response

- **WHEN** `--ecs-enable` is set and the query carries an OPT record without an ECS option
- **THEN** the response contains no ECS option

### Requirement: Opt-out ECS (source prefix length 0) is honored

When `--ecs-enable` is set and a query carries an otherwise well-formed ECS option with SOURCE PREFIX-LENGTH 0 (the RFC 7871 client opt-out), the dns-server SHALL NOT use ECS for view selection (all rules evaluate against the source IP) and the response SHALL echo the ECS option — preserving the query's FAMILY — with SCOPE PREFIX-LENGTH 0. Opt-out SHALL be honored for FAMILY 0, 1, and 2 alike: FAMILY 0 with SOURCE PREFIX-LENGTH 0 is the form `dig +subnet=0` sends and the DNS library delivers it to the handler, so it MUST be treated as opt-out, not as malformed.

The malformed-ECS checks take precedence over opt-out classification: an option with SOURCE PREFIX-LENGTH 0 whose SCOPE PREFIX-LENGTH is non-zero or whose ADDRESS bits are non-zero (with prefix length 0, every address bit is beyond the prefix) is malformed, not opt-out (see the malformed-ECS requirement). This case is wire-reachable for FAMILY 1 and 2 because the library does not check the option's address octet count against the prefix length; FAMILY 0 is unaffected because the library zeroes the address during unpacking.

#### Scenario: Opt-out query is answered by source IP with scope 0

- **WHEN** the query carries ECS FAMILY 1, SOURCE PREFIX-LENGTH 0, empty ADDRESS
- **THEN** the view is selected using the source IP AND the response ECS option carries SOURCE PREFIX-LENGTH 0 and SCOPE PREFIX-LENGTH 0

#### Scenario: FAMILY 0 opt-out probe is honored

- **WHEN** the query carries ECS FAMILY 0, SOURCE PREFIX-LENGTH 0 (the `dig +subnet=0` form)
- **THEN** the view is selected using the source IP AND the response ECS option carries FAMILY 0, SOURCE PREFIX-LENGTH 0, SCOPE PREFIX-LENGTH 0 (no FORMERR)

#### Scenario: Source prefix length 0 with non-zero address bits is malformed, not opt-out

- **WHEN** `--ecs-enable` is set and the query carries ECS FAMILY 1, SOURCE PREFIX-LENGTH 0, ADDRESS `203.0.113.9` (non-zero address bits despite prefix length 0)
- **THEN** the server responds FORMERR AND the response carries an OPT record without an ECS option

### Requirement: Malformed ECS options are rejected with FORMERR when enabled

ECS wire-format violations are rejected in two layers, and the spec assigns each violation to exactly one layer:

**Library layer (pre-handler, independent of `--ecs-enable`)**: the DNS message library rejects, at message-unpack time, an ECS option whose FAMILY is not 0, 1, or 2; whose FAMILY is 0 with a non-zero SOURCE PREFIX-LENGTH; or whose SOURCE PREFIX-LENGTH or SCOPE PREFIX-LENGTH exceeds the family maximum (32 for FAMILY 1, 128 for FAMILY 2). Such queries never reach the query handler; the DNS server replies FORMERR with all sections cleared (no OPT record). This paragraph is descriptive (current library behavior, verified against the pinned dependency version), not load-bearing: the handler-layer classifier SHALL be a total function that classifies every representable ECS option, treating forms outside the cases enumerated below (e.g. an unexpected FAMILY value, an out-of-range prefix length) as malformed by default, so that correctness does not depend on library invariants surviving a dependency upgrade. Per the project testing principle, library-layer rejections are not covered by this project's tests.

**Handler layer (only when `--ecs-enable` is set)**: the dns-server SHALL reject a query whose handler-reachable ECS option is malformed by responding FORMERR. The handler-layer FORMERR response SHALL carry an OPT record but SHALL NOT carry an ECS option. These malformed checks take precedence over opt-out classification (a SOURCE PREFIX-LENGTH 0 option failing either check is malformed, not opt-out). A handler-reachable ECS option SHALL be considered malformed when either of the following holds:

- SCOPE PREFIX-LENGTH in the query is non-zero (RFC 7871 mandates 0 in queries)
- address bits beyond SOURCE PREFIX-LENGTH are non-zero (the library zero-pads short addresses and truncates long ones, so octet-count mismatches are not observable in the handler; only non-zero trailing bits are; with SOURCE PREFIX-LENGTH 0 every address bit is beyond the prefix)

#### Scenario: Unknown family is rejected before the handler regardless of flag state

- **WHEN** a raw wire-format query carrying an ECS option with FAMILY 3 arrives, with or without `--ecs-enable`
- **THEN** the server responds FORMERR with no OPT record AND the query handler is never invoked

#### Scenario: Non-zero query scope is rejected by the handler

- **WHEN** `--ecs-enable` is set and the query carries an ECS option with FAMILY 1, SOURCE PREFIX-LENGTH 24, SCOPE PREFIX-LENGTH 24
- **THEN** the server responds FORMERR AND the response carries an OPT record without an ECS option

#### Scenario: Non-zero address bits beyond the source prefix are rejected by the handler

- **WHEN** `--ecs-enable` is set and the query carries an ECS option with FAMILY 1, SOURCE PREFIX-LENGTH 24, ADDRESS `203.0.113.9` (non-zero bits in the fourth octet, beyond /24)
- **THEN** the server responds FORMERR AND the response carries an OPT record without an ECS option

##### Example: validation matrix

| FAMILY | SOURCE PREFIX-LENGTH | query SCOPE | trailing bits | Verdict | Rejected by |
| ------ | -------------------- | ----------- | ------------- | ------- | ----------- |
| 1      | 24                   | 0           | zero          | valid   | — |
| 2      | 56                   | 0           | zero          | valid   | — |
| 1      | 0                    | 0           | zero          | opt-out | — |
| 0      | 0                    | 0           | zero (library zeroes the address) | opt-out | — |
| 3      | 24                   | 0           | —             | FORMERR (no OPT) | library unpack |
| 0      | 8                    | 0           | —             | FORMERR (no OPT) | library unpack |
| 1      | 33                   | 0           | —             | FORMERR (no OPT) | library unpack |
| 2      | 129                  | 0           | —             | FORMERR (no OPT) | library unpack |
| 1      | 24                   | 24          | zero          | FORMERR (OPT, no ECS) | handler |
| 1      | 24                   | 0           | non-zero      | FORMERR (OPT, no ECS) | handler |
| 1      | 0                    | 0           | non-zero      | FORMERR (OPT, no ECS) | handler (malformed beats opt-out) |
| 1      | 0                    | 24          | zero          | FORMERR (OPT, no ECS) | handler (malformed beats opt-out) |
