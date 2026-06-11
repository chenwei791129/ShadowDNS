## MODIFIED Requirements

### Requirement: Resolve client IP to a view using first-match semantics

The view-matcher SHALL accept two addresses — the real client source IP and a geo lookup address — and return the name of the first view whose `match-clients` rule set contains a matching rule. Country and ASN rules SHALL be evaluated against the geo lookup address; `any`, IP, and CIDR rules SHALL be evaluated against the source IP. Callers without an ECS-derived address SHALL pass the source IP as the geo lookup address, in which case behavior is identical to single-address resolution. Rules within a view SHALL be evaluated in declaration order and the first matching rule SHALL select that view without evaluating subsequent rules or views.

#### Scenario: First view whose rule matches wins

- **WHEN** views are declared in order `view-th` (rule: country TH), `view-eu` (rule: country DE), `view-other` (rule: any) AND the geo lookup address resolves to country DE
- **THEN** the matcher returns `view-eu`

#### Scenario: Fallback to `any` when no earlier view matches

- **WHEN** the geo lookup address resolves to a country not listed in any earlier view
- **THEN** the matcher returns the name of the view whose rule list contains `any`

#### Scenario: No matching view returns an empty result

- **WHEN** no view contains a rule that matches either address and no view declares `any`
- **THEN** the matcher returns an explicit no-view sentinel AND the caller is responsible for producing REFUSED

#### Scenario: Geo and ACL rules evaluate different addresses in one resolution

- **WHEN** views are declared in order `view-internal` (rule: CIDR `192.0.2.0/24`), `view-asia` (rule: country TW), the source IP is `198.51.100.1` (outside the CIDR), and the geo lookup address `203.0.113.0` resolves to country TW
- **THEN** the matcher returns `view-asia` because the CIDR rule evaluated the source IP and the country rule evaluated the geo lookup address

### Requirement: Evaluate country match via MaxMind GeoLite2-Country

The view-matcher SHALL look up the geo lookup address in a MaxMind GeoLite2-Country `.mmdb` file loaded at startup and compare the resulting ISO 3166-1 alpha-2 country code (case-insensitive) against rules of type country.

#### Scenario: Country code matches

- **WHEN** the mmdb lookup for the geo lookup address returns country code `TH` and a rule declares `geoip country TH;`
- **THEN** the rule matches

#### Scenario: Case insensitivity

- **WHEN** a rule declares `geoip country th;` (lowercase) and the mmdb returns `TH`
- **THEN** the rule matches

#### Scenario: IP not in mmdb is treated as no-match for country rules

- **WHEN** the mmdb lookup returns no country for the geo lookup address
- **THEN** all country rules evaluate to no-match for that client AND matching proceeds to subsequent rules

### Requirement: Evaluate ASN match via MaxMind GeoLite2-ASN

The view-matcher SHALL look up the geo lookup address in a MaxMind GeoLite2-ASN `.mmdb` file loaded at startup and compare the resulting AS number against the numeric AS number extracted from ASN rules.

#### Scenario: ASN number matches

- **WHEN** the mmdb lookup for the geo lookup address returns ASN 4134 and a rule declares `geoip asnum "AS4134 Chinanet";`
- **THEN** the rule matches

#### Scenario: ASN description text is ignored in comparison

- **WHEN** the mmdb description for ASN 4134 differs from the rule description but the number matches
- **THEN** the rule matches

### Requirement: Evaluate IP and CIDR rules without external lookup

The view-matcher SHALL evaluate `IPRule` and `CIDRRule` entries by direct comparison against the real client source IP; no GeoIP lookup SHALL be performed for these rule types, and the geo lookup address MUST NOT influence their evaluation (an ECS-derived address is client-controlled data and MUST NOT satisfy ACL-style rules).

#### Scenario: Single IP rule matches exactly

- **WHEN** a rule declares `192.0.2.8;` and the client source IP is `192.0.2.8`
- **THEN** the rule matches

#### Scenario: Client IP inside CIDR prefix matches

- **WHEN** a rule declares `198.51.100.0/26;` and the client source IP is `198.51.100.30`
- **THEN** the rule matches

#### Scenario: Client IP outside CIDR prefix does not match

- **WHEN** the client source IP is `198.51.100.100` (outside the /26 prefix)
- **THEN** the rule does not match

#### Scenario: Geo lookup address never satisfies a CIDR rule

- **WHEN** a rule declares `192.0.2.0/24;`, the client source IP is `203.0.113.7`, and the geo lookup address is `192.0.2.5`
- **THEN** the rule does not match
