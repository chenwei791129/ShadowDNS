## MODIFIED Requirements

### Requirement: Derive listen address set from named.conf listen-on

The dns-server SHALL derive its listen-address set at startup (and at every SIGHUP reload that reparses named.conf) using this precedence:

1. If the `--listen` CLI flag has a non-empty host component (e.g. `127.0.0.1:5353`, `10.0.0.1:53`), the listen-address set SHALL contain exactly the single address passed via `--listen`, and `options.listen-on` from named.conf SHALL be ignored. This preserves override semantics for tests and special deployments.
2. Otherwise (the host component is empty, e.g. `:53`, `:5353`, `:0`), if `options.listen-on` in named.conf is non-empty, the listen-address set SHALL be the IPv4 addresses resolved from the `listen-on` token list, each combined with the port component of `--listen` (default 53).
3. Otherwise (empty host AND `listen-on` absent), the listen-address set SHALL be resolved as if `listen-on { any; };` were specified, with the port component from `--listen`.

In all cases the port component from `--listen` (default 53) SHALL be applied consistently: when `--listen` is `:5353`, every resolved IPv4 address SHALL use port 5353.

Token resolution rules for `listen-on`:

- `any` SHALL expand to every IPv4 address reported by `net.InterfaceAddrs()` for interfaces that are up, excluding IPv6 addresses and excluding the link-local range `169.254.0.0/16`. Loopback addresses (`127.0.0.0/8`, including aliases like `127.0.0.53`) SHALL be included.
- `none` SHALL resolve to an empty set; when the resulting listen-address set is empty and `none` was the explicit cause, the server SHALL exit with a fatal error explaining that no IPv4 listeners would be started.
- Literal IPv4 addresses SHALL be used as-is.
- Unsupported tokens (exclusion syntax `!addr`, ACL names, `port N`, `interface` keyword) SHALL be logged at WARN level including the offending token, and the token SHALL be skipped. Parsing MUST NOT fail because of unsupported tokens.

SIGHUP reload SHALL NOT rebind listeners even if the resolved listen-address set would change; operators needing a binding change MUST restart the process. The dns-server SHALL log an INFO message at reload time stating that listen-address changes require restart if the resolved set differs from the currently bound set.

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

#### Scenario: listen-on { none; } on IPv4 causes fatal error

- **WHEN** named.conf sets `listen-on { none; };` AND `--listen` is at its default value
- **THEN** the server exits with a fatal error indicating no IPv4 listeners would be started

#### Scenario: SIGHUP reload does not rebind listeners

- **WHEN** the server is running with listeners bound for `listen-on { 10.0.0.1; };` AND named.conf is edited to `listen-on { 10.0.0.2; };` AND SIGHUP is sent
- **THEN** the server continues serving on `10.0.0.1:53` only AND logs an INFO message stating that listen-address changes require restart
