## MODIFIED Requirements

### Requirement: Account key construction with name imputation

The limiter SHALL build each account key from the client address masked by `ipv4-prefix-length` (default 24) or `ipv6-prefix-length` (default 56), the response category, and an imputed name. The imputed name SHALL be derived per category: for `responses` (positive answers) the exact query name SHALL be used, EXCEPT when the positive answer was synthesized from a wildcard, in which case the closest enclosing wildcard owner (the `*.zone` node that produced the answer) SHALL be used so that a flood of distinct labels covered by one wildcard aggregates into a single account (mirroring the zone-origin aggregation used for negative answers and matching BIND's imputation of the wildcard owner); for `nxdomains` and `nodata` the matched zone origin SHALL be used so that a flood of distinct non-existent names under one zone aggregates into a single account; for `errors` (including REFUSED for names outside all zones) an empty name SHALL be used so that all error responses to one client block aggregate into a single account. The wildcard owner and query name SHALL both be folded to the lookup-key form so 0x20-randomized case variants share one account.

#### Scenario: Positive answers key on the query name

- **WHEN** two UDP queries from the same client block request different existing names that both return positive answers from exact-match (non-wildcard) records
- **THEN** the two responses SHALL be accounted under distinct accounts keyed by their respective query names

#### Scenario: Wildcard-synthesized positive-answer flood aggregates per wildcard owner

- **WHEN** many UDP queries from the same client block request distinct labels all covered by one wildcard (for example `*.example.com`), each returning a wildcard-synthesized positive answer
- **THEN** all those responses SHALL be accounted under a single account keyed by the wildcard owner (`*.example.com.`), not under distinct per-label accounts

##### Example: Wildcard-synthesized flood aggregation

- **GIVEN** a zone serving `*.example.com`, `responses-per-second = 5`, and queries for `r1.example.com`, `r2.example.com`, … each matching the wildcard
- **WHEN** 20 such queries arrive in one second from the same client block
- **THEN** all 20 share one account keyed by `(client-block, responses, *.example.com.)` and responses beyond 5 SHALL be over-limit

#### Scenario: Random-subdomain NXDOMAIN flood aggregates per zone

- **WHEN** many UDP queries from the same client block request distinct non-existent names under one zone, all returning NXDOMAIN
- **THEN** all those NXDOMAIN responses SHALL be accounted under a single account keyed by the zone origin

##### Example: NXDOMAIN aggregation

- **GIVEN** zone origin `example.com.`, `nxdomains-per-second = 5`, and queries for `a1.example.com`, `a2.example.com`, … each non-existent
- **WHEN** 20 such queries arrive in one second from the same client block
- **THEN** all 20 share one account keyed by `(client-block, nxdomains, example.com.)` and responses beyond 5 SHALL be over-limit

#### Scenario: Client address is masked to the configured prefix

- **WHEN** `ipv4-prefix-length = 24` and two queries arrive from `192.0.2.10` and `192.0.2.200`
- **THEN** both SHALL map to the same masked client block `192.0.2.0/24` for accounting
