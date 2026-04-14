## ADDED Requirements

### Requirement: Resolve client IP to a view using first-match semantics

The view-matcher SHALL accept a client IPv4 address and return the name of the first view whose `match-clients` rule set contains a matching rule. Rules within a view SHALL be evaluated in declaration order and the first matching rule SHALL select that view without evaluating subsequent rules or views.

#### Scenario: First view whose rule matches wins

- **WHEN** views are declared in order `view-th` (rule: country TH), `view-eu` (rule: country DE), `view-other` (rule: any) AND the client IP resolves to country DE
- **THEN** the matcher returns `view-eu`

#### Scenario: Fallback to `any` when no earlier view matches

- **WHEN** the client IP resolves to a country not listed in any earlier view
- **THEN** the matcher returns the name of the view whose rule list contains `any`

#### Scenario: No matching view returns an empty result

- **WHEN** no view contains a rule that matches the client IP and no view declares `any`
- **THEN** the matcher returns an explicit no-view sentinel AND the caller is responsible for producing REFUSED

### Requirement: Evaluate country match via MaxMind GeoLite2-Country

The view-matcher SHALL look up the client IP in a MaxMind GeoLite2-Country `.mmdb` file loaded at startup and compare the resulting ISO 3166-1 alpha-2 country code (case-insensitive) against rules of type country.

#### Scenario: Country code matches

- **WHEN** the mmdb lookup for the client IP returns country code `TH` and a rule declares `geoip country TH;`
- **THEN** the rule matches

#### Scenario: Case insensitivity

- **WHEN** a rule declares `geoip country th;` (lowercase) and the mmdb returns `TH`
- **THEN** the rule matches

#### Scenario: IP not in mmdb is treated as no-match for country rules

- **WHEN** the mmdb lookup returns no country for a given IP
- **THEN** all country rules evaluate to no-match for that client AND matching proceeds to subsequent rules

### Requirement: Evaluate ASN match via MaxMind GeoLite2-ASN

The view-matcher SHALL look up the client IP in a MaxMind GeoLite2-ASN `.mmdb` file loaded at startup and compare the resulting AS number against the numeric AS number extracted from ASN rules.

#### Scenario: ASN number matches

- **WHEN** the mmdb lookup returns ASN 4134 and a rule declares `geoip asnum "AS4134 Chinanet";`
- **THEN** the rule matches

#### Scenario: ASN description text is ignored in comparison

- **WHEN** the mmdb description for ASN 4134 differs from the rule description but the number matches
- **THEN** the rule matches

### Requirement: Evaluate IP and CIDR rules without external lookup

The view-matcher SHALL evaluate `IPRule` and `CIDRRule` entries by direct comparison against the client IP; no GeoIP lookup SHALL be performed for these rule types.

#### Scenario: Single IP rule matches exactly

- **WHEN** a rule declares `192.0.2.8;` and the client IP is `192.0.2.8`
- **THEN** the rule matches

#### Scenario: Client IP inside CIDR prefix matches

- **WHEN** a rule declares `198.51.100.0/26;` and the client IP is `198.51.100.30`
- **THEN** the rule matches

#### Scenario: Client IP outside CIDR prefix does not match

- **WHEN** the client IP is `198.51.100.100` (outside the /26 prefix)
- **THEN** the rule does not match

### Requirement: Fail startup when GeoIP databases are missing or unreadable

The view-matcher SHALL load both `GeoLite2-Country.mmdb` and `GeoLite2-ASN.mmdb` from the directory specified by `options.geoip-directory` at startup. If either file is missing, unreadable, or fails validation as a MaxMind mmdb, the server SHALL exit with a non-zero status and an error message naming the missing or invalid file.

#### Scenario: Missing country mmdb is fatal

- **WHEN** the country mmdb file does not exist at the configured path
- **THEN** the process exits with a non-zero status AND logs the expected path

#### Scenario: Corrupt ASN mmdb is fatal

- **WHEN** the ASN mmdb file fails library-level validation
- **THEN** the process exits with a non-zero status AND logs the validation error
