## ADDED Requirements

### Requirement: Parse the rate-limit block in the options block

The config-loader SHALL parse a `rate-limit { ... }` block inside the `options` block of `named.conf` into a rate-limit configuration value. It SHALL recognize the sub-options `responses-per-second`, `referrals-per-second`, `nodata-per-second`, `nxdomains-per-second`, `errors-per-second`, `all-per-second`, `window`, `slip`, `ipv4-prefix-length`, `ipv6-prefix-length`, `exempt-clients`, `log-only`, `max-table-size`, and `min-table-size`. Absent sub-options SHALL take BIND-compatible defaults: per-second limits default to `0` (and the per-category limits default to the value of `responses-per-second` when not individually set), `window` defaults to `15`, `slip` defaults to `2`, `ipv4-prefix-length` defaults to `24`, `ipv6-prefix-length` defaults to `56`, `log-only` defaults to `no`, `max-table-size` defaults to `20000`, and `min-table-size` defaults to `500`. A value outside its BIND-defined valid range SHALL cause a fatal parse error consistent with other numeric option validation. When no `rate-limit` block is present, the parsed configuration SHALL indicate that rate limiting is unconfigured (distinct from a block with all-zero limits).

#### Scenario: Full rate-limit block parses with explicit values

- **WHEN** the `options` block contains `rate-limit { responses-per-second 10; window 20; slip 3; exempt-clients { 192.0.2.0/24; }; };`
- **THEN** the parsed configuration SHALL report `responses-per-second = 10`, `window = 20`, `slip = 3`, and an exempt list containing `192.0.2.0/24`

#### Scenario: Omitted sub-options take BIND defaults

- **WHEN** the `options` block contains `rate-limit { responses-per-second 5; };`
- **THEN** the parsed configuration SHALL report `window = 15`, `slip = 2`, `ipv4-prefix-length = 24`, `ipv6-prefix-length = 56`, `max-table-size = 20000`, and `min-table-size = 500`

#### Scenario: Per-category limit defaults to responses-per-second

- **WHEN** the block sets `responses-per-second 8;` and does not set `nxdomains-per-second`
- **THEN** the parsed `nxdomains-per-second` SHALL be `8`

#### Scenario: Out-of-range value is fatal

- **WHEN** the block contains `slip 99;` (outside the valid range 0–10)
- **THEN** the loader SHALL return a fatal parse error and SHALL NOT start the server

#### Scenario: Absent block is distinguishable from zeroed block

- **WHEN** the `options` block contains no `rate-limit` block
- **THEN** the parsed configuration SHALL indicate rate limiting is unconfigured rather than configured with zero limits

### Requirement: Warn and ignore unsupported rate-limit constructs

The config-loader SHALL emit a warning and ignore the `qps-scale` sub-option when it appears inside a `rate-limit` block, because load-adaptive scaling is not implemented. The config-loader SHALL emit a warning and ignore a `rate-limit` block that appears inside a `view` clause, because rate limiting is supported only at the global `options` scope; such a view-level block SHALL NOT cause a fatal error.

#### Scenario: qps-scale is warned and ignored

- **WHEN** a `rate-limit` block contains `qps-scale 250;`
- **THEN** the loader SHALL emit a warning, SHALL ignore the sub-option, and SHALL continue parsing the rest of the block

#### Scenario: View-level rate-limit is warned and ignored

- **WHEN** a `view` clause contains a `rate-limit { ... }` block
- **THEN** the loader SHALL emit a warning, SHALL ignore the block, and SHALL NOT return a fatal error
