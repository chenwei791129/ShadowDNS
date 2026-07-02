## MODIFIED Requirements

### Requirement: application/dns-json queries are parsed from name and type parameters

For an `application/dns-json` GET request, the endpoint SHALL read the query name from the `name` parameter and the query type from the `type` parameter. The `name` parameter SHALL be required and non-empty; a missing or empty `name` SHALL be rejected per the error-handling requirement. The name SHALL be normalized to a trailing-dot FQDN, but its on-wire letter case SHALL be preserved (not lowercased) so the JSON `Question` name and any owner echo match what wire-format DoH returns for the same name. The endpoint SHALL further canonicalize the name to the same on-wire presentation form the wire-format path produces — control bytes (any byte less than 0x20 or equal to 0x7f) SHALL be escaped in the RFC 1035 `\DDD` decimal form and master-file special characters SHALL be escaped exactly as `miekg/dns` renders them when unpacking a wire-format name — so that a `name` carrying raw control bytes cannot inject unescaped bytes into any downstream consumer of the question name (for example the query log). This canonicalization SHALL be implemented by round-tripping the name through the on-wire encoding (pack then unpack); a name that cannot be encoded as a valid on-wire name during this step SHALL be rejected per the error-handling requirement (HTTP 400) rather than dispatched. The `type` parameter SHALL be optional and SHALL default to `A` when absent. The `type` parameter SHALL accept DNS type mnemonics case-insensitively (for example `TXT`, `txt`, and `Txt` all resolve to TXT) and SHALL accept numeric DNS type codes in the range 0 to 65535 inclusive. The parsed name and type SHALL form a single-question DNS query, with the recursion-desired bit set on the dispatched query, dispatched through the same authoritative query path used by the wire-format, UDP, and TCP transports, so view selection, the ephemeral overlay, rate limiting, and response assembly behave identically across transports.

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

#### Scenario: control bytes in the JSON name are escaped to match the wire path

- **GIVEN** a `name` parameter whose raw bytes include a control byte such as a newline (0x0A)
- **WHEN** the endpoint parses the dns-json request
- **THEN** the resulting question name SHALL be byte-for-byte identical to the presentation-form name the wire-format path yields for the same on-wire name (the newline rendered as `\010`)
- **AND** no raw control byte SHALL survive into the question name or into any query log line produced for the request

#### Scenario: the JSON path and the wire-format path yield the same question name

- **GIVEN** the same on-wire name carrying a control byte, submitted once via the dns-json `name` parameter and once via the wire-format `?dns=` parameter
- **WHEN** the endpoint parses each request
- **THEN** the two requests SHALL produce a byte-for-byte identical question name
