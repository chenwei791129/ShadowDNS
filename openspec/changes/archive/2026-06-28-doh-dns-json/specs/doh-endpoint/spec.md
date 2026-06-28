## ADDED Requirements

### Requirement: DoH endpoint serves application/dns-json over GET via Accept negotiation

The DoH endpoint SHALL serve the Google Public DNS-compatible `application/dns-json` format on the `/dns-query` path for `GET` requests that do NOT carry a `?dns=` parameter and whose `Accept` header lists the `application/dns-json` media type. The `?dns=` parameter SHALL take precedence: a `GET` request carrying `?dns=` SHALL be handled as RFC 8484 wire-format regardless of its `Accept` header, so a wire-format query is never misrouted to the JSON parser. A `GET` request with neither `?dns=` nor an `Accept` header listing `application/dns-json` SHALL retain the existing wire-format behavior (missing `?dns=` rejected per the existing rules). `POST` requests SHALL always be handled as RFC 8484 wire-format regardless of the `Accept` header; the Accept-negotiation branch SHALL exist only on the GET path and SHALL NOT be added to the POST handler. A response produced for the JSON format SHALL carry the `Content-Type: application/dns-json` header.

#### Scenario: Accept application/dns-json selects the JSON format

- **WHEN** a GET request arrives at `/dns-query` with `Accept: application/dns-json`, no `?dns=` parameter, and query string `name=example.com&type=A`
- **THEN** the response SHALL be a JSON document carrying `Content-Type: application/dns-json`

#### Scenario: dns parameter takes precedence over a JSON Accept header

- **WHEN** a GET request arrives at `/dns-query` carrying both a `?dns=` base64url parameter and an `Accept` header that lists `application/dns-json`
- **THEN** the request SHALL be handled as RFC 8484 wire-format with `Content-Type: application/dns-message`, and the `?dns=` parameter SHALL NOT be ignored in favor of the JSON path

#### Scenario: Absent JSON Accept retains wire-format behavior

- **WHEN** a GET request arrives at `/dns-query` with a `?dns=` base64url parameter and an `Accept` header that does not list `application/dns-json`
- **THEN** the response SHALL be RFC 8484 wire-format with `Content-Type: application/dns-message`, identical to the behavior before this change

#### Scenario: POST is always wire-format

- **WHEN** a POST request arrives at `/dns-query` with `Accept: application/dns-json` and an `application/dns-message` body
- **THEN** the request SHALL be handled as RFC 8484 wire-format and SHALL NOT be parsed as JSON

### Requirement: application/dns-json queries are parsed from name and type parameters

For an `application/dns-json` GET request, the endpoint SHALL read the query name from the `name` parameter and the query type from the `type` parameter. The `name` parameter SHALL be required and non-empty; a missing or empty `name` SHALL be rejected per the error-handling requirement. The name SHALL be normalized to a trailing-dot FQDN, but its on-wire letter case SHALL be preserved (not lowercased) so the JSON `Question` name and any owner echo match what wire-format DoH returns for the same name. The `type` parameter SHALL be optional and SHALL default to `A` when absent. The `type` parameter SHALL accept DNS type mnemonics case-insensitively (for example `TXT`, `txt`, and `Txt` all resolve to TXT) and SHALL accept numeric DNS type codes in the range 0 to 65535 inclusive. The parsed name and type SHALL form a single-question DNS query, with the recursion-desired bit set on the dispatched query, dispatched through the same authoritative query path used by the wire-format, UDP, and TCP transports, so view selection, the ephemeral overlay, rate limiting, and response assembly behave identically across transports.

#### Scenario: type defaults to A when omitted

- **WHEN** a JSON request specifies `name=example.com` with no `type` parameter
- **THEN** the query SHALL be resolved as an `A` query for `example.com`

#### Scenario: type accepts mnemonic and numeric forms case-insensitively

- **WHEN** a JSON request specifies a `type` parameter as a mnemonic in any letter case or as a numeric code in range
- **THEN** the endpoint SHALL resolve the query for the corresponding DNS type

##### Example: type parameter parsing

| `type` value | Resolved DNS type |
| ------------ | ----------------- |
| `A`          | A (1)             |
| `1`          | A (1)             |
| `TXT`        | TXT (16)          |
| `txt`        | TXT (16)          |
| `16`         | TXT (16)          |
| `AAAA`       | AAAA (28)         |

#### Scenario: on-wire name case is preserved in the response

- **WHEN** a JSON request specifies `name=ExAmple.COM&type=A`
- **THEN** the JSON `Question` name and any echoed owner name SHALL preserve the submitted case `ExAmple.COM.`, matching wire-format DoH for the same name

### Requirement: application/dns-json refuses zone-transfer query types

The `application/dns-json` path SHALL refuse `AXFR` and `IXFR` query types with a REFUSED result, matching the wire-format DoH path, because a zone transfer is a multi-message stream that has no representation in a single JSON response and the synthetic single-shot writer would otherwise capture only the last envelope. The endpoint SHALL NOT dispatch a zone-transfer type through the streaming transfer path. The refusal SHALL be returned as HTTP 200 with `Status` 5 (REFUSED) and an empty `Answer`, identical in effect to the wire-format path's refusal.

#### Scenario: AXFR over JSON is refused

- **WHEN** a JSON request specifies `name=example.com&type=AXFR`
- **THEN** the response SHALL be HTTP 200 with `Status` 5 (REFUSED) and an empty `Answer`, and no zone-transfer stream SHALL be produced

### Requirement: application/dns-json edns_client_subnet parameter injects an EDNS Client Subnet option

When an `application/dns-json` GET request includes the `edns_client_subnet` parameter, the endpoint SHALL parse it as an `<ip>/<prefix>` client subnet and attach a corresponding EDNS0 Client Subnet option to the dispatched query, so that the existing EDNS Client Subnet handling performs view and geo selection identically to a wire-format query carrying that option. The injected option SHALL have SCOPE PREFIX-LENGTH 0 and SHALL have all address bits beyond the source prefix length zeroed (masked), because the downstream ECS classifier rejects any option whose host bits are set as malformed. When the prefix is omitted, the endpoint SHALL default to `/24` for an IPv4 address and `/56` for an IPv6 address. When ECS handling is disabled (the `--ecs-enable` flag is false), the injected option SHALL be ignored by the query path and the response SHALL NOT include an `edns_client_subnet` field, identical to a wire-format query carrying ECS while ECS is disabled.

#### Scenario: edns_client_subnet drives geo view selection

- **WHEN** ECS handling is enabled and a JSON request specifies `name=example.com&type=A&edns_client_subnet=198.51.100.0/24`
- **THEN** the query SHALL be resolved as if it carried an EDNS Client Subnet option for `198.51.100.0/24`, and the JSON response SHALL include the applied scope in an `edns_client_subnet` field

#### Scenario: host bits beyond the prefix are masked rather than rejected

- **WHEN** ECS handling is enabled and a JSON request specifies `edns_client_subnet=198.51.100.5/24` (host bits set beyond the /24 prefix)
- **THEN** the endpoint SHALL zero the host bits before building the option so the query resolves normally, and SHALL NOT produce a FORMERR result

#### Scenario: omitted prefix defaults by address family

- **WHEN** a JSON request includes an `edns_client_subnet` value with no `/prefix`
- **THEN** the endpoint SHALL apply the family default prefix length

##### Example: default prefix by family

| `edns_client_subnet` value | Applied source prefix |
| -------------------------- | --------------------- |
| `198.51.100.0`             | /24                   |
| `198.51.100.0/16`          | /16                   |
| `2001:db8::`               | /56                   |

#### Scenario: ECS disabled ignores the parameter

- **WHEN** the `--ecs-enable` flag is false and a JSON request includes `edns_client_subnet=198.51.100.0/24`
- **THEN** the subnet SHALL NOT affect resolution and the response SHALL NOT include an `edns_client_subnet` field

### Requirement: application/dns-json responses follow the Google Public DNS schema

A successful `application/dns-json` response SHALL be a JSON object containing `Status` (the integer DNS RCODE), `TC`, `RD`, `RA`, `AD`, and `CD` (booleans taken from the response header), `Question` (an array of objects each with a string `name` and an integer `type`), and `Answer` (an array of objects each with a string `name`, an integer `type`, an integer `TTL`, and a string `data`). The `RD` field SHALL be true, reflecting the recursion-desired bit set on the dispatched query. The `CD` field SHALL be false; the `cd` query parameter SHALL NOT set the response Checking-Disabled bit. The `data` field SHALL be the RDATA in DNS presentation format, derived by stripping the record header from the record's presentation form (not by splitting on whitespace, so multi-field RDATA such as SOA and MX is preserved). When the response carries a server-populated EDNS Client Subnet option, the object SHALL additionally include an `edns_client_subnet` string field formatted as `<network>/<source-prefix>/<scope-prefix>`, where the scope-prefix echoes the source prefix the server applied (this authoritative server does not narrow or widen the scope to a geo boundary). The response SHALL carry a `Cache-Control: max-age=N` header where N is bounded by the minimum Answer TTL, identical to the wire-format path. Field ordering and whitespace in the JSON body SHALL NOT be constrained; only the field names, types, and values are normative.

#### Scenario: TXT answer serialized to JSON

- **WHEN** a JSON request resolves a name that has a single TXT record and ECS is not in effect
- **THEN** the response SHALL be a JSON object whose `Answer` contains one object with the TXT type code and the RDATA in presentation format, whose `RD` is true and `CD` is false, that carries a `Cache-Control` header bounded by the answer TTL, and that SHALL NOT include an `edns_client_subnet` field

##### Example: single TXT answer (field values, any field order)

- **GIVEN** `_ephemeral-doh-check.example.com.` holds one TXT value `hello` with TTL 120
- **WHEN** a JSON request specifies `name=_ephemeral-doh-check.example.com&type=TXT`
- **THEN** the response body SHALL be a JSON object equivalent to `{"Status":0,"TC":false,"RD":true,"RA":false,"AD":false,"CD":false,"Question":[{"name":"_ephemeral-doh-check.example.com.","type":16}],"Answer":[{"name":"_ephemeral-doh-check.example.com.","type":16,"TTL":120,"data":"\"hello\""}]}` with field order not significant, and the response SHALL carry `Cache-Control: max-age=120`

#### Scenario: out-of-zone query conveys REFUSED in Status

- **WHEN** a JSON request queries a name outside any zone served by ShadowDNS
- **THEN** the response SHALL be HTTP 200 with `Status` 5 (REFUSED) and an empty `Answer` array

### Requirement: application/dns-json request errors return HTTP 400, internal failures return HTTP 500, and DNS-level results return HTTP 200

For an `application/dns-json` GET request, the endpoint SHALL return HTTP 400 when the request is malformed: the `name` parameter is missing or empty, the `type` parameter cannot be parsed as a mnemonic or as a numeric code in range 0 to 65535, or the `edns_client_subnet` parameter cannot be parsed as a client subnet. When the request is well-formed and dispatched, the endpoint SHALL return HTTP 200 regardless of the DNS RCODE; DNS-level outcomes such as REFUSED, NXDOMAIN, or an empty answer SHALL be conveyed in the JSON `Status` and `Answer` fields rather than via an HTTP error status. When the dispatched query produces no captured response message (an internal failure), the endpoint SHALL return HTTP 500, matching the wire-format path's empty-capture guard, rather than emitting a misleading empty success object.

#### Scenario: missing name returns 400

- **WHEN** a JSON GET request omits the `name` parameter or sends it empty
- **THEN** the endpoint SHALL return HTTP 400 and SHALL NOT dispatch a query

#### Scenario: unparseable type returns 400

- **WHEN** a JSON GET request specifies a `type` value that is neither a known mnemonic nor a numeric code in range 0 to 65535 (for example `65537` or `notatype`)
- **THEN** the endpoint SHALL return HTTP 400 and SHALL NOT dispatch a query

#### Scenario: DNS-level REFUSED returns HTTP 200

- **WHEN** a well-formed JSON request resolves to a REFUSED result
- **THEN** the endpoint SHALL return HTTP 200 with `Status` 5 in the JSON body

#### Scenario: empty captured response returns 500

- **WHEN** a well-formed JSON request is dispatched but the query path captures no response message
- **THEN** the endpoint SHALL return HTTP 500 rather than a JSON object with an empty Answer and Status 0

### Requirement: application/dns-json path tolerates cd and ignores do and ct

The `application/dns-json` path SHALL accept and ignore the `cd` parameter without returning an error and without setting the response Checking-Disabled bit, because ShadowDNS is non-recursive and performs no DNSSEC validation to disable. The `do` and `ct` parameters SHALL NOT be honored, and their presence SHALL NOT cause an error. No DNSSEC records SHALL be added to the response on account of these parameters.

#### Scenario: cd is accepted and ignored

- **WHEN** a well-formed JSON request includes `cd=1`
- **THEN** the request SHALL be resolved normally, SHALL NOT return an error attributable to the `cd` parameter, and the JSON `CD` field SHALL be false
