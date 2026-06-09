## MODIFIED Requirements

### Requirement: Derive listen address set from named.conf listen-on

The dns-server SHALL derive its listen-address set at startup (and at every SIGHUP reload that reparses named.conf) using this precedence:

1. If the `--listen` CLI flag has a non-empty host component (e.g. `127.0.0.1:5353`, `10.0.0.1:53`, `[::1]:5353`), the listen-address set SHALL contain exactly the single address passed via `--listen`, and both `options.listen-on` and `options.listen-on-v6` from named.conf SHALL be ignored. The host MAY be an IPv4 literal or an IPv6 literal in bracket form. This preserves override semantics for tests and special deployments.
2. Otherwise (the host component is empty, e.g. `:53`, `:5353`, `:0`), the listen-address set SHALL be the union of the IPv4 addresses resolved from the `listen-on` token list and the IPv6 addresses resolved from the `listen-on-v6` token list, each address combined with the port component of `--listen` (default 53). The IPv4 addresses SHALL appear before the IPv6 addresses, each family preserving first-appearance order. IPv6 addresses SHALL be emitted in bracket form `[addr]:port`.
3. Within the IPv4 family of case 2: if `options.listen-on` is absent, the IPv4 set SHALL be resolved as if `listen-on { any; };` were specified. If `options.listen-on-v6` is absent, the IPv6 set SHALL be empty (IPv6 is opt-in; absence SHALL NOT imply `any`). This preserves IPv4-only behavior for deployments that do not declare `listen-on-v6`.

In all cases the port component from `--listen` (default 53) SHALL be applied consistently across both families: when `--listen` is `:5353`, every resolved IPv4 and IPv6 address SHALL use port 5353.

Token resolution rules for `listen-on` (IPv4 family):

- `any` SHALL expand to every IPv4 address reported by `net.InterfaceAddrs()` for interfaces that are up, excluding IPv6 addresses and excluding the link-local range `169.254.0.0/16`. Loopback addresses (`127.0.0.0/8`, including aliases like `127.0.0.53`) SHALL be included.
- `none` SHALL resolve to an empty set.
- Literal IPv4 addresses SHALL be used as-is.
- Unsupported tokens (IPv6 literals, exclusion syntax `!addr`, ACL names, `port N`, `interface` keyword) SHALL be logged at WARN level including the offending token, and the token SHALL be skipped. Parsing MUST NOT fail because of unsupported tokens.

Token resolution rules for `listen-on-v6` (IPv6 family):

- `any` SHALL expand to every IPv6 address reported by `net.InterfaceAddrs()` for interfaces that are up, excluding IPv4 addresses and excluding the link-local range `fe80::/10`. The loopback address `::1` SHALL be included.
- `none` SHALL resolve to an empty set.
- Literal IPv6 addresses SHALL be used as-is and emitted in bracket form.
- Unsupported tokens (IPv4 literals, exclusion syntax `!addr`, ACL names, `port N`, `interface` keyword) SHALL be logged at WARN level including the offending token, and the token SHALL be skipped. Parsing MUST NOT fail because of unsupported tokens.

When the resulting listen-address set (after unioning both families) is empty, the server SHALL exit with a fatal error explaining that no listeners would be started. When the empty result is caused by an explicit `none` in a family that was the only declared family, the fatal error SHALL distinguish that explicit cause from the "all tokens unsupported" cause for that family.

SIGHUP reload SHALL NOT rebind listeners even if the resolved listen-address set (IPv4 or IPv6) would change; operators needing a binding change MUST restart the process. The dns-server SHALL log an INFO message at reload time stating that listen-address changes require restart if the resolved set differs from the currently bound set; this comparison SHALL cover both IPv4 and IPv6 addresses.

#### Scenario: listen-on { any; } binds every IPv4 interface address

- **WHEN** named.conf sets `listen-on { any; };` AND the host has IPv4 addresses `10.0.0.1`, `127.0.0.1`, and `127.0.0.53`
- **THEN** the server attempts to bind UDP and TCP on `10.0.0.1:53`, `127.0.0.1:53`, and `127.0.0.53:53`

#### Scenario: listen-on with explicit addresses binds only those addresses

- **WHEN** named.conf sets `listen-on { 10.0.0.1; 192.168.1.1; };`
- **THEN** the server attempts to bind UDP and TCP on exactly `10.0.0.1:53` and `192.168.1.1:53` AND does not attempt any other address

#### Scenario: --listen override bypasses listen-on

- **WHEN** the process is started with `--listen 127.0.0.1:5353` AND named.conf sets `listen-on { any; };`
- **THEN** the server binds UDP and TCP only on `127.0.0.1:5353` AND does not enumerate interface addresses

#### Scenario: --listen with empty host does not bypass listen-on

- **WHEN** the process is started without `--listen` (or with any `:PORT` form that has no host, such as `:53` or `:0`) AND named.conf sets `listen-on { 10.0.0.1; };`
- **THEN** the server binds on `10.0.0.1:<port>` AND not on the wildcard `0.0.0.0:<port>`

#### Scenario: Port from --listen is applied to listen-on addresses

- **WHEN** the process is started with `--listen :5353` AND named.conf sets `listen-on { 10.0.0.1; };`
- **THEN** the server binds on `10.0.0.1:5353`

#### Scenario: Unsupported listen-on token is skipped with a warning

- **WHEN** named.conf sets `listen-on { !10.0.0.1; any; };`
- **THEN** the server logs a WARN message naming the unsupported token `!10.0.0.1` AND proceeds to resolve `any`

#### Scenario: listen-on { none; } with no other family causes fatal error

- **WHEN** named.conf sets `listen-on { none; };` AND `listen-on-v6` is absent AND `--listen` is at its default value
- **THEN** the server exits with a fatal error indicating no listeners would be started

#### Scenario: listen-on-v6 absent yields IPv4-only behavior

- **WHEN** named.conf sets `listen-on { 10.0.0.1; };` AND `listen-on-v6` is absent AND `--listen` is `:53`
- **THEN** the resolved listen-address set is exactly `{10.0.0.1:53}` AND no IPv6 listener is attempted

#### Scenario: listen-on-v6 { none; } yields IPv4-only behavior

- **WHEN** named.conf sets `listen-on { 10.0.0.1; };` AND `listen-on-v6 { none; };` AND `--listen` is `:53`
- **THEN** the resolved listen-address set is exactly `{10.0.0.1:53}` AND the server starts successfully

#### Scenario: listen-on-v6 with explicit addresses binds those IPv6 addresses

- **WHEN** named.conf sets `listen-on-v6 { 2001:db8::1; };` AND `listen-on { 10.0.0.1; };` AND `--listen` is `:53`
- **THEN** the resolved listen-address set is `{10.0.0.1:53, [2001:db8::1]:53}` with the IPv4 address ordered before the IPv6 address

#### Scenario: listen-on-v6 { any; } enumerates IPv6 interfaces excluding link-local

- **WHEN** named.conf sets `listen-on-v6 { any; };` AND the host has IPv6 addresses `2001:db8::1`, `fe80::1`, and `::1`
- **THEN** the server attempts to bind UDP and TCP on `[2001:db8::1]:53` and `[::1]:53` AND does not attempt `[fe80::1]:53`

##### Example: any expansion mixing both families

- **GIVEN** interface addresses: `10.0.0.1`, `169.254.1.1`, `2001:db8::1`, `fe80::1`, `::1`
- **WHEN** named.conf sets `listen-on { any; };` AND `listen-on-v6 { any; };` AND `--listen` is `:53`
- **THEN** the resolved set is `{10.0.0.1:53, [2001:db8::1]:53, [::1]:53}` (IPv4 link-local `169.254.1.1` and IPv6 link-local `fe80::1` are excluded; both loopbacks retained; IPv4 ordered first)

#### Scenario: --listen with IPv6 literal host binds only that address

- **WHEN** the process is started with `--listen [::1]:5353` AND named.conf sets `listen-on { any; };` AND `listen-on-v6 { any; };`
- **THEN** the server binds UDP and TCP only on `[::1]:5353` AND does not enumerate interface addresses

#### Scenario: IPv4 literal in listen-on-v6 is skipped with a warning

- **WHEN** named.conf sets `listen-on-v6 { 10.0.0.1; 2001:db8::1; };`
- **THEN** the server logs a WARN message naming the unsupported token `10.0.0.1` AND resolves the IPv6 set to `{[2001:db8::1]:53}`

#### Scenario: SIGHUP reload does not rebind listeners

- **WHEN** the server is running with listeners bound for `listen-on { 10.0.0.1; };` AND named.conf is edited to `listen-on { 10.0.0.2; };` AND SIGHUP is sent
- **THEN** the server continues serving on `10.0.0.1:53` only AND logs an INFO message stating that listen-address changes require restart

#### Scenario: SIGHUP reload drift detection covers IPv6

- **WHEN** the server is running with listeners bound for `listen-on-v6 { 2001:db8::1; };` AND named.conf is edited to `listen-on-v6 { 2001:db8::2; };` AND SIGHUP is sent
- **THEN** the server continues serving on `[2001:db8::1]:53` only AND logs an INFO message stating that listen-address changes require restart
