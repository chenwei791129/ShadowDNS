## ADDED Requirements

### Requirement: Fail closed when a match-clients rule cannot be evaluated

When the config-loader drops a `match-clients` rule it cannot evaluate (a named-acl reference, a `!` negation, or a nested group — see config-loader), the view-matcher SHALL treat that dropped rule as never-matching. A dropped rule SHALL NOT be promoted to `any` or to any matching behavior. Consequently, a view whose entire `match-clients` set was dropped SHALL match no client and SHALL serve none of its zones; the matcher SHALL fall through to subsequent views exactly as it does for a view whose rules simply do not match the client. The matcher SHALL NOT fail open under any circumstance: an unevaluable access-control construct SHALL only ever reduce, never widen, the set of clients a view serves.

#### Scenario: View with only a dropped rule matches no client

- **WHEN** a view `"internal"` declared `match-clients { internal-net; }` where `internal-net` was dropped as an unrecognized rule, and a query arrives from any source IP
- **THEN** the view-matcher does not select `"internal"` for that query AND evaluation proceeds to subsequent views

#### Scenario: Dropped rule does not widen a view with other rules

- **WHEN** a view declares `match-clients { internal-net; 192.0.2.0/24; }` where `internal-net` was dropped, and a query arrives from source IP `198.51.100.7` (outside the CIDR)
- **THEN** the view-matcher does not select this view (the surviving CIDR rule does not match and the dropped rule never matches)

#### Scenario: Dropped rule is never treated as a catch-all

- **WHEN** a view's only `match-clients` entry was a dropped unrecognized rule AND no later view declares `any`
- **THEN** a client that matches no other view receives the explicit no-view result (REFUSED) rather than being served by the view with the dropped rule
