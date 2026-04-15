## MODIFIED Requirements

### Requirement: Listen for DNS queries on UDP and TCP port 53

The dns-server SHALL bind both UDP and TCP listeners for every address in its resolved listen-address set (see "Derive listen address set from named.conf listen-on" below) and serve DNS queries on both transports on every successfully bound address. The TCP listener SHALL remain required even when zone transfer is disabled, because TCP is the RFC 7766 fallback for responses larger than the UDP payload limit.

#### Scenario: UDP query receives response

- **WHEN** a client sends a valid DNS query over UDP on port 53 to any address the server has successfully bound
- **THEN** the server responds over UDP within the same 5-tuple

#### Scenario: TCP query receives response

- **WHEN** a client sends a valid DNS query over TCP on port 53 to any address the server has successfully bound
- **THEN** the server accepts the connection, reads the 2-byte length prefix, and writes a length-prefixed response

#### Scenario: Response exceeding UDP limit sets TC flag

- **WHEN** a response over UDP would exceed 512 bytes (or the negotiated EDNS0 UDP size) and cannot be truncated to fit
- **THEN** the server sets the TC (truncated) flag in the UDP response header so the client falls back to TCP

## ADDED Requirements

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

### Requirement: Tolerate per-address bind failures

The dns-server SHALL attempt to bind each address in the resolved listen-address set independently. For each address, UDP and TCP SHALL be bound as an atomic pair: if either the UDP or the TCP bind for a given address fails, the already-bound half of that pair SHALL be closed and the address SHALL be counted as failed. Address-level bind failures SHALL be logged at WARN level with the address and the underlying error, and the server SHALL continue attempting the remaining addresses. Successful bind attempts SHALL be logged at INFO level with the address.

The server SHALL return a fatal error and fail to start only when every address in the resolved listen-address set has failed to bind. The fatal error SHALL include the count of attempted addresses.

When the WARN is caused by `EADDRINUSE` on a loopback address in `127.0.0.0/8` (typical of systemd-resolved's stub listener occupying `127.0.0.53` or `127.0.0.54`), the WARN SHALL include a hint stating the likely cause and that `DNSStubListener=no` in `resolved.conf` removes the conflict.

#### Scenario: One address fails, others succeed

- **WHEN** the resolved listen-address set is `{10.0.0.1:53, 127.0.0.53:53}` AND `127.0.0.53:53` is already in use by another process
- **THEN** the server logs WARN for `127.0.0.53:53` with the systemd-resolved hint AND logs INFO for `10.0.0.1:53` AND starts successfully serving queries on `10.0.0.1:53`

#### Scenario: TCP bind fails after UDP succeeds for the same address

- **WHEN** the resolved listen-address set contains `10.0.0.1:53` AND the UDP bind on `10.0.0.1:53` succeeds AND the TCP bind on `10.0.0.1:53` subsequently fails
- **THEN** the server closes the already-open UDP socket on `10.0.0.1:53` AND logs WARN for `10.0.0.1:53` AND treats the address as failed

#### Scenario: All addresses fail to bind

- **WHEN** every address in the resolved listen-address set fails to bind
- **THEN** the server returns a fatal error including the number of attempted addresses AND the process exits with a non-zero status
