## MODIFIED Requirements

### Requirement: Resolve client IP to a view using first-match semantics

The view-matcher SHALL accept two addresses — the real client source IP and a geo lookup address — and return the name of the first view whose `match-clients` address-match-list **accepts** the client. A view's address-match-list SHALL be evaluated in declaration order: the first element whose predicate matches the client decides the list outcome — a positive (non-negated) matching element makes the list **accept** (select this view); a negated matching element (`!`) makes the list **reject** (do not select this view, fall through to the next view); if no element matches, the list does not accept (default deny, fall through). Country and ASN element predicates SHALL be evaluated against the geo lookup address; `any`, `none`, `localhost`, `localnets`, IP, and CIDR element predicates SHALL be evaluated against the source IP. A named-acl reference or a nested `{ ... }` group SHALL match when its own address-match-list accepts the client, evaluated by the same first-match rule recursively; a leading `!` negates that result. The built-in `any` always matches; `none` never matches; `localhost` matches the server's own addresses; `localnets` matches the networks attached to the server's interfaces. Callers without an ECS-derived address SHALL pass the source IP as the geo lookup address, in which case behavior is identical to single-address resolution. An element that the config-loader dropped (an undefined reference) SHALL never match, and SHALL never be promoted to a matching or catch-all behavior (fail-closed).

#### Scenario: First view whose list accepts wins

- **WHEN** views are declared in order `view-th` (rule: country TH), `view-eu` (rule: country DE), `view-other` (rule: any) AND the geo lookup address resolves to country DE
- **THEN** the matcher returns `view-eu`

#### Scenario: Fallback to `any` when no earlier view accepts

- **WHEN** the geo lookup address resolves to a country not listed in any earlier view
- **THEN** the matcher returns the name of the view whose list contains `any`

#### Scenario: No accepting view returns an empty result

- **WHEN** no view's list accepts either address and no view declares `any`
- **THEN** the matcher returns an explicit no-view sentinel AND the caller is responsible for producing REFUSED

#### Scenario: Geo and ACL rules evaluate different addresses in one resolution

- **WHEN** views are declared in order `view-internal` (rule: CIDR `192.0.2.0/24`), `view-asia` (rule: country TW), the source IP is `198.51.100.1` (outside the CIDR), and the geo lookup address `203.0.113.0` resolves to country TW
- **THEN** the matcher returns `view-asia` because the CIDR rule evaluated the source IP and the country rule evaluated the geo lookup address

#### Scenario: Negated element rejects, then any accepts the rest

- **WHEN** a view declares `match-clients { ! 192.0.2.0/24; any; }` and a query arrives from `198.51.100.7`
- **THEN** the `! 192.0.2.0/24` element does not match (so the list does not reject), the `any` element matches, and the matcher selects this view

##### Example: negate-then-any boundary

| Source IP | `! 192.0.2.0/24` matches? | List outcome |
|-----------|---------------------------|--------------|
| 192.0.2.5 | yes (negated match) | reject — view not selected |
| 198.51.100.7 | no | falls to `any` → accept |

#### Scenario: Named-acl reference selects via the referenced list

- **WHEN** `acl "internal" { 10.0.0.0/8; }` is defined, a view declares `match-clients { internal; }`, and the source IP is `10.0.0.3`
- **THEN** the reference matches (the `internal` list accepts) AND the matcher selects this view

#### Scenario: Negated named reference rejects matching clients

- **WHEN** a view declares `match-clients { ! internal; any; }` where `internal` is `10.0.0.0/8`, and the source IP is `10.0.0.3`
- **THEN** the `! internal` element matches and rejects the view (the matcher does not select it for `10.0.0.3`)

#### Scenario: Built-in localhost matches the server's own address

- **WHEN** a view declares `match-clients { localhost; }` and the source IP is one of the server's own interface addresses
- **THEN** the matcher selects this view

#### Scenario: Undefined reference never matches

- **WHEN** a view's only element is a reference to an undefined acl (dropped by the config-loader) and a query arrives from any source IP
- **THEN** the matcher does not select this view AND does not treat the dropped element as a catch-all
