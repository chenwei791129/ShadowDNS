## MODIFIED Requirements

### Requirement: application/dns-json responses follow the Google Public DNS schema

A successful `application/dns-json` response SHALL be a JSON object containing `Status` (the integer DNS RCODE), `TC`, `RD`, `RA`, `AD`, and `CD` (booleans taken from the response header), `Question` (an array of objects each with a string `name` and an integer `type`), and `Answer` (an array of objects each with a string `name`, an integer `type`, an integer `TTL`, and a string `data`). The `RD` field SHALL be true, reflecting the recursion-desired bit set on the dispatched query. The `CD` field SHALL be false; the `cd` query parameter SHALL NOT set the response Checking-Disabled bit. The `data` field SHALL be the RDATA in DNS presentation format, derived by stripping the record header from the record's presentation form (not by splitting on whitespace, so multi-field RDATA such as SOA and MX is preserved). When the response carries a server-populated EDNS Client Subnet option, the object SHALL additionally include an `edns_client_subnet` string field formatted as `<network>/<source-prefix>/<scope-prefix>`, where the scope-prefix echoes the source prefix the server applied (this authoritative server does not narrow or widen the scope to a geo boundary). The response SHALL carry a `Cache-Control: max-age=N` header where N is bounded by the minimum Answer TTL, identical to the wire-format path. `Question` and `Answer` SHALL encode as JSON arrays including when empty. Every JSON response body SHALL end with exactly one newline. Field ordering, non-semantic escaping, and other whitespace in the JSON body SHALL NOT be constrained; only HTTP metadata and the decoded field names, types, and values are normative.

#### Scenario: TXT answer serialized to JSON

- **WHEN** a JSON request resolves a name that has a single TXT record and ECS is not in effect
- **THEN** the response SHALL be a JSON object whose `Answer` contains one object with the TXT type code and the RDATA in presentation format, whose `RD` is true and `CD` is false, that carries a `Cache-Control` header bounded by the answer TTL, and that SHALL NOT include an `edns_client_subnet` field
- **THEN** the response body SHALL end with exactly one newline

##### Example: Single TXT answer decoded values

- **GIVEN** `_ephemeral-doh-check.example.com.` holds one TXT value `hello` with TTL 120
- **WHEN** a JSON request specifies `name=_ephemeral-doh-check.example.com&type=TXT`
- **THEN** the decoded response SHALL be equivalent to `{"Status":0,"TC":false,"RD":true,"RA":false,"AD":false,"CD":false,"Question":[{"name":"_ephemeral-doh-check.example.com.","type":16}],"Answer":[{"name":"_ephemeral-doh-check.example.com.","type":16,"TTL":120,"data":"\"hello\""}]}` with field order and escaping not significant, and the response SHALL carry `Cache-Control: max-age=120`

#### Scenario: Out-of-zone query conveys REFUSED in Status

- **WHEN** a JSON request queries a name outside any zone served by ShadowDNS
- **THEN** the response SHALL be HTTP 200 with `Status` 5 (REFUSED) and an empty `Answer` array

#### Scenario: Empty sections remain arrays

- **WHEN** a valid JSON query produces a DNS response with no questions or no answers in a section
- **THEN** the corresponding `Question` or `Answer` member SHALL decode as an empty JSON array rather than `null`

#### Scenario: Non-semantic encoding differences preserve the contract

- **WHEN** a JSON answer contains string data with HTML-sensitive characters such as `<`, `>`, or `&`
- **THEN** clients SHALL observe the same decoded string value regardless of whether those characters are emitted literally or with Unicode escapes
